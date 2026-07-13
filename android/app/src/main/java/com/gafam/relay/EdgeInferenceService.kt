package com.gafam.relay

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat

/**
 * Edge inference holder (Phase 2c). Wake/stop without GGUF for now — reserves a foreground slot
 * and reports RAM budget consent to the VPC.
 */
class EdgeInferenceService : Service() {

    companion object {
        private const val TAG = "GAFAM_Edge"
        private const val CHANNEL_ID = "gafam_edge"
        private const val NOTIF_ID = 5151

        const val ACTION_WAKE = "com.gafam.relay.edge.WAKE"
        const val ACTION_STOP = "com.gafam.relay.edge.STOP"
        const val EXTRA_RAM_BUDGET_MB = "ram_budget_mb"

        const val STATE_IDLE = "idle"
        const val STATE_WAKING = "waking"
        const val STATE_AWAKE = "awake"
        const val STATE_STOPPING = "stopping"

        @Volatile
        var edgeService: String = STATE_IDLE

        @Volatile
        var ramBudgetMb: Int = 2048

        /** Simulated reservation until llama.cpp loads a model (Phase 2c-2). */
        @Volatile
        var ramReservedMb: Int = 0

        @Volatile
        var statusMessage: String = ""

        fun startWake(context: Context, budgetMb: Int) {
            val intent = Intent(context, EdgeInferenceService::class.java).apply {
                action = ACTION_WAKE
                putExtra(EXTRA_RAM_BUDGET_MB, budgetMb)
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
    }

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createChannel()
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        when (intent?.action) {
            ACTION_STOP -> {
                edgeService = STATE_STOPPING
                statusMessage = "Stopping edge service"
                ramReservedMb = 0
                updateNotification("GAFAM Edge — stopping")
                stopForeground(STOP_FOREGROUND_REMOVE)
                edgeService = STATE_IDLE
                statusMessage = "Edge idle"
                stopSelf()
            }
            ACTION_WAKE, null -> {
                val budget = intent?.getIntExtra(EXTRA_RAM_BUDGET_MB, ramBudgetMb) ?: ramBudgetMb
                ramBudgetMb = budget.coerceIn(512, 4096)
                edgeService = STATE_WAKING
                statusMessage = "Waking edge (budget ${ramBudgetMb} MB)"
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
                // Phase 2c-1: no GGUF load — placeholder reservation for plumbing test.
                ramReservedMb = 64
                edgeService = STATE_AWAKE
                statusMessage = "Edge awake — model not loaded (2c-1)"
                updateNotification("GAFAM Edge — awake (${ramBudgetMb} MB budget)")
                Log.i(TAG, "Edge awake ramBudget=$ramBudgetMb reserved=$ramReservedMb")
            }
        }
        return START_STICKY
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = NotificationChannel(
                CHANNEL_ID,
                "GAFAM Edge inference",
                NotificationManager.IMPORTANCE_LOW
            )
            channel.description = "On-device inference wake/stop"
            val mgr = getSystemService(NotificationManager::class.java)
            mgr.createNotificationChannel(channel)
        }
    }

    private fun buildNotification(text: String): Notification {
        val pending = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_IMMUTABLE
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
        val mgr = getSystemService(NotificationManager::class.java)
        mgr.notify(NOTIF_ID, buildNotification(text))
    }
}
