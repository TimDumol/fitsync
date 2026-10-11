package com.fitsync.garmin

import java.util.concurrent.locks.ReentrantLock

internal class GarminSyncRunner {
    private val operationLock = ReentrantLock()

    fun <T> run(block: () -> T): T {
        if (!operationLock.tryLock()) throw GarminNativeException("INTERNAL", "Another Garmin operation is already running", true)
        try { return block() } finally { operationLock.unlock() }
    }
}

internal class GarminNativeException(val code: String, val safeMessage: String, val retryable: Boolean): RuntimeException(safeMessage)
