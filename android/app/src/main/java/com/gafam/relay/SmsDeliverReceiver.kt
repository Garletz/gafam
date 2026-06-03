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

class SmsDeliverReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == Telephony.Sms.Intents.SMS_DELIVER_ACTION) {
            val messages = Telephony.Sms.Intents.getMessagesFromIntent(intent)
            for (sms in messages) {
                val sender = sms.originatingAddress ?: "Unknown"
                val body = sms.messageBody ?: ""
                Log.d("GAFAM_Relay", "SMS Deliver Intercepté de $sender : $body")
                
                // Intercept verification SMS
                if (body.startsWith("GAFAM-VFY-")) {
                    val localIntent = Intent("com.gafam.relay.VFY_SMS")
                    localIntent.putExtra("body", body)
                    context.sendBroadcast(localIntent)
                    return
                }

                sendToVpc(context, sender, body)
            }
        }
    }

    private fun sendToVpc(context: Context, sender: String, body: String) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)

        if (apiUrl == null || jwtSecret == null) {
            Log.d("GAFAM_Relay", "Ignoré: l'app n'est pas encore jumelée avec un VPC.")
            return
        }

        thread {
            try {
                val vpcUrl = URL("$apiUrl/api/sms/") 
                val connection = vpcUrl.openConnection() as HttpURLConnection
                connection.requestMethod = "POST"
                connection.setRequestProperty("Content-Type", "application/json")
                connection.setRequestProperty("Authorization", "Bearer $jwtSecret")
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
