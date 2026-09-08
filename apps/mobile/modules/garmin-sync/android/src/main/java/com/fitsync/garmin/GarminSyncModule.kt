package com.fitsync.garmin

import android.content.Context
import expo.modules.kotlin.modules.Module
import expo.modules.kotlin.modules.ModuleDefinition
import java.util.UUID

class GarminSyncModule : Module() {
  private val runner = GarminSyncRunner()
  private lateinit var vault: GarminTokenVault

  override fun definition() = ModuleDefinition {
    Name("GarminSync")
    Events("garminSyncProgress", "garminSyncLog")

    OnCreate {
      vault = GarminTokenVault(requireNotNull(appContext.reactContext))
    }

    AsyncFunction("getConnectionState") { athleteID: String ->
      validateAthlete(athleteID)
      if (!vault.has(athleteID)) "disconnected" else if (vault.read(athleteID) == null) "needs_reconnect" else "connected"
    }

    AsyncFunction("startLogin") { athleteID: String, email: String, password: String ->
      validateAthlete(athleteID)
      operation {
        val value = runner.pythonCall("start_login", email, password)
        emitDiagnostics(value)
        if (value["ok"] != true) throwNative(value)
        if (value["status"] == "needs_mfa") mapOf("status" to "needs_mfa", "loginID" to value["login_id"])
        else {
          saveToken(athleteID, value["token_json"] as? String)
          mapOf("status" to "connected")
        }
      }
    }

    AsyncFunction("resumeLogin") { athleteID: String, loginID: String, code: String ->
      validateAthlete(athleteID)
      operation {
        val value = runner.pythonCall("resume_login", loginID, code)
        emitDiagnostics(value)
        if (value["ok"] != true) throwNative(value)
        saveToken(athleteID, value["token_json"] as? String)
        mapOf("status" to "connected")
      }
    }

    AsyncFunction("cancelLogin") { loginID: String ->
      runner.pythonCall("cancel_login", loginID)
      Unit
    }

    AsyncFunction("syncNow") { options: Map<String, Any?> ->
      val athleteID = options["athleteID"] as? String ?: throw GarminNativeException("INTERNAL", "Garmin athlete is invalid", false)
      validateAthlete(athleteID)
      val accessToken = options["accessToken"] as? String ?: throw GarminNativeException("FITNESSD_UNAUTHORIZED", "FitSync sign-in expired", true)
      val token = vault.read(athleteID) ?: throw GarminNativeException("GARMIN_AUTH_FAILED", "Reconnect Garmin before syncing", true)
      val config = mapOf("athlete_id" to athleteID, "api_base_url" to options["apiBaseURL"], "days" to options["days"], "extended" to options["extended"], "work_dir" to workDir(athleteID).absolutePath)
      operation {
        val stages = listOf("authenticating", "fetching_activities", "fetching_sleep", "fetching_recovery", "fetching_activity_details", "fetching_training", "uploading", "cleaning_up")
        stages.forEachIndexed { index, stage -> sendEvent("garminSyncProgress", mapOf("stage" to stage, "completed" to index, "total" to stages.size, "message" to "Syncing Garmin data")) }
        val value = runner.pythonCall("sync", org.json.JSONObject(config).toString(), token, accessToken)
        emitDiagnostics(value)
        if (value["ok"] != true) throwNative(value)
        saveToken(athleteID, value["refreshed_garmin_token_json"] as? String)
        sendEvent("garminSyncProgress", mapOf("stage" to "complete", "completed" to stages.size, "total" to stages.size, "message" to "Garmin sync complete"))
        mapOf("status" to "success", "recordsSent" to value["records_sent"], "archivesSent" to value["archives_sent"], "batchesSent" to value["batches_sent"], "startedAt" to value["started_at"], "completedAt" to value["completed_at"], "latestRecordDate" to value["latest_record_date"])
      }
    }

    AsyncFunction("disconnect") { athleteID: String ->
      validateAthlete(athleteID)
      vault.delete(athleteID)
      deleteTree(workDir(athleteID))
      Unit
    }
  }

  private fun operation(block: () -> Map<String, Any?>): Map<String, Any?> = runner.run(block)

  private fun saveToken(athleteID: String, token: String?) {
    if (token.isNullOrBlank()) throw GarminNativeException("INTERNAL", "Garmin session could not be saved", false)
    vault.write(athleteID, token)
  }

  private fun throwNative(value: Map<String, Any?>): Nothing {
    val error = value["error"] as? Map<*, *>
    val details = (value["logs"] as? String)?.takeIf { it.isNotBlank() }
      ?: error?.get("details") as? String
    throw GarminNativeException(error?.get("code") as? String ?: "INTERNAL", error?.get("message") as? String ?: "Garmin operation failed", error?.get("retryable") as? Boolean ?: false, details)
  }

  private fun emitDiagnostics(value: Map<String, Any?>) {
    val logs = value["logs"] as? String ?: return
    if (logs.isBlank()) return
    logs.lineSequence().filter { it.isNotBlank() }.forEach { line ->
      sendEvent("garminSyncLog", mapOf("level" to diagnosticLevel(line), "message" to line))
    }
  }

  private fun diagnosticLevel(line: String): String {
    val normalized = line.uppercase()
    return when {
      normalized.contains(" ERROR ") || normalized.contains(" CRITICAL ") -> "error"
      normalized.contains(" WARNING ") -> "warn"
      normalized.contains(" INFO ") -> "info"
      else -> "debug"
    }
  }

  private fun validateAthlete(athleteID: String) { UUID.fromString(athleteID) }

  private fun workDir(athleteID: String) = java.io.File(requireNotNull(appContext.reactContext).noBackupFilesDir, "garmin-work/$athleteID")

  private fun deleteTree(file: java.io.File) {
    if (file.isDirectory) file.listFiles()?.forEach { deleteTree(it) }
    file.delete()
  }
}
