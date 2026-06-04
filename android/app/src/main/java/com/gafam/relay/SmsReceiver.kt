package com.gafam.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.provider.Telephony
import android.util.Log
import java.net.HttpURLConnection
import java.net.URL
import kotlin.concurrent.thread
import org.json.JSONObject

class SmsReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Telephony.Sms.Intents.SMS_RECEIVED_ACTION) {
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
            
            Log.d("GAFAM_Relay", "SMS Intercepté de $sender : $body")

            // Intercept verification SMS
            if (body.startsWith("GAFAM-VFY-")) {
                val localIntent = Intent("com.gafam.relay.VFY_SMS")
                localIntent.putExtra("body", body)
                context.sendBroadcast(localIntent)
                pendingResult.finish()
                return
            }

            // Broadcast to MainActivity UI
            val uiIntent = Intent("com.gafam.relay.NEW_SMS")
            uiIntent.putExtra("sender", sender)
            uiIntent.putExtra("body", body)
            context.sendBroadcast(uiIntent)
            
            // Envoi en arrière-plan vers le VPC
            sendToVpc(context, sender, body, pendingResult)
        }
    }

    private fun sendToVpc(context: Context, sender: String, body: String, pendingResult: PendingResult?) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)

        if (apiUrl == null || jwtSecret == null) {
            Log.d("GAFAM_Relay", "Ignoré: l'app n'est pas encore jumelée avec un VPC.")
            pendingResult?.finish()
            return
        }

        thread {
            try {
                val vpcUrl = URL("$apiUrl/api/sms/") 
                val connection = vpcUrl.openConnection() as HttpURLConnection
                connection.connectTimeout = 5000
                connection.readTimeout = 5000
                connection.requestMethod = "POST"
                connection.setRequestProperty("Content-Type", "application/json")
                connection.setRequestProperty("Authorization", "Bearer $jwtSecret")
                connection.doOutput = true

                val jsonBody = JSONObject().apply {
                    put("sender", sender)
                    put("body", body)
                    put("timestamp", System.currentTimeMillis())
                }

                val plaintext = jsonBody.toString().toByteArray(Charsets.UTF_8)
                
                // Derive key using SHA-256
                val digest = java.security.MessageDigest.getInstance("SHA-256")
                val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
                val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")

                // Encrypt using AES/GCM/NoPadding
                val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
                val iv = ByteArray(12)
                java.security.SecureRandom().nextBytes(iv)
                val gcmSpec = javax.crypto.spec.GCMParameterSpec(128, iv)
                cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, secretKey, gcmSpec)
                
                val ciphertext = cipher.doFinal(plaintext)
                
                val encryptedPayload = JSONObject().apply {
                    put("encrypted_data", android.util.Base64.encodeToString(ciphertext, android.util.Base64.NO_WRAP))
                    put("iv", android.util.Base64.encodeToString(iv, android.util.Base64.NO_WRAP))
                }

                connection.outputStream.use { os ->
                    val input = encryptedPayload.toString().toByteArray(Charsets.UTF_8)
                    os.write(input, 0, input.size)
                }

                val responseCode = connection.responseCode
                Log.d("GAFAM_Relay", "Réponse VPC: $responseCode")
            } catch (e: Exception) {
                Log.e("GAFAM_Relay", "Erreur d'envoi VPC", e)
            } finally {
                pendingResult?.finish()
            }
        }
    }
}
