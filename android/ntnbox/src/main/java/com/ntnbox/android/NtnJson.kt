package com.ntnbox.android

/**
 * Pure JSON mappers for ntnbox wire formats (no Android org.json).
 * Intentionally minimal — payloads are small and controlled by ntnbox.
 */
object NtnJson {
    fun parseCondition(json: String): NtnCondition {
        return NtnCondition(
            inCoverage = requireBoolean(json, "in_coverage"),
            elapsedSec = requireDouble(json, "elapsed_sec"),
            untilNextTransitionSec = requireDouble(json, "until_next_transition_sec"),
            delayMs = optionalDouble(json, "delay_ms"),
            jitterMs = optionalDouble(json, "jitter_ms"),
            lossPct = optionalDouble(json, "loss_pct"),
            bandwidthKbps = optionalDouble(json, "bandwidth_kbps"),
        )
    }

    fun parseLookahead(json: String): NtnLookahead {
        return NtnLookahead(
            inCoverage = requireBoolean(json, "in_coverage"),
            untilNextTransitionSec = requireDouble(json, "until_next_transition_sec"),
            nextOpenAt = optionalString(json, "next_open_at"),
            nextCloseAt = optionalString(json, "next_close_at"),
            nextWindowDurationSec = optionalDouble(json, "next_window_duration_sec"),
            effectiveLookaheadSec = requireDouble(json, "effective_lookahead_sec"),
            maxElevationDeg = optionalDouble(json, "max_elevation_deg"),
        )
    }

    data class CoveragePayload(
        val kind: CoverageKind,
        val inCoverage: Boolean,
        val untilNextTransition: Double? = null,
    )

    fun parseCoverageEvent(json: String): CoveragePayload {
        return CoveragePayload(
            kind = CoverageKind.fromWire(optionalString(json, "kind")),
            inCoverage = optionalBoolean(json, "in_coverage") ?: false,
            untilNextTransition = optionalDouble(json, "until_next_transition"),
        )
    }

    fun parseLinkState(json: String): NtnLinkState {
        return NtnLinkState(
            delayMs = requireDouble(json, "delay_ms"),
            jitterMs = requireDouble(json, "jitter_ms"),
            lossPct = requireDouble(json, "loss_pct"),
            bandwidthKbps = requireDouble(json, "bandwidth_kbps"),
            at = optionalString(json, "at"),
        )
    }

    private fun requireBoolean(json: String, key: String): Boolean {
        return optionalBoolean(json, key)
            ?: throw IllegalArgumentException("missing boolean $key")
    }

    private fun requireDouble(json: String, key: String): Double {
        return optionalDouble(json, key)
            ?: throw IllegalArgumentException("missing number $key")
    }

    private fun optionalBoolean(json: String, key: String): Boolean? {
        val re = Regex("\"${Regex.escape(key)}\"\\s*:\\s*(true|false)")
        val m = re.find(json) ?: return null
        return m.groupValues[1] == "true"
    }

    private fun optionalDouble(json: String, key: String): Double? {
        val re = Regex("\"${Regex.escape(key)}\"\\s*:\\s*(-?\\d+(?:\\.\\d+)?(?:[eE][-+]?\\d+)?)")
        val m = re.find(json) ?: return null
        return m.groupValues[1].toDouble()
    }

    private fun optionalString(json: String, key: String): String? {
        val re = Regex("\"${Regex.escape(key)}\"\\s*:\\s*\"([^\"]*)\"")
        val m = re.find(json) ?: return null
        return m.groupValues[1]
    }
}
