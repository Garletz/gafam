package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.os.Bundle
import android.util.Log
import android.widget.Button
import android.widget.LinearLayout
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import org.json.JSONObject
import java.net.HttpURLConnection
import java.net.URL
import java.util.UUID
import kotlin.concurrent.thread
import android.app.AlertDialog
import android.content.BroadcastReceiver
import android.content.Intent
import android.content.IntentFilter
import android.telephony.SmsManager
import android.text.InputType
import android.widget.EditText

class MainActivity : AppCompatActivity() {

    private lateinit var statusText: TextView
    private lateinit var smsLogText: TextView
    private val smsHistory = mutableListOf<String>()

    private val barcodeLauncher = registerForActivityResult(ScanContract()) { result ->
        if (result.contents == null) {
            Toast.makeText(this, "Scan cancelled", Toast.LENGTH_LONG).show()
        } else {
            handleScanResult(result.contents)
        }
    }

    private var vfyReceiver: BroadcastReceiver? = null
    private var smsUiReceiver: BroadcastReceiver? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        val layout = LinearLayout(this)
        layout.orientation = LinearLayout.VERTICAL
        layout.setPadding(32, 32, 32, 32)
        
        statusText = TextView(this)
        statusText.textSize = 18f
        layout.addView(statusText)

        val scanBtn = Button(this)
        scanBtn.text = "Scan VPC QR Code"
        scanBtn.setOnClickListener {
            val options = ScanOptions()
            options.setDesiredBarcodeFormats(ScanOptions.QR_CODE)
            options.setPrompt("Scan the GAFAM VPC QR Code")
            options.setBeepEnabled(true)
            options.setOrientationLocked(true)
            options.setCaptureActivity(CustomScannerActivity::class.java)
            barcodeLauncher.launch(options)
        }
        layout.addView(scanBtn)
        
