package com.fitsync.nativeapp

import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.asStateFlow
import java.time.Instant

object LogBook {
    private val mutable = MutableStateFlow<List<String>>(emptyList())
    val entries = mutable.asStateFlow()
    private val credential = Regex("(?i)([\"']?(?:password|passphrase|[\\w-]*token[\\w-]*|authorization|cookie|secret|credential|mfa)[\"']?\\s*[:=]\\s*)(?:[\"'][^\"']*[\"']|[^\\s,;}]+)")
    fun redact(value: String) = value.replace(Regex("(?i)Bearer\\s+[^\\s,;}]+"), "Bearer [REDACTED]").replace(credential) { it.groupValues[1] + "[REDACTED]" }
    @Synchronized fun add(scope: String, message: String, level: String = "info") {
        mutable.value = (mutable.value + "${Instant.now()} ${level.uppercase()} [$scope] ${redact(message)}").takeLast(500)
    }
    @Synchronized fun clear() { mutable.value = emptyList() }
}
