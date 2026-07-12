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
}
