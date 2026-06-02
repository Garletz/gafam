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
            val messages = Telephony.Sms.Intents.getMessagesFromIntent(intent)
            for (sms in messages) {
                val sender = sms.originatingAddress ?: "Unknown"
                val body = sms.messageBody ?: ""
                
                Log.d("GAFAM_Relay", "SMS Intercepté de $sender : $body")
                
                // Envoi en arrière-plan vers le VPC
                sendToVpc(sender, body)
            }
        }
    }

    private fun sendToVpc(sender: String, body: String) {
        // Lance le réseau dans un thread séparé (interdit sur le main thread)
        thread {
            try {
                // TODO: L'adresse IP sera configurable plus tard via l'interface Kotlin
                val vpcUrl = URL("http://10.0.2.2:5150/api/sms") 
                val connection = vpcUrl.openConnection() as HttpURLConnection
                connection.requestMethod = "POST"
                connection.setRequestProperty("Content-Type", "application/json")
                connection.setRequestProperty("Authorization", "Bearer TEST_JWT_SECRET")
                connection.doOutput = true

                val jsonBody = JSONObject().apply {
                    put("sender", sender)
                    put("body", body)
                    put("timestamp", System.currentTimeMillis())
                }

                connection.outputStream.use { os ->
                    val input = jsonBody.toString().toByteArray(Charsets.UTF_8)
                    os.write(input, 0, input.size)
                }

                val responseCode = connection.responseCode
                Log.d("GAFAM_Relay", "Réponse VPC: $responseCode")
                
            } catch (e: Exception) {
                Log.e("GAFAM_Relay", "Erreur d'envoi VPC", e)
            }
        }
    }
}
