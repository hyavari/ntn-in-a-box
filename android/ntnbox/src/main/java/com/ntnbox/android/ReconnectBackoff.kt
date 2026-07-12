package com.ntnbox.android

/**
 * Capped exponential backoff for SSE reconnects (pure JVM).
 */
class ReconnectBackoff(
    private val initialMs: Long = 500L,
    private val maxMs: Long = 15_000L,
) {
    private var attempt = 0

    fun nextDelayMs(): Long {
        attempt += 1
        val shift = (attempt - 1).coerceAtMost(10)
        val delay = initialMs * (1L shl shift)
        return delay.coerceAtMost(maxMs)
    }

    fun reset() {
        attempt = 0
    }
}
