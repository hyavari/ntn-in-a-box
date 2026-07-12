package com.ntnbox.android

import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Assert.assertTrue
import org.junit.Test

class NtnJsonTest {
    @Test
    fun parseConditionInCoverage() {
        val json = """
            {"in_coverage":true,"elapsed_sec":2.5,"until_next_transition_sec":12.5,
             "delay_ms":250.0,"jitter_ms":40.0,"loss_pct":8.0,"bandwidth_kbps":16.0}
        """.trimIndent()
        val c = NtnJson.parseCondition(json)
        assertTrue(c.inCoverage)
        assertEquals(2.5, c.elapsedSec, 0.001)
        assertEquals(12.5, c.untilNextTransitionSec, 0.001)
        assertEquals(250.0, c.delayMs!!, 0.001)
        assertEquals(16.0, c.bandwidthKbps!!, 0.001)
    }

    @Test
    fun parseConditionOutOfCoverageOmitsLink() {
        val json = """{"in_coverage":false,"elapsed_sec":10.0,"until_next_transition_sec":100.0}"""
        val c = NtnJson.parseCondition(json)
        assertFalse(c.inCoverage)
        assertNull(c.delayMs)
    }

    @Test
    fun parseCoverageEventKinds() {
        val opened = NtnJson.parseCoverageEvent(
            """{"kind":"window_opened","in_coverage":true,"until_next_transition":14.0}""",
        )
        assertEquals(CoverageKind.WINDOW_OPENED, opened.kind)
        assertTrue(opened.inCoverage)

        val closing = NtnJson.parseCoverageEvent(
            """{"kind":"window_closing","in_coverage":true}""",
        )
        assertEquals(CoverageKind.WINDOW_CLOSING, closing.kind)
    }

    @Test
    fun parseConditionMissingFieldThrows() {
        try {
            NtnJson.parseCondition("""{"in_coverage":true,"elapsed_sec":1.0}""")
            throw AssertionError("expected IllegalArgumentException")
        } catch (e: IllegalArgumentException) {
            assertTrue(e.message!!.contains("until_next_transition_sec"))
        }
    }
}

class ReconnectBackoffTest {
    @Test
    fun increasesThenCaps() {
        val b = ReconnectBackoff(initialMs = 500, maxMs = 2_000)
        assertEquals(500, b.nextDelayMs())
        assertEquals(1_000, b.nextDelayMs())
        assertEquals(2_000, b.nextDelayMs())
        assertEquals(2_000, b.nextDelayMs())
        b.reset()
        assertEquals(500, b.nextDelayMs())
    }
}

class CoverageKindTest {
    @Test
    fun fromWire() {
        assertEquals(CoverageKind.WINDOW_OPENING, CoverageKind.fromWire("window_opening"))
        assertEquals(CoverageKind.UNKNOWN, CoverageKind.fromWire("nope"))
    }
}
