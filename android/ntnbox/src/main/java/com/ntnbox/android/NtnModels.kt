package com.ntnbox.android

/**
 * Current NTN link/coverage snapshot from GET /devices/{id}/condition.
 */
data class NtnCondition(
    val inCoverage: Boolean,
    val elapsedSec: Double,
    val untilNextTransitionSec: Double,
    val delayMs: Double? = null,
    val jitterMs: Double? = null,
    val lossPct: Double? = null,
    val bandwidthKbps: Double? = null,
)

/**
 * Prediction snapshot from GET /devices/{id}/lookahead.
 */
data class NtnLookahead(
    val inCoverage: Boolean,
    val untilNextTransitionSec: Double,
    val nextOpenAt: String? = null,
    val nextCloseAt: String? = null,
    val nextWindowDurationSec: Double? = null,
    val effectiveLookaheadSec: Double,
    val maxElevationDeg: Double? = null,
)

/**
 * Coverage event kinds from SSE event: coverage (matches ntnbox bus).
 */
enum class CoverageKind {
    WINDOW_OPENING,
    WINDOW_OPENED,
    WINDOW_CLOSING,
    WINDOW_CLOSED,
    UNKNOWN,
    ;

    companion object {
        fun fromWire(kind: String?): CoverageKind = when (kind) {
            "window_opening" -> WINDOW_OPENING
            "window_opened" -> WINDOW_OPENED
            "window_closing" -> WINDOW_CLOSING
            "window_closed" -> WINDOW_CLOSED
            else -> UNKNOWN
        }
    }
}
