package com.fitsync.nativeapp

import org.junit.Assert.*
import org.junit.Test

class LogBookTest {
    @Test fun credentialsAreRedactedIncludingQuotedSpacesAndBearer() {
        val value = LogBook.redact("password='secret words' access_token=abc authorization: Bearer xyz cookie=hidden mfa=123456")
        for (secret in listOf("secret words", "abc", "xyz", "hidden", "123456")) assertFalse(value.contains(secret))
        assertTrue(value.contains("[REDACTED]"))
    }
    @Test fun bufferIsBoundedAndClearable() {
        LogBook.clear(); repeat(600) { LogBook.add("test", "message $it") }
        assertEquals(500, LogBook.entries.value.size); assertTrue(LogBook.entries.value.last().contains("message 599"))
        LogBook.clear(); assertTrue(LogBook.entries.value.isEmpty())
    }
}
