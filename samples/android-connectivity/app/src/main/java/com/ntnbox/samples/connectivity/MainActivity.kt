package com.ntnbox.samples.connectivity

import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.TextView
import androidx.appcompat.app.AppCompatActivity
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.atomic.AtomicInteger

/**
 * Connectivity-only NTN demo: poll a URL, queue on failure, drain on success.
 * UI copy stays "online / offline / retrying" — no companion library yet.
 */
class MainActivity : AppCompatActivity() {
    private val queue = OfflineQueue()
    private val handler = Handler(Looper.getMainLooper())
    private val executor = Executors.newSingleThreadExecutor()
    private val msgCounter = AtomicInteger(0)

    private lateinit var statusText: TextView
    private lateinit var detailText: TextView

    private val targetUrl: String by lazy {
        intent?.getStringExtra(EXTRA_URL)
            ?: System.getenv("TARGET_URL")
            ?: DEFAULT_URL
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
        detailText = findViewById(R.id.detailText)
        updateUi("starting…", "target=$targetUrl\nqueue=0")
        handler.post(pollRunnable)
    }

    override fun onDestroy() {
        handler.removeCallbacks(pollRunnable)
        executor.shutdownNow()
        super.onDestroy()
    }

    private fun maybePoll() {
        val now = System.currentTimeMillis()
        if (!queue.shouldAttempt(now)) {
            updateUi(
                "offline / retrying",
                "backoff ${queue.backoffRemainingMs(now)}ms\nqueue=${queue.size}\ntarget=$targetUrl",
            )
            return
        }

        val pendingLabel = "msg-${msgCounter.incrementAndGet()}"
        executor.execute {
            val result = httpGet(targetUrl)
            handler.post {
                if (result.ok) {
                    val flushed = queue.drain()
                    queue.onSuccess()
                    updateUi(
                        "online",
                        "http ${result.code} in ${result.latencyMs}ms\n" +
                            "flushed ${flushed.size} queued item(s)\n" +
                            "target=$targetUrl",
                    )
                } else {
                    queue.enqueue(pendingLabel)
                    queue.onFailure()
                    updateUi(
                        "offline / retrying",
                        "error: ${result.error}\nqueue=${queue.size}\n" +
                            "failures=${queue.failureCount}\ntarget=$targetUrl",
                    )
                }
            }
        }
    }

    private fun updateUi(status: String, detail: String) {
        statusText.text = status
        detailText.text = detail
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
