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
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
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
    private var syncSwitchRef: android.widget.Switch? = null

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        val layout = LinearLayout(this)
        layout.orientation = LinearLayout.VERTICAL
        layout.setPadding(32, 32, 32, 32)
        layout.setBackgroundColor(android.graphics.Color.BLACK)
        
        statusText = TextView(this)
        statusText.textSize = 18f
        statusText.setTextColor(android.graphics.Color.WHITE)
        layout.addView(statusText)

        val scanBtn = Button(this)
        scanBtn.setBackgroundColor(android.graphics.Color.DKGRAY)
        scanBtn.setTextColor(android.graphics.Color.WHITE)
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
        defaultSmsBtn.setBackgroundColor(android.graphics.Color.DKGRAY)
        defaultSmsBtn.setTextColor(android.graphics.Color.WHITE)
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
        authWebBtn.setBackgroundColor(android.graphics.Color.DKGRAY)
        authWebBtn.setTextColor(android.graphics.Color.WHITE)
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
        testSmsBtn.setBackgroundColor(android.graphics.Color.DKGRAY)
        testSmsBtn.setTextColor(android.graphics.Color.WHITE)
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
        smsLogTitle.setTextColor(android.graphics.Color.WHITE)
        smsLogTitle.setTypeface(null, android.graphics.Typeface.BOLD)
        layout.addView(smsLogTitle)

        val syncContactsSwitch = android.widget.Switch(this)
        syncContactsSwitch.text = "Sync Contacts with VPC"
        syncContactsSwitch.setTextColor(android.graphics.Color.WHITE)
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        syncContactsSwitch.isChecked = prefs.getBoolean("contacts_sync_enabled", true)
        syncContactsSwitch.setOnCheckedChangeListener { _, isChecked ->
            prefs.edit().putBoolean("contacts_sync_enabled", isChecked).apply()
            updateVpcSettings("contacts_sync_enabled", if (isChecked) "true" else "false")
            if (isChecked) {
                val apiUrl = prefs.getString("apiUrl", null)
                val jwtSecret = prefs.getString("jwtSecret", null)
                if (apiUrl != null && jwtSecret != null) {
                    syncContacts(apiUrl, jwtSecret)
                }
            }
        }
        layout.addView(syncContactsSwitch)

        // Store a reference to update it from the poller
        syncSwitchRef = syncContactsSwitch

        smsLogText = TextView(this)
        smsLogText.textSize = 13f
        smsLogText.setTextColor(android.graphics.Color.LTGRAY)
        smsLogText.text = "No SMS intercepted yet."
        layout.addView(smsLogText)
        
        setContentView(layout)
        updateStatus()

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECEIVE_SMS) 
            != PackageManager.PERMISSION_GRANTED ||
            ContextCompat.checkSelfPermission(this, Manifest.permission.READ_CONTACTS) 
            != PackageManager.PERMISSION_GRANTED) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.RECEIVE_SMS, Manifest.permission.READ_SMS, Manifest.permission.SEND_SMS, Manifest.permission.INTERNET, Manifest.permission.CAMERA, Manifest.permission.READ_CONTACTS),
                101
            )
        }

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

        // Sync contacts on start if enabled
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl != null && jwtSecret != null) {
            syncContacts(apiUrl, jwtSecret)
        }
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
            val certFingerprint = json.getString("cert_fingerprint")
            
            val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            var deviceId = prefs.getString("deviceId", null)
            if (deviceId == null) {
                deviceId = UUID.randomUUID().toString()
                prefs.edit().putString("deviceId", deviceId).apply()
            }
            
            pairDevice(apiUrl, jwtSecret, deviceId, certFingerprint) { success ->
                runOnUiThread {
                    if (success) {
                        prefs.edit()
                            .putString("apiUrl", apiUrl)
                            .putString("jwtSecret", jwtSecret)
                            .putString("certFingerprint", certFingerprint)
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

    private fun pairDevice(apiUrl: String, token: String, deviceId: String, certFingerprint: String, callback: (Boolean) -> Unit) {
        thread {
            try {
                // Temporarily save prefs so ApiClient can use them
                getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                    .edit()
                    .putString("apiUrl", apiUrl)
                    .putString("certFingerprint", certFingerprint)
                    .apply()

                val client = ApiClient.getClient(this) ?: throw Exception("Failed to init API Client")
                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/gafam/pair-device")

                val payload = JSONObject()
                payload.put("device_name", "Android Relay")
                payload.put("device_id", deviceId)

                val body = payload.toString().toRequestBody("application/json".toMediaType())
                val request = Request.Builder()
                    .url(spoofedUrl)
                    .post(body)
                    .addHeader("Authorization", "Bearer $token")
                    .build()

                val response = client.newCall(request).execute()
                callback(response.isSuccessful)
            } catch (e: Exception) {
                Log.e("GAFAM_Relay", "Pairing error", e)
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

                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/challenge")
                val client = ApiClient.getClient(this@MainActivity) ?: throw Exception("Failed to init API Client")

                val payload = JSONObject()
                payload.put("phone", phone)
                payload.put("challengeTime", challengeTimeStr)
                payload.put("challengeClicks", challengeClicks)

                val body = payload.toString().toRequestBody("application/json".toMediaType())
                val request = Request.Builder()
                    .url(spoofedUrl)
                    .post(body)
                    .addHeader("Authorization", "Bearer $token")
                    .build()

                val response = client.newCall(request).execute()
                val code = response.code
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
                Thread.sleep(1000) // Poll every 1 second
            }
        }
    }

    private fun pollOutbox() {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl == null || jwtSecret == null) return

        try {
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/outbox")
            val client = ApiClient.getClient(this) ?: return

            val request = Request.Builder()
                .url(spoofedUrl)
                .get()
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()

            val response = client.newCall(request).execute()

            // Also poll settings
            val settingsReq = Request.Builder()
                .url(ApiClient.getSpoofedUrl(apiUrl, "/api/settings"))
                .get()
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()
            
            try {
                val setRes = client.newCall(settingsReq).execute()
                if (setRes.isSuccessful) {
                    val setStr = setRes.body?.string() ?: "{}"
                    val setJson = JSONObject(setStr)
                    if (setJson.has("contacts_sync_enabled")) {
                        val isEnabled = setJson.getString("contacts_sync_enabled") == "true"
                        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                        if (prefs.getBoolean("contacts_sync_enabled", true) != isEnabled) {
                            prefs.edit().putBoolean("contacts_sync_enabled", isEnabled).apply()
                            runOnUiThread {
                                syncSwitchRef?.isChecked = isEnabled
                            }
                        }
                    }
                }
            } catch (e: Exception) {}

            if (response.isSuccessful) {
                val responseStr = response.body?.string() ?: return
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
            val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/sms/outbox?id=$id")
            val client = ApiClient.getClient(this) ?: return

            val request = Request.Builder()
                .url(spoofedUrl)
                .delete()
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()

            client.newCall(request).execute()
        } catch (e: Exception) {
            Log.e("GAFAM_Relay", "Error deleting outbox msg", e)
        }
    }

    private fun updateVpcSettings(key: String, value: String) {
        thread {
            try {
                val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                val apiUrl = prefs.getString("apiUrl", null)
                val jwtSecret = prefs.getString("jwtSecret", null)
                if (apiUrl == null || jwtSecret == null) return@thread

                val client = ApiClient.getClient(this) ?: return@thread
                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/settings")
                
                val payload = JSONObject()
                payload.put("key", key)
                payload.put("value", value)

                val body = payload.toString().toRequestBody("application/json".toMediaType())
                val request = Request.Builder()
                    .url(spoofedUrl)
                    .post(body)
                    .addHeader("Authorization", "Bearer $jwtSecret")
                    .build()
                
                client.newCall(request).execute()
            } catch (e: Exception) {
                Log.e("GAFAM_Relay", "Error updating VPC settings", e)
            }
        }
    }

    private fun syncContacts(apiUrl: String, jwtSecret: String) {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        if (!prefs.getBoolean("contacts_sync_enabled", true)) {
            Log.d("GAFAM_Relay", "Contact sync disabled, skipping.")
            return
        }

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.READ_CONTACTS) != PackageManager.PERMISSION_GRANTED) return

        thread {
            try {
                val contacts = org.json.JSONArray()
                val cursor = contentResolver.query(
                    android.provider.ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
                    null, null, null, null
                )
                
                cursor?.use {
                    val nameIdx = it.getColumnIndex(android.provider.ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME)
                    val phoneIdx = it.getColumnIndex(android.provider.ContactsContract.CommonDataKinds.Phone.NUMBER)
                    
                    while (it.moveToNext()) {
                        val name = it.getString(nameIdx) ?: "Unknown"
                        val phone = it.getString(phoneIdx)?.replace(" ", "") ?: ""
                        if (phone.isNotEmpty()) {
                            val contactObj = JSONObject().apply {
                                put("phone_number", phone)
                                put("display_name", name)
                                put("is_verified", 1) // Default local contacts to verified friends
                            }
                            contacts.put(contactObj)
                        }
                    }
                }

                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/gafam/contacts")
                val client = ApiClient.getClient(this) ?: return@thread
                
                val requestBody = contacts.toString().toRequestBody("application/json".toMediaType())
                val request = Request.Builder()
                    .url(spoofedUrl)
                    .post(requestBody)
                    .addHeader("Authorization", "Bearer $jwtSecret")
                    .build()
                
                client.newCall(request).execute()
                Log.d("GAFAM_Relay", "Contacts synced successfully")
            } catch (e: Exception) {
                Log.e("GAFAM_Relay", "Failed to sync contacts", e)
            }
        }
    }
}
