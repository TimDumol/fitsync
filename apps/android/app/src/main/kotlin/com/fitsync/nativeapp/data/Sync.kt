package com.fitsync.nativeapp.data

import com.fitsync.nativeapp.auth.SessionExpired
import android.content.Context
import androidx.room.withTransaction
import androidx.work.*
import com.fitsync.nativeapp.FitSyncApplication
import com.fitsync.nativeapp.LogBook
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import org.json.JSONObject
import java.util.concurrent.TimeUnit

class Sync(private val repository: Repository, private val api: Api) {
    private val mutex = Mutex()
    suspend fun exclusive(block: suspend () -> Unit) = mutex.withLock { block() }
    suspend fun run(athlete: String) = mutex.withLock {
        val store = repository.store
        for (item in store.queue(athlete).filter { it.operation != "delete" && it.error?.contains("new IDs must be UUIDv7") == true }) {
            // Never change the identity of a workout already accepted by the server.
            try { api.workout(athlete, item.id) } catch (error: ApiError) {
                if (error.status != 404) throw error
                repository.repairRejectedIds(item)
            }
        }
        for (item in store.queue(athlete).filter { !it.blocked && it.nextAttempt <= System.currentTimeMillis() }) {
            // A queued operation may have changed since the list was read.
            if (store.pending(athlete, item.id)?.generation != item.generation) continue
            try {
                val workout = JSONObject(item.json)
                val response = when (item.operation) {
                    "delete" -> { try { api.delete(workout) } catch (error: ApiError) { if (error.status != 404) throw error }; null }
                    "complete" -> {
                        val existing = try { api.workout(athlete, item.id) } catch (error: ApiError) { if (error.status != 404) throw error; null }
                        when (existing?.text("status")) {
                            "completed" -> existing
                            "discarded" -> throw ApiError(409, "Workout was discarded on the server")
                            else -> {
                                if (existing == null) api.save(workout.copyJson().put("status", "in_progress").put("completed_at", JSONObject.NULL))
                                api.complete(workout)
                            }
                        }
                    }
                    else -> api.save(workout)
                }
                repository.acknowledge(item, response)
            } catch (error: CancellationException) { throw error
            } catch (error: Exception) {
                val status = (error as? ApiError)?.status
                val permanent = status in listOf(400, 403, 409, 422)
                repository.db.withTransaction {
                    if (store.pending(athlete, item.id)?.generation == item.generation) {
                        val retries = item.retries + 1
                        store.enqueue(item.copy(retries = retries, nextAttempt = System.currentTimeMillis() + minOf(3_600_000L, 1000L shl minOf(retries, 10)), error = LogBook.redact(error.message ?: "Sync failed"), blocked = permanent))
                    }
                }
                LogBook.add("sync", error.message ?: "Sync failed", "warn")
                if (status == 401 || error is SessionExpired) throw error
            }
        }
        var cursor = store.meta("sync_cursor:$athlete")?.toLongOrNull() ?: 0
        do {
            val page = api.pull(athlete, cursor)
            check(!page.getBoolean("has_more") || page.getLong("next_after") > cursor) { "Sync did not advance" }
            repository.applyPage(athlete, page)
            cursor = page.getLong("next_after")
        } while (page.getBoolean("has_more"))
    }
    companion object {
        fun schedule(context: Context, athlete: String, delayMs: Long = 0) {
            val request = OneTimeWorkRequestBuilder<SyncWorker>().setInputData(workDataOf("athlete" to athlete))
                .setConstraints(Constraints.Builder().setRequiredNetworkType(NetworkType.CONNECTED).build())
                .setInitialDelay(delayMs, TimeUnit.MILLISECONDS).setBackoffCriteria(BackoffPolicy.EXPONENTIAL, 30, TimeUnit.SECONDS).addTag("fitsync-sync").build()
            WorkManager.getInstance(context).enqueueUniqueWork("fitsync-sync:$athlete", ExistingWorkPolicy.APPEND_OR_REPLACE, request)
        }
    }
}
class SyncWorker(context: Context, params: WorkerParameters): CoroutineWorker(context, params) {
    override suspend fun doWork(): Result {
        val app = applicationContext as FitSyncApplication
        if (com.fitsync.nativeapp.BuildConfig.FIXTURES) return Result.success()
        return try {
            app.initialize()
            val athlete = inputData.getString("athlete") ?: return Result.failure()
            if (!app.authorized(athlete)) return Result.failure()
            app.sync.run(athlete)
            val remaining = app.repository.store.queue(athlete).filter { !it.blocked }
            if (remaining.isEmpty()) Result.success() else Result.retry()
        } catch (error: CancellationException) { throw error
        } catch (_: SessionExpired) { app.auth.expire(); Result.failure()
        } catch (error: ApiError) { if (error.status == 401) { app.auth.expire(); Result.failure() } else Result.retry()
        } catch (_: Exception) { Result.retry() }
    }
}
