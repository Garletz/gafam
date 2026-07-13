package com.gafam.relay

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat
import java.util.concurrent.Executors

/**
 * Edge inference holder — ONNX Runtime GenAI (Qwen3-0.6B INT4).
 */
class EdgeInferenceService : Service() {

    companion object {
        private const val TAG = "GAFAM_Edge"
        private const val CHANNEL_ID = "gafam_edge"
        private const val NOTIF_ID = 5151

        const val ACTION_WAKE = "com.gafam.relay.edge.WAKE"
        const val ACTION_STOP = "com.gafam.relay.edge.STOP"
        const val EXTRA_RAM_REQUEST_MB = "ram_request_mb"

        const val STATE_IDLE = "idle"
        const val STATE_LOADING = "loading"
        const val STATE_WAKING = "waking"
        const val STATE_AWAKE = "awake"
        const val STATE_INFERRING = "inferring"
        const val STATE_STOPPING = "stopping"
        const val STATE_ERROR = "error"

        private val executor = Executors.newSingleThreadExecutor()

        @Volatile
        var edgeService: String = STATE_IDLE

        @Volatile
        var ramRequestMb: Int = 0

        @Volatile
        var ramReservedMb: Int = 0

        @Volatile
        var statusMessage: String = ""

        @Volatile
        var modelOnDevice: Boolean = false

        @Volatile
        var lastInferJobId: String = ""

        @Volatile
        var lastInferContent: String = ""

        @Volatile
        var lastInferError: String = ""

        @Volatile
        var lastInferLatencyMs: Int = 0

        @Volatile
        private var inferReported: Boolean = true

        fun startWake(context: Context, requestMb: Int) {
            val intent = Intent(context, EdgeInferenceService::class.java).apply {
                action = ACTION_WAKE
                putExtra(EXTRA_RAM_REQUEST_MB, requestMb)
            }
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun requestStop(context: Context) {
            val intent = Intent(context, EdgeInferenceService::class.java).apply {
                action = ACTION_STOP
            }
            context.startService(intent)
        }

        fun runInfer(context: Context, jobId: String, prompt: String, onDone: (() -> Unit)? = null) {
            executor.execute {
                val start = System.currentTimeMillis()
                lastInferJobId = jobId
                lastInferContent = ""
                lastInferError = ""
                inferReported = false
                try {
                    if (!EdgeLlmEngine.isLoaded()) {
                        ensureModelLoaded(context)
                    }
                    edgeService = STATE_INFERRING
                    statusMessage = "Inferring: ${prompt.take(40)}"
                    val answer = EdgeLlmEngine.complete(prompt, maxTokens = 128)
                    lastInferContent = answer
                    lastInferLatencyMs = (System.currentTimeMillis() - start).toInt()
                    statusMessage = "Infer done (${lastInferLatencyMs} ms)"
                    Log.i(TAG, "Infer job=$jobId answer=${answer.take(120)}")
                } catch (e: Exception) {
                    lastInferError = e.message ?: "infer_failed"
                    lastInferLatencyMs = (System.currentTimeMillis() - start).toInt()
                    edgeService = STATE_ERROR
                    statusMessage = "Infer error: ${lastInferError}"
                    Log.e(TAG, "Infer failed job=$jobId", e)
                } finally {
                    if (edgeService == STATE_INFERRING) {
                        edgeService = if (EdgeLlmEngine.isLoaded()) STATE_AWAKE else STATE_IDLE
                    }
                    onDone?.invoke()
                }
            }
        }

        fun takeInferReport(): InferReport? {
            if (inferReported || lastInferJobId.isBlank()) return null
            // Poll loop can sync while inferring — never ship an empty early report to VPC.
            if (edgeService == STATE_INFERRING) return null
            if (lastInferContent.isBlank() && lastInferError.isBlank()) return null
            inferReported = true
            return InferReport(
                jobId = lastInferJobId,
                content = lastInferContent,
                error = lastInferError,
                latencyMs = lastInferLatencyMs
            )
        }

        private fun ensureModelLoaded(context: Context) {
            if (EdgeLlmEngine.isLoaded()) return
            edgeService = STATE_LOADING
            statusMessage = "Downloading Qwen3 ONNX from VPC…"
            EdgeModelDownloader.ensureDownloaded(context.applicationContext) { pct ->
                statusMessage = "Model download $pct%"
            }
            val dir = EdgeModelDownloader.modelDir(context.applicationContext)
            EdgeLlmEngine.load(dir)
            modelOnDevice = true
            ramReservedMb = 520
            edgeService = STATE_AWAKE
            statusMessage = "Qwen3-0.6B ONNX loaded"
        }
    }

    data class InferReport(
        val jobId: String,
        val content: String,
        val error: String,
        val latencyMs: Int
    )

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> handleStop()
            ACTION_WAKE, null -> {
                if (!handleWake(intent)) return START_NOT_STICKY
            }
        }
        return START_STICKY
    }

    private fun handleStop() {
        edgeService = STATE_STOPPING
        statusMessage = "Stopping edge service"
        updateNotification("GAFAM Edge — stopping")
        executor.execute {
            EdgeLlmEngine.unload()
            modelOnDevice = false
            ramReservedMb = 0
            ramRequestMb = 0
            edgeService = STATE_IDLE
            statusMessage = "Edge idle"
            stopForeground(STOP_FOREGROUND_REMOVE)
            stopSelf()
        }
    }

    private fun handleWake(intent: Intent?): Boolean {
        val requested = intent?.getIntExtra(EXTRA_RAM_REQUEST_MB, 512) ?: 512
        val decision = EdgeRamPolicy.resolveWakeBudget(applicationContext, requested)
        if (!decision.ok) {
            edgeService = STATE_ERROR
            ramReservedMb = 0
            ramRequestMb = 0
            statusMessage = decision.message
            Log.w(TAG, "Wake rejected: ${decision.message}")
            return false
        }
        ramRequestMb = decision.effectiveMb
        edgeService = STATE_WAKING
        statusMessage = decision.message
        val notification = buildNotification("GAFAM Edge — waking…")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                NOTIF_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            )
        } else {
            startForeground(NOTIF_ID, notification)
        }

        executor.execute {
            try {
                ensureModelLoaded(applicationContext)
                statusMessage = "${decision.message} — Qwen3 ONNX ready"
                updateNotification("GAFAM Edge — awake (${ramRequestMb} Mo)")
                Log.i(TAG, "Edge awake request=$requested effective=$ramRequestMb")
            } catch (e: Exception) {
                edgeService = STATE_ERROR
                modelOnDevice = false
                ramReservedMb = 0
                statusMessage = "Load failed: ${e.message}"
                Log.e(TAG, "Wake load failed", e)
            }
        }
        return true
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = android.app.NotificationChannel(
                CHANNEL_ID,
                "GAFAM Edge inference",
                android.app.NotificationManager.IMPORTANCE_LOW
            )
            channel.description = "On-device ONNX inference wake/stop"
            val mgr = getSystemService(android.app.NotificationManager::class.java)
            mgr.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(text: String): android.app.Notification {
        val pending = android.app.PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            android.app.PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("GAFAM Edge")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_menu_compass)
            .setContentIntent(pending)
            .setOngoing(true)
            .build()
    }

    private fun updateNotification(text: String) {
        val mgr = getSystemService(android.app.NotificationManager::class.java)
        mgr.notify(NOTIF_ID, buildNotification(text))
    }
}