        val defaultSmsBtn = Button(this)
        defaultSmsBtn.text = "Set as Default SMS App"
        defaultSmsBtn.setOnClickListener {
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.Q) {
                val roleManager = getSystemService(android.app.role.RoleManager::class.java)
                if (roleManager?.isRoleAvailable(android.app.role.RoleManager.ROLE_SMS) == true) {
                    val intent = roleManager.createRequestRoleIntent(android.app.role.RoleManager.ROLE_SMS)
                    startActivityForResult(intent, 102)
                }
            } else {
                val intent = android.content.Intent(android.provider.Telephony.Sms.Intents.ACTION_CHANGE_DEFAULT)
                intent.putExtra(android.provider.Telephony.Sms.Intents.EXTRA_PACKAGE_NAME, packageName)
                startActivityForResult(intent, 102)
            }
        }
        layout.addView(defaultSmsBtn)

        val authWebBtn = Button(this)
        authWebBtn.text = "Authorize Web Login"
        authWebBtn.setOnClickListener {
            val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            val apiUrl = prefs.getString("apiUrl", null)
            val token = prefs.getString("jwtSecret", null)
            if (apiUrl == null || token == null) {
                Toast.makeText(this, "Not paired with a VPC yet", Toast.LENGTH_LONG).show()
                return@setOnClickListener
            }
            generateAndSendChallenge(apiUrl, token)
        }
        layout.addView(authWebBtn)

        val testSmsBtn = Button(this)
        testSmsBtn.text = "Send Test SMS to Myself"
        testSmsBtn.setOnClickListener {
            val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            val phone = prefs.getString("myPhoneNumber", null)
            if (phone == null) {
                Toast.makeText(this, "No phone number registered.", Toast.LENGTH_SHORT).show()
                return@setOnClickListener
            }
            try {
                val smsManager = SmsManager.getDefault()
                val testMessage = "GAFAM Test SMS - ${System.currentTimeMillis()}"
                smsManager.sendTextMessage(phone, null, testMessage, null, null)
                Toast.makeText(this, "Test SMS sent to $phone", Toast.LENGTH_SHORT).show()
            } catch (e: Exception) {
                Toast.makeText(this, "Failed to send SMS.", Toast.LENGTH_SHORT).show()
            }
        }
        layout.addView(testSmsBtn)

        val smsLogTitle = TextView(this)
        smsLogTitle.text = "\nRecent Intercepted SMS:"
        smsLogTitle.textSize = 16f
        smsLogTitle.setTypeface(null, android.graphics.Typeface.BOLD)
        layout.addView(smsLogTitle)

        smsLogText = TextView(this)
        smsLogText.textSize = 13f
        smsLogText.text = "No SMS intercepted yet."
        layout.addView(smsLogText)
        
        setContentView(layout)
        updateStatus()

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECEIVE_SMS) 
            != PackageManager.PERMISSION_GRANTED) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.RECEIVE_SMS, Manifest.permission.READ_SMS, Manifest.permission.SEND_SMS, Manifest.permission.INTERNET, Manifest.permission.CAMERA),
                101
            )
        }

        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        if (prefs.getString("myPhoneNumber", null) == null) {
            promptForPhoneNumber()
        }

        // Setup UI Receiver
        smsUiReceiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                val sender = intent.getStringExtra("sender") ?: "Unknown"
                val body = intent.getStringExtra("body") ?: ""
                
                smsHistory.add(0, "From: $sender\n$body\n")
                if (smsHistory.size > 10) smsHistory.removeAt(smsHistory.size - 1)
                
                smsLogText.text = smsHistory.joinToString("\n---\n")
            }
        }
        val uiFilter = IntentFilter("com.gafam.relay.NEW_SMS")
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(smsUiReceiver, uiFilter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            registerReceiver(smsUiReceiver, uiFilter)
        }

        startOutboxPoller()
    }

    override fun onDestroy() {
        super.onDestroy()
        vfyReceiver?.let { unregisterReceiver(it) }
        smsUiReceiver?.let { unregisterReceiver(it) }
    }

    private fun updateStatus() {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val url = prefs.getString("apiUrl", null)
        val phone = prefs.getString("myPhoneNumber", "Not Set")
        if (url != null) {
            statusText.text = "Relay Agent is ACTIVE\n\nPhone: $phone\nConnected to:\n$url\n\nWaiting for SMS..."
        } else {
            statusText.text = "Relay Agent is INACTIVE\nPhone: $phone\n\nPlease scan a VPC QR Code to connect."
        }
    }

    private fun promptForPhoneNumber() {
        val input = EditText(this)
        input.inputType = InputType.TYPE_CLASS_PHONE
        input.hint = "Ex: 0611223344"

        AlertDialog.Builder(this)
            .setTitle("Enter Your Phone Number")
            .setMessage("We need to verify your phone number via a self-SMS to link it securely.")
            .setView(input)
            .setCancelable(false)
            .setPositiveButton("Verify") { _, _ ->
                val phone = input.text.toString().trim()
                if (phone.isNotEmpty()) {
                    startSelfSmsVerification(phone)
                } else {
                    promptForPhoneNumber()
                }
            }
            .show()
    }

    private fun startSelfSmsVerification(phone: String) {
        val secretCode = "GAFAM-VFY-${(1000..9999).random()}"
        statusText.text = "⏳ Verifying phone number via self-SMS...\nPlease wait."

        // Register temporary receiver
        val filter = IntentFilter("com.gafam.relay.VFY_SMS")
        vfyReceiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                val body = intent.getStringExtra("body") ?: ""
                if (body.contains(secretCode)) {
                    Log.d("GAFAM_Relay", "Self-SMS Verification Success!")
                    getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                        .edit().putString("myPhoneNumber", phone).apply()
                    Toast.makeText(this@MainActivity, "Phone Verified!", Toast.LENGTH_LONG).show()
                    updateStatus()
                    context.unregisterReceiver(this)
                    vfyReceiver = null
                }
            }
        }
        if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
            registerReceiver(vfyReceiver, filter, Context.RECEIVER_NOT_EXPORTED)
        } else {
            registerReceiver(vfyReceiver, filter)
        }

        // Send SMS to self
        try {
            val smsManager = SmsManager.getDefault()
            smsManager.sendTextMessage(phone, null, secretCode, null, null)
            Toast.makeText(this, "Verification SMS sent...", Toast.LENGTH_SHORT).show()
        } catch (e: Exception) {
            Log.e("GAFAM_Relay", "Error sending verification SMS", e)
            Toast.makeText(this, "Failed to send SMS. Ensure permissions are granted.", Toast.LENGTH_LONG).show()
            promptForPhoneNumber()
        }
    }

    private fun handleScanResult(contents: String) {
        statusText.text = "✅ QR Code Scanned!\n\nConnecting to VPC and verifying secure handshake..."
        
        try {
            val json = JSONObject(contents)
            val apiUrl = json.getString("url")
            val jwtSecret = json.getString("token")
            
            val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            var deviceId = prefs.getString("deviceId", null)
            if (deviceId == null) {
                deviceId = UUID.randomUUID().toString()
                prefs.edit().putString("deviceId", deviceId).apply()
            }
            
            pairDevice(apiUrl, jwtSecret, deviceId) { success ->
                runOnUiThread {
                    if (success) {
                        prefs.edit()
                            .putString("apiUrl", apiUrl)
                            .putString("jwtSecret", jwtSecret)
                            .apply()
                        statusText.text = "🎉 Successfully Paired!\n\nRelay Agent is ACTIVE\n\nConnected to:\n$apiUrl\n\nWaiting for SMS..."
                        Toast.makeText(this, "VPC Connection Secured", Toast.LENGTH_LONG).show()
                    } else {
                        statusText.text = "❌ Pairing Failed.\n\nCould not reach the VPC or invalid token.\nPlease check your network or try scanning again."
                        Toast.makeText(this, "Network or Auth Error", Toast.LENGTH_LONG).show()
                    }
                }
            }
        } catch (e: Exception) {
            Log.e("GAFAM", "QR Parse Error", e)
            statusText.text = "❌ Invalid QR Code format.\n\nPlease scan a valid GAFAM VPC QR Code."
            Toast.makeText(this, "Invalid QR Code", Toast.LENGTH_LONG).show()
        }
    }

    private fun pairDevice(apiUrl: String, token: String, deviceId: String, callback: (Boolean) -> Unit) {
        thread {
            try {
                val url = URL("$apiUrl/api/gafam/pair-device")
                val conn = url.openConnection() as HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "application/json")
                conn.setRequestProperty("Authorization", "Bearer $token")
                conn.doOutput = true
                
                val payload = JSONObject()
                payload.put("device_name", "Android Relay")
                payload.put("device_id", deviceId)
                
                conn.outputStream.write(payload.toString().toByteArray())
                
                val code = conn.responseCode
                Log.d("GAFAM", "Pairing response code: $code")
                callback(code in 200..299)
            } catch (e: Exception) {
                Log.e("GAFAM", "Pairing Network Error", e)
                callback(false)
            }
        }
    }

    private fun generateAndSendChallenge(apiUrl: String, token: String) {
        val calendar = java.util.Calendar.getInstance()
        calendar.add(java.util.Calendar.MINUTE, 2 + (Math.random() * 4).toInt())
        val hour = calendar.get(java.util.Calendar.HOUR_OF_DAY)
        val minute = calendar.get(java.util.Calendar.MINUTE)
        val challengeTimeStr = String.format("%02d%02d", hour, minute)
        val displayTime = String.format("%02d:%02d", hour, minute)
        
        val challengeClicks = 1 + (Math.random() * 8).toInt()

        statusText.text = "🔐 Programming Challenge...\nTime: $displayTime\nImpulsions: $challengeClicks"

        thread {
            try {
                val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                val phone = prefs.getString("myPhoneNumber", "")

                val url = URL("$apiUrl/api/auth/challenge")
                val conn = url.openConnection() as HttpURLConnection
                conn.requestMethod = "POST"
                conn.setRequestProperty("Content-Type", "application/json")
                conn.setRequestProperty("Authorization", "Bearer $token")
                conn.doOutput = true

                val payload = JSONObject()
                payload.put("phone", phone)
                payload.put("challengeTime", challengeTimeStr)
                payload.put("challengeClicks", challengeClicks)

                conn.outputStream.write(payload.toString().toByteArray())

                val code = conn.responseCode
                runOnUiThread {
                    if (code in 200..299) {
                        val alertMessage = "Rendez-vous à $displayTime — $challengeClicks impulsions"
                        AlertDialog.Builder(this@MainActivity)
                            .setTitle("Challenge Programmé")
                            .setMessage("Saisissez $displayTime sur gafam.cloud et préparez-vous à cliquer $challengeClicks fois à l'heure pile.")
                            .setPositiveButton("OK", null)
                            .show()
                            
                        statusText.text = "✅ Challenge Prêt!\n$alertMessage\n\nAttendez l'heure sur le navigateur."
                    } else {
                        statusText.text = "❌ Failed to program challenge. HTTP $code"
                        Toast.makeText(this@MainActivity, "Failed. Is VPC reachable?", Toast.LENGTH_LONG).show()
                    }
                }
            } catch (e: Exception) {
                Log.e("GAFAM", "Challenge auth error", e)
                runOnUiThread {
                    statusText.text = "❌ Network error during challenge creation."
                    Toast.makeText(this@MainActivity, "Failed: " + e.message, Toast.LENGTH_LONG).show()
                }
            }
        }
    }

    private var isPollingOutbox = false

    private fun startOutboxPoller() {
        if (isPollingOutbox) return
        isPollingOutbox = true
        
        thread {
            while (isPollingOutbox) {
                pollOutbox()
                Thread.sleep(10000) // Poll every 10 seconds
            }
        }
    }

    private fun pollOutbox() {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl == null || jwtSecret == null) return

        try {
            val url = URL("$apiUrl/api/auth/sms/outbox")
            val connection = url.openConnection() as HttpURLConnection
            connection.requestMethod = "GET"
            connection.setRequestProperty("Authorization", "Bearer $jwtSecret")
            connection.connectTimeout = 5000
            connection.readTimeout = 5000

            if (connection.responseCode == 200) {
                val responseStr = connection.inputStream.bufferedReader().readText()
                val payload = JSONObject(responseStr)
                val encryptedData = payload.getString("encrypted_data")
                val ivStr = payload.getString("iv")

                // Decrypt
                val digest = java.security.MessageDigest.getInstance("SHA-256")
                val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
                val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")

                val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
                val iv = android.util.Base64.decode(ivStr, android.util.Base64.DEFAULT)
                val ciphertext = android.util.Base64.decode(encryptedData, android.util.Base64.DEFAULT)
                val gcmSpec = javax.crypto.spec.GCMParameterSpec(128, iv)
                cipher.init(javax.crypto.Cipher.DECRYPT_MODE, secretKey, gcmSpec)
                
                val plaintext = cipher.doFinal(ciphertext)
                val outboxArray = org.json.JSONArray(String(plaintext, Charsets.UTF_8))

                for (i in 0 until outboxArray.length()) {
                    val msg = outboxArray.getJSONObject(i)
                    val id = msg.getInt("id")
                    val recipient = msg.getString("recipient")
                    val body = msg.getString("body")

                    // Send SMS via Android telephony
                    try {
                        val smsManager = SmsManager.getDefault()
                        smsManager.sendTextMessage(recipient, null, body, null, null)
                        Log.d("GAFAM_Relay", "Sent remote SMS to $recipient")
                    } catch (e: Exception) {
                        Log.e("GAFAM_Relay", "Failed to send SMS to $recipient", e)
                    }

                    // Delete from Outbox
                    deleteFromOutbox(apiUrl, jwtSecret, id)
                }
            }
        } catch (e: Exception) {
            // Ignore polling errors
        }
    }

    private fun deleteFromOutbox(apiUrl: String, jwtSecret: String, id: Int) {
        try {
            val url = URL("$apiUrl/api/auth/sms/outbox?id=$id")
            val connection = url.openConnection() as HttpURLConnection
            connection.requestMethod = "DELETE"
            connection.setRequestProperty("Authorization", "Bearer $jwtSecret")
            connection.responseCode 
        } catch (e: Exception) {
            Log.e("GAFAM_Relay", "Error deleting outbox msg", e)
        }
    }
}
