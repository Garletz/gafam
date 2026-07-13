package com.gafam.relay

import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.os.Build
import android.os.IBinder
import android.util.Log
import androidx.core.app.NotificationCompat

/**
 * Edge inference holder. Wake/stop with RAM policy from [EdgeRamPolicy].
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
        const val STATE_WAKING = "waking"
        const val STATE_AWAKE = "awake"
        const val STATE_STOPPING = "stopping"
        const val STATE_ERROR = "error"

        @Volatile
        var edgeService: String = STATE_IDLE

        @Volatile
        var ramRequestMb: Int = 0

        @Volatile
        var ramReservedMb: Int = 0

        @Volatile
        var statusMessage: String = ""

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
                ramRequestMb = 0
                updateNotification("GAFAM Edge — stopping")
                stopForeground(STOP_FOREGROUND_REMOVE)
                edgeService = STATE_IDLE
                statusMessage = "Edge idle"
                stopSelf()
            }
            ACTION_WAKE, null -> {
                val requested = intent?.getIntExtra(EXTRA_RAM_REQUEST_MB, 512) ?: 512
                val decision = EdgeRamPolicy.resolveWakeBudget(applicationContext, requested)
                if (!decision.ok) {
                    edgeService = STATE_ERROR
                    ramReservedMb = 0
                    ramRequestMb = 0
                    statusMessage = decision.message
                    Log.w(TAG, "Wake rejected: ${decision.message}")
                    return START_NOT_STICKY
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
                ramReservedMb = 64
                edgeService = STATE_AWAKE
                statusMessage = "${decision.message} — model not loaded (2c-2)"
                updateNotification("GAFAM Edge — awake (${ramRequestMb} Mo)")
                Log.i(TAG, "Edge awake request=$requested effective=$ramRequestMb")
            }
        }
        return START_STICKY
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val channel = android.app.NotificationChannel(
                CHANNEL_ID,
                "GAFAM Edge inference",
                android.app.NotificationManager.IMPORTANCE_LOW
            )
            channel.description = "On-device inference wake/stop"
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
