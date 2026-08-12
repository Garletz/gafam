package com.gafam.relay

import android.content.BroadcastReceiver
import android.content.ContentValues
import android.content.Context
import android.content.Intent
import android.net.Uri
import android.provider.Telephony
import android.util.Log
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import kotlin.concurrent.thread

class SmsDeliverReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Telephony.Sms.Intents.SMS_DELIVER_ACTION) {
            val pendingResult = goAsync()
            val messages = Telephony.Sms.Intents.getMessagesFromIntent(intent)

            if (messages.isEmpty()) {
                pendingResult.finish()
                return
            }

            val sender = messages[0].originatingAddress ?: "Unknown"
            val bodyBuilder = StringBuilder()
            for (sms in messages) {
                bodyBuilder.append(sms.messageBody ?: "")
            }
            val body = bodyBuilder.toString()
            val ts = if (messages[0].timestampMillis > 0) messages[0].timestampMillis else System.currentTimeMillis()

            Log.d("GAFAM_Relay", "SMS Deliver Intercepté de $sender : $body")
            LogShipper.event(context, "I", "sms", "Delivered SMS from $sender (${body.length} chars)")
            RelayForegroundService.start(context.applicationContext)

            try {
                val values = ContentValues().apply {
                    put("address", sender)
                    put("body", body)
                    put("date", ts)
                    put("date_sent", ts)
                    put("type", 1)
                    put("read", 0)
                }
                context.contentResolver.insert(Uri.parse("content://sms/inbox"), values)
            } catch (e: Exception) {
                Log.w("GAFAM_Relay", "Failed to write SMS to provider: ${e.message}")
            }

            if (body.startsWith("GAFAM-VFY-")) {
                val localIntent = Intent("com.gafam.relay.VFY_SMS")
                localIntent.putExtra("body", body)
                context.sendBroadcast(localIntent)
                pendingResult.finish()
                return
            }

