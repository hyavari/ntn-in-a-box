package com.ntnbox.android

/**
 * Pure JSON mappers for ntnbox wire formats (no Android org.json).
 * Intentionally minimal — payloads are small and controlled by ntnbox.
 * String fields and arrays respect JSON quoting/escapes.
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
        val deviceId: String? = null,
    )

    fun parseCoverageEvent(json: String): CoveragePayload {
        return CoveragePayload(
            kind = CoverageKind.fromWire(optionalString(json, "kind")),
            inCoverage = optionalBoolean(json, "in_coverage") ?: false,
            untilNextTransition = optionalDouble(json, "until_next_transition"),
            deviceId = optionalString(json, "device_id"),
        )
    }

    fun parseLinkState(json: String): NtnLinkState {
        return NtnLinkState(
            delayMs = requireDouble(json, "delay_ms"),
            jitterMs = requireDouble(json, "jitter_ms"),
            lossPct = requireDouble(json, "loss_pct"),
            bandwidthKbps = requireDouble(json, "bandwidth_kbps"),
            at = optionalString(json, "at"),
            deviceId = optionalString(json, "device_id"),
        )
    }

    fun parseMessage(json: String): NtnMessage {
        return NtnMessage(
            id = optionalString(json, "id") ?: throw IllegalArgumentException("missing id"),
            from = optionalString(json, "from"),
            to = optionalString(json, "to"),
            body = optionalString(json, "body"),
            status = optionalString(json, "status")
                ?: throw IllegalArgumentException("missing status"),
            acceptedAt = optionalString(json, "accepted_at"),
            deliveredAt = optionalString(json, "delivered_at"),
        )
    }

    fun parseMessageList(json: String): List<NtnMessage> {
        val trimmed = json.trim()
        if (trimmed == "[]" || trimmed.isEmpty()) return emptyList()
        if (!trimmed.startsWith("[")) {
            throw IllegalArgumentException("expected JSON array")
        }
        val objs = mutableListOf<String>()
        var depth = 0
        var start = -1
        var inString = false
        var escape = false
        for (i in trimmed.indices) {
            val c = trimmed[i]
            if (inString) {
                if (escape) {
                    escape = false
                } else when (c) {
                    '\\' -> escape = true
                    '"' -> inString = false
                }
                continue
            }
            when (c) {
                '"' -> inString = true
                '{' -> {
                    if (depth == 0) start = i
                    depth++
                }
                '}' -> {
                    depth--
                    if (depth == 0 && start >= 0) {
                        objs.add(trimmed.substring(start, i + 1))
                        start = -1
                    }
                }
            }
        }
        return objs.map { parseMessage(it) }
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
        val keyRe = Regex("\"${Regex.escape(key)}\"\\s*:\\s*\"")
        val m = keyRe.find(json) ?: return null
        return decodeJsonString(json, m.range.last + 1)
    }

    /** Decode a JSON string starting at [start] (first char after opening quote). */
    private fun decodeJsonString(json: String, start: Int): String? {
        val sb = StringBuilder()
        var i = start
        while (i < json.length) {
            val c = json[i]
            when {
                c == '"' -> return sb.toString()
                c == '\\' && i + 1 < json.length -> {
                    when (val esc = json[i + 1]) {
                        '"', '\\', '/' -> {
                            sb.append(esc)
                            i += 2
                        }
                        'n' -> {
                            sb.append('\n')
                            i += 2
                        }
                        'r' -> {
                            sb.append('\r')
                            i += 2
                        }
                        't' -> {
                            sb.append('\t')
                            i += 2
                        }
                        'u' -> {
                            if (i + 5 >= json.length) return null
                            val code = json.substring(i + 2, i + 6).toIntOrNull(16) ?: return null
                            sb.append(code.toChar())
                            i += 6
                        }
                        else -> {
                            sb.append(esc)
                            i += 2
                        }
                    }
                }
                else -> {
                    sb.append(c)
                    i++
                }
            }
        }
        return null
    }
}
