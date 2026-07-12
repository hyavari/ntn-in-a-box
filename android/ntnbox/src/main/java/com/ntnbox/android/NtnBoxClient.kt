package com.ntnbox.android

import kotlinx.coroutines.channels.awaitClose
import kotlinx.coroutines.flow.Flow
import kotlinx.coroutines.flow.callbackFlow
import okhttp3.Call
import okhttp3.Callback
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.Response
import okhttp3.sse.EventSource
import okhttp3.sse.EventSourceListener
import okhttp3.sse.EventSources
import java.io.IOException
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.Executor
import java.util.concurrent.Executors
import java.util.concurrent.ScheduledExecutorService
import java.util.concurrent.ScheduledFuture
import java.util.concurrent.TimeUnit
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicReference

/**
 * Android client for ntnbox SSE coverage events + condition polling.
 *
 * Defaults assume `adb reverse tcp:8080 tcp:8080` and device id `sandbox-0`
 * from `ntnbox run --addr`.
 *
 * Coverage kinds come from SSE only. Condition poll supplies countdown/metrics
 * and does not synthesize coverage kinds.
 */
class NtnBoxClient(
    baseUrl: String = DEFAULT_BASE_URL,
    private val deviceId: String = DEFAULT_DEVICE_ID,
    private val conditionPollMs: Long = DEFAULT_POLL_MS,
    httpClient: OkHttpClient = OkHttpClient(),
    private val leadSec: Double? = null,
) {
    private val root = baseUrl.trimEnd('/')
    private val listeners = CopyOnWriteArrayList<ListenerRegistration>()
    private val started = AtomicBoolean(false)
    private val backoff = ReconnectBackoff()

    /** Finite timeouts for one-shot condition / lookahead GETs. */
    private val pollClient: OkHttpClient = httpClient.newBuilder()
        .connectTimeout(5, TimeUnit.SECONDS)
        .readTimeout(5, TimeUnit.SECONDS)
        .writeTimeout(5, TimeUnit.SECONDS)
        .build()

    /** No read timeout — ntnbox SSE is idle between coverage events (no keepalive). */
    private val sseClient: OkHttpClient = httpClient.newBuilder()
        .readTimeout(0, TimeUnit.MILLISECONDS)
        .build()

    private var scheduler: ScheduledExecutorService? = null
    private var pollFuture: ScheduledFuture<*>? = null
    private var eventSource: EventSource? = null
    private var reconnectFuture: ScheduledFuture<*>? = null
    private val inflightPoll = AtomicReference<Call?>(null)
    private val inflightLookahead = AtomicReference<Call?>(null)

    fun addListener(executor: Executor, listener: NtnBoxListener) {
        listeners.add(ListenerRegistration(executor, listener))
    }

    fun removeListener(listener: NtnBoxListener) {
        listeners.removeAll { it.listener === listener }
    }

    fun start() {
        if (!started.compareAndSet(false, true)) return
        val sched = Executors.newSingleThreadScheduledExecutor { r ->
            Thread(r, "ntnbox-client").apply { isDaemon = true }
        }
        scheduler = sched
        startEventSource(sched)
        pollFuture = sched.scheduleAtFixedRate(
            {
                pollCondition()
                pollLookahead()
            },
            0L,
            conditionPollMs,
            TimeUnit.MILLISECONDS,
        )
    }

    fun stop() {
        if (!started.compareAndSet(true, false)) return
        reconnectFuture?.cancel(false)
        reconnectFuture = null
        pollFuture?.cancel(false)
        pollFuture = null
        inflightPoll.getAndSet(null)?.cancel()
        inflightLookahead.getAndSet(null)?.cancel()
        eventSource?.cancel()
        eventSource = null
        scheduler?.shutdownNow()
        scheduler = null
        backoff.reset()
        dispatchConnection(false)
    }

    /**
     * Flow of coverage updates. Does not call [start]; the client must be started.
     */
    fun coverageFlow(): Flow<Pair<Boolean, CoverageKind>> = callbackFlow {
        val listener = object : NtnBoxListener {
            override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) {
                trySend(inCoverage to kind)
            }

            override fun onCondition(condition: NtnCondition) = Unit

            override fun onConnectionChanged(connected: Boolean) = Unit
        }
        addListener(Executor { it.run() }, listener)
        awaitClose { removeListener(listener) }
    }

    /** Flow of condition snapshots. Does not call [start]. */
    fun conditionFlow(): Flow<NtnCondition> = callbackFlow {
        val listener = object : NtnBoxListener {
            override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) = Unit

            override fun onCondition(condition: NtnCondition) {
                trySend(condition)
            }

            override fun onConnectionChanged(connected: Boolean) = Unit
        }
        addListener(Executor { it.run() }, listener)
        awaitClose { removeListener(listener) }
    }

    /** Flow of lookahead snapshots. Does not call [start]. */
    fun lookaheadFlow(): Flow<NtnLookahead> = callbackFlow {
        val listener = object : NtnBoxListener {
            override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) = Unit

            override fun onCondition(condition: NtnCondition) = Unit

            override fun onLookahead(lookahead: NtnLookahead) {
                trySend(lookahead)
            }

            override fun onConnectionChanged(connected: Boolean) = Unit
        }
        addListener(Executor { it.run() }, listener)
        awaitClose { removeListener(listener) }
    }

    /** Flow of SSE connection up/down. Does not call [start]. */
    fun connectionFlow(): Flow<Boolean> = callbackFlow {
        val listener = object : NtnBoxListener {
            override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) = Unit

            override fun onCondition(condition: NtnCondition) = Unit

            override fun onConnectionChanged(connected: Boolean) {
                trySend(connected)
            }
        }
        addListener(Executor { it.run() }, listener)
        awaitClose { removeListener(listener) }
    }

    private fun startEventSource(sched: ScheduledExecutorService) {
        eventSource?.cancel()
        val request = Request.Builder()
            .url("$root/events")
            .header("Accept", "text/event-stream")
            .build()

        val listener = object : EventSourceListener() {
            override fun onOpen(eventSource: EventSource, response: Response) {
                if (!started.get()) return
                backoff.reset()
                dispatchConnection(true)
            }

            override fun onEvent(
                eventSource: EventSource,
                id: String?,
                type: String?,
                data: String,
            ) {
                if (!started.get()) return
                if (type != null && type != "coverage") return
                try {
                    val payload = NtnJson.parseCoverageEvent(data)
                    dispatchCoverage(payload.inCoverage, payload.kind)
                } catch (_: Exception) {
                    // ignore malformed event
                }
            }

            override fun onClosed(eventSource: EventSource) {
                if (!started.get()) return
                dispatchConnection(false)
                scheduleReconnect(sched)
            }

            override fun onFailure(eventSource: EventSource, t: Throwable?, response: Response?) {
                if (!started.get()) return
                dispatchConnection(false)
                scheduleReconnect(sched)
            }
        }

        eventSource = EventSources.createFactory(sseClient).newEventSource(request, listener)
    }

    private fun scheduleReconnect(sched: ScheduledExecutorService) {
        if (!started.get()) return
        eventSource = null
        val delay = backoff.nextDelayMs()
        reconnectFuture?.cancel(false)
        reconnectFuture = sched.schedule(
            {
                if (started.get()) {
                    startEventSource(sched)
                }
            },
            delay,
            TimeUnit.MILLISECONDS,
        )
    }

    private fun pollCondition() {
        if (!started.get()) return
        inflightPoll.getAndSet(null)?.cancel()
        val request = Request.Builder()
            .url("$root/devices/$deviceId/condition")
            .get()
            .build()
        val call = pollClient.newCall(request)
        inflightPoll.set(call)
        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                inflightPoll.compareAndSet(call, null)
            }

            override fun onResponse(call: Call, response: Response) {
                inflightPoll.compareAndSet(call, null)
                if (!started.get()) return
                response.use { resp ->
                    if (!resp.isSuccessful) return
                    val body = resp.body?.string() ?: return
                    if (!started.get()) return
                    try {
                        val condition = NtnJson.parseCondition(body)
                        dispatchCondition(condition)
                    } catch (_: Exception) {
                        // ignore malformed
                    }
                }
            }
        })
    }

    private fun pollLookahead() {
        if (!started.get()) return
        inflightLookahead.getAndSet(null)?.cancel()
        val lead = leadSec
        val url = if (lead != null && lead > 0) {
            "$root/devices/$deviceId/lookahead?lead_sec=$lead"
        } else {
            "$root/devices/$deviceId/lookahead"
        }
        val request = Request.Builder().url(url).get().build()
        val call = pollClient.newCall(request)
        inflightLookahead.set(call)
        call.enqueue(object : Callback {
            override fun onFailure(call: Call, e: IOException) {
                inflightLookahead.compareAndSet(call, null)
            }

            override fun onResponse(call: Call, response: Response) {
                inflightLookahead.compareAndSet(call, null)
                if (!started.get()) return
                response.use { resp ->
                    if (!resp.isSuccessful) return
                    val body = resp.body?.string() ?: return
                    if (!started.get()) return
                    try {
                        dispatchLookahead(NtnJson.parseLookahead(body))
                    } catch (_: Exception) {
                        // ignore malformed
                    }
                }
            }
        })
    }

    private fun dispatchCoverage(inCoverage: Boolean, kind: CoverageKind) {
        if (!started.get()) return
        for (reg in listeners) {
            reg.executor.execute {
                if (started.get()) {
                    reg.listener.onCoverageChanged(inCoverage, kind)
                }
            }
        }
    }

    private fun dispatchCondition(condition: NtnCondition) {
        if (!started.get()) return
        for (reg in listeners) {
            reg.executor.execute {
                if (started.get()) {
                    reg.listener.onCondition(condition)
                }
            }
        }
    }

    private fun dispatchLookahead(lookahead: NtnLookahead) {
        if (!started.get()) return
        for (reg in listeners) {
            reg.executor.execute {
                if (started.get()) {
                    reg.listener.onLookahead(lookahead)
                }
            }
        }
    }

    private fun dispatchConnection(connected: Boolean) {
        for (reg in listeners) {
            reg.executor.execute {
                reg.listener.onConnectionChanged(connected)
            }
        }
    }

    companion object {
        const val DEFAULT_BASE_URL = "http://127.0.0.1:8080"
        const val DEFAULT_DEVICE_ID = "sandbox-0"
        const val DEFAULT_POLL_MS = 1000L
    }
}