            sendToVpc(context, sender, body, ts, pendingResult)
        }
    }

    private fun sendToVpc(context: Context, sender: String, body: String, timestamp: Long, pendingResult: PendingResult?) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)

        if (apiUrl == null || jwtSecret == null) {
            Log.d("GAFAM_Relay", "Ignoré: l'app n'est pas encore jumelée avec un VPC.")
            pendingResult?.finish()
            return
        }

        thread {
            val encryptedPayload = buildEncryptedPayload(context, sender, body, timestamp, jwtSecret) ?: run {
                pendingResult?.finish()
                return@thread
            }

            val client = ApiClient.getClient(context) ?: run {
                pendingResult?.finish()
                return@thread
            }

            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/")
            val bodyPayload = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
            val request = Request.Builder()
                .url(spoofedUrl)
                .post(bodyPayload)
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()

            var success = false
            var lastException: Exception? = null
            for (attempt in 0 until 3) {
                if (attempt > 0) {
                    Thread.sleep((2000L * attempt).coerceAtMost(6000L))
                }
                try {
                    val response = client.newCall(request).execute()
                    val code = response.code
                    response.close()
                    if (code in 200..299) {
                        success = true
                        Log.d("GAFAM_Relay", "VPC SMS sync succeeded on attempt ${attempt + 1}")
                        break
                    }
                    Log.w("GAFAM_Relay", "VPC SMS sync HTTP $code on attempt ${attempt + 1}")
                } catch (e: Exception) {
                    lastException = e
                    Log.w("GAFAM_Relay", "VPC SMS sync attempt ${attempt + 1} failed: ${e.message}")
                }
            }

            if (success) {
                flushRetryQueue(context)
            } else {
                enqueueRetry(context, sender, body, timestamp)
                LogShipper.event(context, "E", "sms", "VPC SMS sync failed after 3 retries: ${lastException?.message}")
            }

            pendingResult?.finish()
        }
    }

    private fun buildEncryptedPayload(context: Context, sender: String, body: String, timestamp: Long, jwtSecret: String): JSONObject? {
        try {
            val jsonBody = JSONObject().apply {
                put("sender", sender)
                put("body", body)
                put("timestamp", if (timestamp > 0) timestamp else System.currentTimeMillis())
            }

            val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)

            val digest = java.security.MessageDigest.getInstance("SHA-256")
            val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
            val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")

            val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
            val iv = ByteArray(12)
            java.security.SecureRandom().nextBytes(iv)
            val gcmSpec = javax.crypto.spec.GCMParameterSpec(128, iv)
            cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, secretKey, gcmSpec)

            val ciphertext = cipher.doFinal(plaintext)

            return JSONObject().apply {
                put("encrypted_data", android.util.Base64.encodeToString(ciphertext, android.util.Base64.NO_WRAP))
                put("iv", android.util.Base64.encodeToString(iv, android.util.Base64.NO_WRAP))
            }
        } catch (e: Exception) {
            Log.e("GAFAM_Relay", "Failed to build encrypted payload", e)
            return null
        }
    }

    private fun enqueueRetry(context: Context, sender: String, body: String, timestamp: Long) {
        try {
            val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            val queueJson = prefs.getString("sms_retry_queue", "[]") ?: "[]"
            val queue = org.json.JSONArray(queueJson)
            val item = JSONObject().apply {
                put("sender", sender)
                put("body", body)
                put("timestamp", timestamp)
                put("queued_at", System.currentTimeMillis())
            }
            queue.put(item)
            prefs.edit().putString("sms_retry_queue", queue.toString()).apply()
            Log.d("GAFAM_Relay", "SMS enqueued for retry, queue size: ${queue.length()}")
        } catch (e: Exception) {
            Log.w("GAFAM_Relay", "Failed to enqueue SMS retry: ${e.message}")
        }
    }

    private fun flushRetryQueue(context: Context) {
        thread {
            try {
                val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                val queueJson = prefs.getString("sms_retry_queue", "[]") ?: "[]"
                val queue = org.json.JSONArray(queueJson)
                if (queue.length() == 0) return@thread

                val apiUrl = prefs.getString("apiUrl", null) ?: return@thread
                val jwtSecret = prefs.getString("jwtSecret", null) ?: return@thread
                val client = ApiClient.getClient(context) ?: return@thread
                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/")

                val remaining = org.json.JSONArray()
                for (i in 0 until queue.length()) {
                    val item = queue.getJSONObject(i)
                    val sender = item.getString("sender")
                    val body = item.getString("body")
                    val timestamp = item.optLong("timestamp", System.currentTimeMillis())

                    val encryptedPayload = buildEncryptedPayload(context, sender, body, timestamp, jwtSecret)
                    if (encryptedPayload == null) {
                        remaining.put(item)
                    } else {

                    val bodyPayload = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
                    val request = Request.Builder()
                        .url(spoofedUrl)
                        .post(bodyPayload)
                        .addHeader("Authorization", "Bearer $jwtSecret")
                        .build()

                    try {
                        val response = client.newCall(request).execute()
                        val code = response.code
                        response.close()
                        if (code in 200..299) {
                            Log.d("GAFAM_Relay", "Retry SMS sent successfully for $sender")
                        } else {
                            remaining.put(item)
                            Log.w("GAFAM_Relay", "Retry SMS failed HTTP $code for $sender")
                        }
                    } catch (e: Exception) {
                        remaining.put(item)
                        Log.w("GAFAM_Relay", "Retry SMS exception for $sender: ${e.message}")
                    }
                    } // end encryptedPayload != null
                }
                prefs.edit().putString("sms_retry_queue", remaining.toString()).apply()
                if (remaining.length() > 0) {
                    Log.d("GAFAM_Relay", "SMS retry queue: ${remaining.length()} items remaining")
                }
            } catch (e: Exception) {
                Log.w("GAFAM_Relay", "Failed to flush SMS retry queue: ${e.message}")
            }
        }
    }

    companion object {
        fun flushQueue(context: Context) {
            // Called externally to retry pending queue items
        }
    }
}
