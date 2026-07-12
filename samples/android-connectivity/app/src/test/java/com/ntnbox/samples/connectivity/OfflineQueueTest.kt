package com.ntnbox.samples.connectivity

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Test

class OfflineQueueTest {
    @Test
    fun enqueueAndDrain() {
        val q = OfflineQueue()
        q.enqueue("a")
        q.enqueue("b")
        assertEquals(2, q.size)
        assertEquals(listOf("a", "b"), q.drain())
        assertEquals(0, q.size)
    }

    @Test
    fun backoffAfterFailures() {
        val q = OfflineQueue(maxBackoffMs = 30_000L)
        val t0 = 1_000_000L
        q.onFailure(t0)
        assertFalse(q.shouldAttempt(t0 + 500))
        assertTrue(q.shouldAttempt(t0 + 2_500))
        q.onSuccess(t0 + 3_000)
        assertEquals(0, q.failureCount)
        assertTrue(q.shouldAttempt(t0 + 3_000))
    }

    @Test
    fun backoffCapsAtMax() {
        val q = OfflineQueue(maxBackoffMs = 3_000L)
        val t0 = 1_000_000L
        q.onFailure(t0) // 2s
        q.onFailure(t0) // would be 4s → capped to 3s
        assertEquals(3_000L, q.backoffRemainingMs(t0))
        assertFalse(q.shouldAttempt(t0 + 2_999))
        assertTrue(q.shouldAttempt(t0 + 3_000))
    }

    @Test
    fun multiFailureProgression() {
        val q = OfflineQueue(maxBackoffMs = 60_000L)
        val t0 = 1_000_000L
        q.onFailure(t0) // 2s
        assertEquals(2_000L, q.backoffRemainingMs(t0))
        q.onFailure(t0) // 4s
        assertEquals(4_000L, q.backoffRemainingMs(t0))
        q.onFailure(t0) // 8s
        assertEquals(8_000L, q.backoffRemainingMs(t0))
        assertEquals(3, q.failureCount)
    }
}
