package com.gafam.relay

import android.app.Activity
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.concurrent.ConcurrentHashMap
import kotlin.concurrent.thread

/**
 * Receives the SmsManager sent-report for outbox messages and reports the
 * outcome to the VPC (`POST /api/auth/sms/status`). The VPC then marks the
 * matching gafam_sms row ("sent"/"failed") and removes the outbox entry.
 *
 * The PendingIntent is registered by RelayForegroundService at send time.
 */
class SmsSentReceiver : BroadcastReceiver() {

    companion object {
        private const val TAG = "GAFAM_Relay"
        const val ACTION_SMS_SENT = "com.gafam.relay.SMS_SENT"

        /** Outbox ids whose final status was already reported (dedup broadcast + fallback). */
        private val reported = ConcurrentHashMap.newKeySet<Int>()

        fun buildSentIntent(context: Context, outboxId: Int, smsId: Int): PendingIntent {
            val intent = Intent(context, SmsSentReceiver::class.java).apply {
                action = ACTION_SMS_SENT
                putExtra("outbox_id", outboxId)
                putExtra("sms_id", smsId)
            }
            return PendingIntent.getBroadcast(
                context,
                outboxId,
                intent,
                PendingIntent.FLAG_IMMUTABLE or PendingIntent.FLAG_UPDATE_CURRENT
            )
        }

        /** Returns true the first time this outbox id is marked as reported. */
        fun markReportedIfNew(outboxId: Int): Boolean = reported.add(outboxId)

        /**
         * Reports the final delivery status of an outbox SMS to the VPC.
         * The status endpoint also deletes the outbox row, so the message is
         * never sent twice.
         */
        fun reportStatus(context: Context, outboxId: Int, smsId: Int, status: String) {
            val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            val apiUrl = prefs.getString("apiUrl", null) ?: return
            val jwtSecret = prefs.getString("jwtSecret", null) ?: return

            thread {
                try {
                    val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/status")
                    val client = ApiClient.getClient(context) ?: return@thread

                    val jsonBody = JSONObject().apply {
                        put("outbox_id", outboxId)
                        put("sms_id", smsId)
                        put("status", status)
                    }
                    val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)

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

                    val request = Request.Builder()
                        .url(spoofedUrl)
                        .post(encryptedPayload.toString().toRequestBody("application/json".toMediaType()))
                        .addHeader("Authorization", "Bearer $jwtSecret")
                        .build()

                    val response = client.newCall(request).execute()
                    Log.d(TAG, "SMS status report ($status) for outbox $outboxId → HTTP ${response.code}")
                    response.close()
                } catch (e: Exception) {
                    Log.e(TAG, "Failed to report SMS status", e)
                } finally {
                    // Keep memory bounded — ids are only useful while the outbox row exists.
                    if (reported.size > 500) reported.clear()
                }
            }
        }
    }

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action != ACTION_SMS_SENT) return
        val outboxId = intent.getIntExtra("outbox_id", 0)
        val smsId = intent.getIntExtra("sms_id", 0)
        if (outboxId == 0) return
        if (!markReportedIfNew(outboxId)) return

        val status = if (resultCode == Activity.RESULT_OK) "sent" else "failed"
        if (status == "failed") {
            Log.w(TAG, "SMS send failed (code $resultCode) for outbox $outboxId")
            LogShipper.event(context, "E", "outbox", "SMS delivery failed (code $resultCode)")
        }
        reportStatus(context.applicationContext, outboxId, smsId, status)
    }
}
