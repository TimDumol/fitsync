package com.fitsync.nativeapp.data

import android.content.Context
import android.database.sqlite.SQLiteDatabase
import androidx.room.*
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.flow.Flow
import java.io.File

@Entity(tableName = "aggregates", primaryKeys = ["athlete", "kind", "id"])
data class Aggregate(val athlete: String, val kind: String, val id: String, val json: String, val revision: Long = 0, val updated: Long = System.currentTimeMillis())
@Entity(tableName = "queue", primaryKeys = ["athlete", "id"])
data class Pending(val athlete: String, val id: String, val operation: String, val json: String, val generation: Long, val retries: Int = 0, val nextAttempt: Long = 0, val error: String? = null, val blocked: Boolean = false)
@Entity(tableName = "metadata")
data class Metadata(@PrimaryKey val key: String, val value: String)

@Dao
interface Store {
    @Query("SELECT * FROM aggregates ORDER BY updated DESC") fun observe(): Flow<List<Aggregate>>
    @Query("SELECT * FROM metadata WHERE `key` LIKE 'active_plan:%'") fun observeActivePlans(): Flow<List<Metadata>>
    @Query("SELECT * FROM queue ORDER BY generation") fun observeQueue(): Flow<List<Pending>>
    @Query("SELECT * FROM aggregates WHERE athlete=:athlete AND kind=:kind") suspend fun list(athlete: String, kind: String): List<Aggregate>
    @Query("SELECT * FROM aggregates WHERE athlete=:athlete AND kind=:kind AND id=:id") suspend fun find(athlete: String, kind: String, id: String): Aggregate?
    @Insert(onConflict = OnConflictStrategy.REPLACE) suspend fun put(value: Aggregate)
    @Query("DELETE FROM aggregates WHERE athlete=:athlete AND id=:id AND kind IN ('draft','workout')") suspend fun removeWorkout(athlete: String, id: String)
    @Query("DELETE FROM aggregates WHERE athlete=:athlete AND kind=:kind AND id=:id") suspend fun remove(athlete: String, kind: String, id: String)
    @Query("SELECT * FROM queue WHERE athlete=:athlete ORDER BY generation") suspend fun queue(athlete: String): List<Pending>
    @Query("SELECT * FROM queue WHERE athlete=:athlete AND id=:id") suspend fun pending(athlete: String, id: String): Pending?
    @Insert(onConflict = OnConflictStrategy.REPLACE) suspend fun enqueue(value: Pending)
    @Query("DELETE FROM queue WHERE athlete=:athlete AND id=:id AND generation=:generation") suspend fun acknowledge(athlete: String, id: String, generation: Long)
    @Query("SELECT value FROM metadata WHERE `key`=:key") suspend fun meta(key: String): String?
    @Insert(onConflict = OnConflictStrategy.REPLACE) suspend fun meta(value: Metadata)
    @Query("DELETE FROM metadata WHERE `key`=:key") suspend fun removeMeta(key: String)
    @Query("DELETE FROM aggregates") suspend fun clearAggregates()
    @Query("DELETE FROM queue") suspend fun clearQueue()
    @Query("DELETE FROM metadata WHERE `key` <> 'legacy_import'") suspend fun clearMetadata()
}
@Database(entities = [Aggregate::class, Pending::class, Metadata::class], version = 1, exportSchema = true)
abstract class FitDatabase : RoomDatabase() {
    abstract fun store(): Store
    companion object {
        fun open(context: Context) = Room.databaseBuilder(context, FitDatabase::class.java, "fitsync-native.db").build()
    }
}

/** Source stays untouched. Room's import marker and every imported row commit together. */
class LegacyImporter(private val context: Context, private val db: FitDatabase) {
    suspend fun import() {
        if (db.store().meta("legacy_import") != null) return
        val candidates = listOf(File(context.filesDir, "SQLite/fitsync.db"), File(context.filesDir, "SQLite/fitsync.db/fitsync.db"), context.getDatabasePath("fitsync.db"))
        val file = candidates.firstOrNull { it.isFile }
        if (file == null) {
            // Detect alternate storage paths without reading unrelated private files.
            val unknown = context.filesDir.walkTopDown().maxDepth(5).any { it.name == "fitsync.db" && it.isFile }
            check(!unknown) { "Existing workout storage was found at an unsupported location. Migration stopped; data is preserved." }
            db.store().meta(Metadata("legacy_import", "absent")); return
        }
        SQLiteDatabase.openDatabase(file.absolutePath, null, SQLiteDatabase.OPEN_READONLY).use { legacy ->
            val metadata = mutableListOf<Metadata>()
            legacy.rawQuery("SELECT key,value FROM local_meta", null).use { rows -> while (rows.moveToNext()) metadata += Metadata(rows.getString(0), rows.getString(1)) }
            val version = metadata.find { it.key == "schema_version" }?.value?.toIntOrNull() ?: 0
            check(version in 0..3) { "Unsupported legacy schema $version. Local workouts are preserved." }
            val aggregates = mutableListOf<Aggregate>()
            for ((table, kind, idColumn) in listOf(Triple("cached_plans", "plan", "plan_id"), Triple("cached_workouts", "workout", "workout_id"), Triple("active_workout_drafts", "draft", "workout_id"))) {
                legacy.rawQuery("SELECT athlete_id,$idColumn,json FROM $table", null).use { rows -> while (rows.moveToNext()) {
                    val payload = org.json.JSONObject(rows.getString(2))
                    check(payload.getString("athlete_id") == rows.getString(0) && payload.getString(idColumn) == rows.getString(1)) { "Legacy workout ownership mismatch; migration stopped." }
                    aggregates += Aggregate(rows.getString(0), kind, rows.getString(1), payload.toString())
                } }
            }
            val pending = mutableListOf<Pending>()
            legacy.rawQuery("SELECT id,athlete_id,operation,aggregate_id,request_json,retry_count,next_attempt_at FROM outbox ORDER BY id", null).use { rows -> while (rows.moveToNext()) {
                val payload = org.json.JSONObject(rows.getString(4))
                check(payload.getString("athlete_id") == rows.getString(1) && payload.getString("workout_id") == rows.getString(3))
                check(rows.getString(2) in listOf("save", "complete", "delete"))
                pending += Pending(rows.getString(1), rows.getString(3), rows.getString(2), payload.toString(), rows.getLong(0), rows.getInt(5), rows.getLong(6))
            } }
            // Normalize the legacy per-operation queue into one latest intent per aggregate.
            val merged = pending.groupBy { it.athlete to it.id }.map { (_, items) ->
                items.lastOrNull { it.operation == "delete" } ?: items.lastOrNull { it.operation == "complete" } ?: items.last()
            }
            metadata.filter { it.key.startsWith("active_timer:") }.forEach { Timer.parse(it.value) }
            db.withTransaction {
                check(db.store().meta("legacy_import") == null)
                aggregates.forEach { db.store().put(it) }
                metadata.filter { it.key != "schema_version" }.forEach { db.store().meta(it) }
                merged.forEach { db.store().enqueue(it) }
                db.store().meta(Metadata("generation", (pending.maxOfOrNull { it.generation } ?: 0).toString()))
                db.store().meta(Metadata("legacy_athletes", aggregates.map { it.athlete }.plus(pending.map { it.athlete }).distinct().joinToString(",")))
                db.store().meta(Metadata("legacy_import", "schema:$version;aggregates:${aggregates.size};queue:${merged.size}"))
            }
        }
    }
}
