package com.ntnbox.samples.connectivity

/**
 * In-memory offline queue with exponential backoff tracking.
 * Pure Kotlin — no Android dependencies (unit-testable).
 */
class OfflineQueue(
    private val maxBackoffMs: Long = 30_000L,
) {
    private val items = ArrayDeque<String>()
    private var consecutiveFailures = 0
    private var nextAttemptAtMs = 0L

    val size: Int get() = items.size
    val failureCount: Int get() = consecutiveFailures

    fun enqueue(item: String) {
        items.addLast(item)
    }

    fun drain(): List<String> {
        val flushed = items.toList()
        items.clear()
        return flushed
    }

    fun onSuccess(nowMs: Long = System.currentTimeMillis()) {
        consecutiveFailures = 0
        nextAttemptAtMs = nowMs
    }

    fun onFailure(nowMs: Long = System.currentTimeMillis()) {
        consecutiveFailures += 1
        val exp = (1L shl consecutiveFailures.coerceAtMost(10).coerceAtLeast(1))
        val backoff = (1000L * exp).coerceAtMost(maxBackoffMs)
        nextAttemptAtMs = nowMs + backoff
    }

    fun shouldAttempt(nowMs: Long = System.currentTimeMillis()): Boolean {
        return nowMs >= nextAttemptAtMs
    }

    fun backoffRemainingMs(nowMs: Long = System.currentTimeMillis()): Long {
        return (nextAttemptAtMs - nowMs).coerceAtLeast(0)
    }
}
