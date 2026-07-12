package com.ntnbox.samples.connectivity

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import com.ntnbox.android.CoverageKind
import com.ntnbox.android.NtnBoxClient
import com.ntnbox.android.NtnBoxListener
import com.ntnbox.android.NtnCondition
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicInteger

/**
 * NTN demo: HTTP retry/queue plus ntnbox companion coverage countdown.
 */
class MainActivity : AppCompatActivity() {
    private val queue = OfflineQueue()
    private val handler = Handler(Looper.getMainLooper())
    private val executor = Executors.newSingleThreadExecutor()
    private val msgCounter = AtomicInteger(0)
    private val httpInFlight = AtomicBoolean(false)

    private lateinit var statusText: TextView
    private lateinit var ntnText: TextView
    private lateinit var detailText: TextView

    private var lastHttpStatus: String = "starting…"
    private var lastHttpDetail: String = ""
    private var lastNtnLine: String = "ntnbox: connecting…"
    private var ntnConnected: Boolean = false

    private val ntnClient = NtnBoxClient()
    private val ntnListener = object : NtnBoxListener {
        override fun onCoverageChanged(inCoverage: Boolean, kind: CoverageKind) {
            handler.post {
                if (inCoverage && kind == CoverageKind.WINDOW_OPENED) {
                    val flushed = queue.drain()
                    queue.onSuccess() // allow HTTP loop to retry immediately
                    if (flushed.isNotEmpty()) {
                        lastHttpDetail =
                            "flushed ${flushed.size} queued item(s) on coverage\n" + lastHttpDetail
                    }
                }
                render()
            }
        }

        override fun onCondition(condition: NtnCondition) {
            handler.post {
                val phase = if (condition.inCoverage) "in coverage" else "out of coverage"
                val until = condition.untilNextTransitionSec.toInt()
                lastNtnLine = if (condition.inCoverage) {
                    "ntnbox: $phase · next transition in ${until}s"
                } else {
                    "ntnbox: $phase · available in ${until}s"
                }
                render()
            }
        }

        override fun onConnectionChanged(connected: Boolean) {
            handler.post {
                ntnConnected = connected
                if (!connected) {
                    lastNtnLine =
                        "ntnbox: SSE disconnected (is ntnbox run --addr up? adb reverse?)"
                }
                render()
            }
        }
    }

    private val targetUrl: String by lazy {
        intent?.getStringExtra(EXTRA_URL) ?: DEFAULT_URL
    }

    private val pollRunnable = object : Runnable {
        override fun run() {
            maybePoll()
            handler.postDelayed(this, POLL_EVERY_MS)
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(R.layout.activity_main)
        statusText = findViewById(R.id.statusText)
        ntnText = findViewById(R.id.ntnText)
        detailText = findViewById(R.id.detailText)
        lastHttpDetail = "target=$targetUrl\nqueue=0"
        render()
        ntnClient.addListener({ r -> handler.post(r) }, ntnListener)
        ntnClient.start()
        handler.post(pollRunnable)
    }

    override fun onDestroy() {
        handler.removeCallbacks(pollRunnable)
        ntnClient.removeListener(ntnListener)
        ntnClient.stop()
        executor.shutdownNow()
        super.onDestroy()
    }

    private fun maybePoll() {
        val now = System.currentTimeMillis()
        if (!queue.shouldAttempt(now)) {
            lastHttpStatus = "offline / retrying"
            lastHttpDetail =
                "backoff ${queue.backoffRemainingMs(now)}ms\nqueue=${queue.size}\ntarget=$targetUrl"
            render()
            return
        }
        if (!httpInFlight.compareAndSet(false, true)) {
            return
        }

        val pendingLabel = "msg-${msgCounter.incrementAndGet()}"
        executor.execute {
            val result = httpGet(targetUrl)
            handler.post {
                httpInFlight.set(false)
                if (result.ok) {
                    val flushed = queue.drain()
                    queue.onSuccess()
                    lastHttpStatus = "online"
                    lastHttpDetail =
                        "http ${result.code} in ${result.latencyMs}ms\n" +
                            "flushed ${flushed.size} queued item(s)\n" +
                            "target=$targetUrl"
                } else {
                    queue.enqueue(pendingLabel)
                    queue.onFailure()
                    lastHttpStatus = "offline / retrying"
                    lastHttpDetail =
                        "error: ${result.error}\nqueue=${queue.size}\n" +
                            "failures=${queue.failureCount}\ntarget=$targetUrl"
                }
                render()
            }
        }
    }

    private fun render() {
        statusText.text = lastHttpStatus
        ntnText.text = lastNtnLine
        detailText.text = lastHttpDetail + if (ntnConnected) "\nsse=connected" else "\nsse=…"
    }

    private data class HttpResult(
        val ok: Boolean,
        val code: Int = 0,
        val latencyMs: Long = 0,
        val error: String = "",
    )

    private fun httpGet(url: String): HttpResult {
        val started = System.currentTimeMillis()
        return try {
            val conn = (URL(url).openConnection() as HttpURLConnection).apply {
                connectTimeout = 5_000
                readTimeout = 5_000
                requestMethod = "GET"
                instanceFollowRedirects = true
            }
            try {
                val code = conn.responseCode
                val latency = System.currentTimeMillis() - started
                if (code in 200..399) {
                    HttpResult(ok = true, code = code, latencyMs = latency)
                } else {
                    HttpResult(ok = false, code = code, latencyMs = latency, error = "HTTP $code")
                }
            } finally {
                conn.disconnect()
            }
        } catch (e: Exception) {
            HttpResult(
                ok = false,
                latencyMs = System.currentTimeMillis() - started,
                error = e.javaClass.simpleName + ": " + (e.message ?: "failed"),
            )
        }
    }

    companion object {
        const val EXTRA_URL = "target_url"
        const val DEFAULT_URL = "https://example.com"
        private const val POLL_EVERY_MS = 2_000L
    }
}
