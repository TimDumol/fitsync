package com.fitsync.nativeapp

import android.app.Application
import com.fitsync.garmin.createGarmin
import com.fitsync.nativeapp.auth.Auth
import com.fitsync.nativeapp.data.*
import com.fitsync.nativeapp.domain.*
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.sync.Mutex
import kotlinx.coroutines.sync.withLock
import kotlinx.coroutines.withContext
import org.json.JSONObject

class FitSyncApplication: Application() {
    val repository by lazy { Repository(FitDatabase.open(this)) }
    val auth by lazy { Auth(this) }
    val api by lazy { Api(createTransport(this) { force -> auth.token(force) }) }
    val sync by lazy { Sync(repository, api) }
    val garmin by lazy { createGarmin(this) }
    private val initialization = Mutex()
    private var initialized = false
    suspend fun initialize() = initialization.withLock {
        if (initialized) return@withLock
        if (BuildConfig.FIXTURES) repository.clear()
        withContext(Dispatchers.IO) { if (!BuildConfig.FIXTURES) LegacyImporter(this@FitSyncApplication, repository.db).import() }
        initialized = true
    }
    suspend fun authorized(athlete: String): Boolean {
        if (!auth.hasSession) return false
        val account = repository.store.meta("account") ?: return false
        return JSONObject(account).objects("accessible_athletes").any { it.text("athlete_id") == athlete }
    }
}
