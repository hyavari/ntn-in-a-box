package com.ntnbox.android

import java.util.concurrent.Executor

/**
 * Listener for NTN coverage / condition updates (SatelliteManager-like callbacks).
 */
interface NtnBoxListener {
    fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind)

    fun onCondition(condition: NtnCondition)

    fun onLookahead(lookahead: NtnLookahead) {}

    /** SSE session connected (true) or dropped / stopped (false). */
    fun onConnectionChanged(connected: Boolean)
}

internal data class ListenerRegistration(
    val executor: Executor,
    val listener: NtnBoxListener,
)
