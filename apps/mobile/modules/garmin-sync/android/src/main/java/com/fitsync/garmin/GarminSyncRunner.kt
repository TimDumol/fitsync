package com.fitsync.garmin

import java.util.concurrent.locks.ReentrantLock
import kotlin.concurrent.withLock

internal class GarminSyncRunner {
  private val operationLock = ReentrantLock()
  private var cancelled = false

  fun cancel() {
    operationLock.withLock { cancelled = true }
  }

  fun <T> run(block: () -> T): T {
    if (!operationLock.tryLock()) throw GarminNativeException("INTERNAL", "Another Garmin operation is already running", true)
    try {
      cancelled = false
      return block()
    } finally {
      operationLock.unlock()
    }
  }

  fun checkCancelled() {
    operationLock.withLock {
      if (cancelled) throw GarminNativeException("CANCELLED", "Garmin sync was cancelled", false)
    }
  }

  fun pythonCall(name: String, vararg args: Any?): Map<String, Any?> {
    checkCancelled()
    return try {
      // The Expo library is compiled separately from the application where
      // Chaquopy is applied, so use the runtime API without a library-level
      // dependency on Chaquopy's application plugin.
      val pythonClass = Class.forName("com.chaquo.python.Python")
      val python = pythonClass.getMethod("getInstance").invoke(null)
      val module = python.javaClass.getMethod("getModule", String::class.java).invoke(python, "phone_garmin_bridge")
      val callAttr = module.javaClass.getMethod("callAttr", String::class.java, Array<Any>::class.java)
      val pythonArgs = arrayOfNulls<Any>(args.size)
      args.copyInto(pythonArgs)
      val result = callAttr.invoke(module, name, pythonArgs)
      val raw = result.javaClass.getMethod("toJava", Class::class.java).invoke(result, String::class.java) as? String
        ?: throw IllegalStateException("Python result was not text")
      NativeJson.parse(raw)
    } catch (error: GarminNativeException) {
      throw error
    } catch (error: Exception) {
      val cause = generateSequence<Throwable>(error) { it.cause }.last()
      val detail = "${cause.javaClass.simpleName}: ${sanitize(cause.message ?: "no message")}".take(4000)
      android.util.Log.e("FitSync/Garmin", "Python bridge failed: $detail", error)
      throw GarminNativeException("INTERNAL", "Garmin operation could not be completed", true, detail)
    }
  }

  private fun sanitize(value: String): String = value.replace(
    Regex("(?i)(password|passphrase|token|authorization|cookie|secret|credential)\\s*[:=]\\s*[^\\s,;}]+"),
    "$1=[REDACTED]",
  )
}

internal class GarminNativeException(val code: String, val safeMessage: String, val retryable: Boolean, details: String? = null) : RuntimeException(
  "$code|$retryable|$safeMessage${details?.takeIf { it.isNotBlank() }?.let { "\\n---GARMIN_DETAILS---\\n$it" } ?: ""}",
)

private object NativeJson {
  fun parse(raw: String): Map<String, Any?> {
    val objectValue = org.json.JSONObject(raw)
    val result = mutableMapOf<String, Any?>("ok" to objectValue.optBoolean("ok"))
    objectValue.optString("logs", "").takeIf { it.isNotBlank() }?.let { result["logs"] = it }
    if (objectValue.optBoolean("ok")) {
      val value = objectValue.optJSONObject("value")
      if (value != null) {
        value.keys().forEach { key -> result[key] = value.opt(key) }
      }
    } else {
      val error = objectValue.optJSONObject("error") ?: org.json.JSONObject()
      val details = error.optString("details", "").takeIf { it.isNotBlank() }
      val errorMap = mutableMapOf<String, Any?>(
        "code" to error.optString("code", "INTERNAL"),
        "message" to error.optString("message", "Garmin operation failed"),
        "retryable" to error.optBoolean("retryable"),
      )
      if (details != null) errorMap["details"] = details
      result["error"] = errorMap
    }
    return result
  }
}
