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
    private var edgeCapLabelRef: TextView? = null
    private val uiHandler = android.os.Handler(android.os.Looper.getMainLooper())
    private val statusRefreshRunnable = object : Runnable {
        override fun run() {
            updateStatus()
            uiHandler.postDelayed(this, 2000)
        }
    }

    private fun edgeStatusLine(): String {
        val snap = EdgeRamPolicy.snapshot(this)
        return when (EdgeInferenceService.edgeService) {
            EdgeInferenceService.STATE_AWAKE ->
                "GAFAM Edge: AWAKE (${EdgeInferenceService.ramRequestMb} Mo tâche)\n" +
                    "Cap tel ${snap.capMb} Mo · dispo ${snap.availMb} Mo · max ${snap.maxDeliverableMb} Mo\n" +
                    EdgeInferenceService.statusMessage
            EdgeInferenceService.STATE_ERROR ->
                "GAFAM Edge: erreur — ${EdgeInferenceService.statusMessage}"
            EdgeInferenceService.STATE_WAKING, EdgeInferenceService.STATE_LOADING ->
                "GAFAM Edge: ${EdgeInferenceService.edgeService}… ${EdgeInferenceService.statusMessage}"
            EdgeInferenceService.STATE_INFERRING ->
                "GAFAM Edge: inferring… ${EdgeInferenceService.statusMessage}"
            EdgeInferenceService.STATE_STOPPING -> "GAFAM Edge: stopping…"
            else -> "GAFAM Edge: idle — cap ${snap.capMb} Mo, max livrable ${snap.maxDeliverableMb} Mo"
        }
    }

    override fun onResume() {
        super.onResume()
        updateStatus()
        uiHandler.post(statusRefreshRunnable)
    }

    override fun onPause() {
        uiHandler.removeCallbacks(statusRefreshRunnable)
        super.onPause()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        val scroll = android.widget.ScrollView(this)
        val layout = LinearLayout(this)
        layout.orientation = LinearLayout.VERTICAL
        layout.setPadding(24, 24, 24, 80)
        layout.setBackgroundColor(0xFF111111.toInt())
        scroll.addView(layout)
        setContentView(scroll)
        
        statusText = TextView(this)
        statusText.textSize = 15f
        statusText.setTextColor(0xFFCCCCCC.toInt())
        layout.addView(statusText)

        val scanBtn = makeBtn("Scan VPC QR Code")
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

        // Gmail setup
        val gmailSetupBtn = makeBtn("🔧 Gmail Login Setup")
        gmailSetupBtn.setOnClickListener {
            startActivity(android.content.Intent(this, GmailScrapeActivity::class.java)
                .putExtra("setup", true)
                .addFlags(android.content.Intent.FLAG_ACTIVITY_NEW_TASK))
        }
        layout.addView(gmailSetupBtn)

        val emailBtn = makeBtn("📧 Email Relay")
        setNotifListenerBtn(emailBtn)
        emailBtn.setOnClickListener { startActivity(Intent(android.provider.Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS)) }
        layout.addView(emailBtn)

        val defaultSmsBtn = makeBtn("Set as Default SMS App")
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

        val syncSmsBtn = Button(this)
        syncSmsBtn.setBackgroundColor(android.graphics.Color.DKGRAY)
        syncSmsBtn.setTextColor(android.graphics.Color.WHITE)
        syncSmsBtn.text = "Sync Recent SMS History"
        syncSmsBtn.setOnClickListener {
            val p = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            if (p.getString("apiUrl", null) == null) {
                Toast.makeText(this, "Pair with VPC first", Toast.LENGTH_SHORT).show()
            } else {
                Toast.makeText(this, "Syncing SMS history…", Toast.LENGTH_SHORT).show()
                SmsHistorySync.syncAsync(this, force = true)
            }
        }
        layout.addView(syncSmsBtn)

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

        val edgeCapTitle = TextView(this)
        edgeCapTitle.text = "\nMax RAM Edge (plafond tel)"
        edgeCapTitle.textSize = 16f
        edgeCapTitle.setTextColor(android.graphics.Color.WHITE)
        edgeCapTitle.setTypeface(null, android.graphics.Typeface.BOLD)
        layout.addView(edgeCapTitle)

        val memSnap = EdgeRamPolicy.snapshot(this)
        val edgeCapLabel = TextView(this)
        edgeCapLabel.textSize = 13f
        edgeCapLabel.setTextColor(android.graphics.Color.LTGRAY)
        edgeCapLabel.text =
            "Cap ${memSnap.capMb} Mo · RAM dispo ${memSnap.availMb}/${memSnap.totalMb} Mo · max livrable ${memSnap.maxDeliverableMb} Mo"
        layout.addView(edgeCapLabel)
        edgeCapLabelRef = edgeCapLabel

        val edgeCapSeek = android.widget.SeekBar(this)
        edgeCapSeek.max = ((8192 - 512) / 256)
        edgeCapSeek.progress = ((EdgeRamPolicy.getCapMb(this) - 512) / 256)
        edgeCapSeek.setOnSeekBarChangeListener(object : android.widget.SeekBar.OnSeekBarChangeListener {
            override fun onProgressChanged(seekBar: android.widget.SeekBar?, progress: Int, fromUser: Boolean) {
                if (!fromUser) return
                val capMb = 512 + progress * 256
                EdgeRamPolicy.setCapMb(this@MainActivity, capMb)
                val snap = EdgeRamPolicy.snapshot(this@MainActivity)
                edgeCapLabel.text =
                    "Cap ${snap.capMb} Mo · RAM dispo ${snap.availMb}/${snap.totalMb} Mo · max livrable ${snap.maxDeliverableMb} Mo"
            }
            override fun onStartTrackingTouch(seekBar: android.widget.SeekBar?) {}
            override fun onStopTrackingTouch(seekBar: android.widget.SeekBar?) {}
        })
        layout.addView(edgeCapSeek)

        // Store a reference to update it from the poller
        syncSwitchRef = syncContactsSwitch

        smsLogText = TextView(this)
        smsLogText.textSize = 13f
        smsLogText.setTextColor(android.graphics.Color.LTGRAY)
        smsLogText.text = "No SMS intercepted yet."
        layout.addView(smsLogText)

        updateStatus()

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECEIVE_SMS) 
            != PackageManager.PERMISSION_GRANTED ||
            ContextCompat.checkSelfPermission(this, Manifest.permission.READ_CONTACTS) 
            != PackageManager.PERMISSION_GRANTED) {
            val perms = mutableListOf(
                Manifest.permission.RECEIVE_SMS,
                Manifest.permission.READ_SMS,
                Manifest.permission.SEND_SMS,
                Manifest.permission.INTERNET,
                Manifest.permission.CAMERA,
                Manifest.permission.READ_CONTACTS
            )
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU) {
                perms.add(Manifest.permission.POST_NOTIFICATIONS)
            }
            ActivityCompat.requestPermissions(this, perms.toTypedArray(), 101)
        } else if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.TIRAMISU &&
            ContextCompat.checkSelfPermission(this, Manifest.permission.POST_NOTIFICATIONS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.POST_NOTIFICATIONS),
                103
            )
        }

        // Demand permission Gmail Content Provider
        if (ContextCompat.checkSelfPermission(this, "com.google.android.gm.permission.READ_CONTENT_PROVIDER")
            != PackageManager.PERMISSION_GRANTED) {
            ActivityCompat.requestPermissions(this,
                arrayOf("com.google.android.gm.permission.READ_CONTENT_PROVIDER"), 104)
        }

        // Test Gmail Content Provider — try multiple URIs
        thread {
            try {
                val uris = listOf(
                    "content://com.google.android.gm",
                    "content://com.google.android.gm/labels",
                    "content://com.google.android.gm/conversations",
                    "content://com.google.android.gm/inbox",
                    "content://com.google.android.gm/messages",
                    "content://com.google.android.gm/account",
                )
                for (uri in uris) {
                    try {
                        val cursor = contentResolver.query(
                            android.net.Uri.parse(uri),
                            null, null, null, null
                        )
                        if (cursor != null) {
                            val cols = cursor.columnNames?.toList()
                            val count = cursor.count
                            Log.i("GAFAM_Relay", "GMAIL URI OK: $uri → columns=$cols count=$count")
                            cursor.close()
                        } else {
                            Log.d("GAFAM_Relay", "GMAIL URI null: $uri")
                        }
                    } catch (e: Exception) {
                        Log.e("GAFAM_Relay", "GMAIL URI ERROR $uri: ${e.message}")
                    }
                }
            } catch (e: Exception) {
                Log.e("GAFAM_Relay", "GMAIL TEST FAILED: ${e.message}")
            }
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

        // Sync contacts on start if enabled; keep relay alive in notification shade
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl != null && jwtSecret != null) {
            RelayForegroundService.start(this)
            syncContacts(apiUrl, jwtSecret)
            SmsHistorySync.syncAsync(this, force = true)
            LogShipper.event(this, "I", "boot", "Paired relay online — foreground service active")
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
        val snap = EdgeRamPolicy.snapshot(this)
        edgeCapLabelRef?.text =
            "Cap ${snap.capMb} Mo · RAM dispo ${snap.availMb}/${snap.totalMb} Mo · max livrable ${snap.maxDeliverableMb} Mo"
        if (url != null) {
            statusText.text =
                "Relay Agent is ACTIVE\n\nPhone: $phone\nConnected to:\n$url\n\n${edgeStatusLine()}\n\nWaiting for SMS...\nKeep the GAFAM notification ON (like a VPN)."
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
                        statusText.text = "🎉 Successfully Paired!\n\nRelay Agent is ACTIVE\n\nConnected to:\n$apiUrl\n\nWaiting for SMS...\n(Notification keeps relay alive)"
                        Toast.makeText(this, "VPC Connection Secured", Toast.LENGTH_LONG).show()
                        RelayForegroundService.start(this)
                        SmsHistorySync.syncAsync(this, force = true)
                        LogShipper.event(this, "I", "pair", "Successfully paired with VPC $apiUrl")
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
                        LogShipper.event(this@MainActivity, "I", "challenge", "Web login challenge programmed: $alertMessage")
                    } else {
                        statusText.text = "❌ Failed to program challenge. HTTP $code"
                        Toast.makeText(this@MainActivity, "Failed. Is VPC reachable?", Toast.LENGTH_LONG).show()
                        LogShipper.event(this@MainActivity, "E", "challenge", "Challenge HTTP $code")
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

    // Outbox polling lives in RelayForegroundService (survives UI close).

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

                val plaintext = contacts.toString().toByteArray(Charsets.UTF_8)

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

                val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/gafam/contacts")
                val client = ApiClient.getClient(this) ?: return@thread
                
                val requestBody = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
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

    private fun isNotifListenerEnabled(): Boolean {
        val flat = android.provider.Settings.Secure.getString(contentResolver, "enabled_notification_listeners") ?: return false
        return flat.contains("com.gafam.relay")
    }

    private fun makeBtn(text: String): Button {
        return Button(this).apply {
            this.text = text
            setBackgroundColor(0xFF222222.toInt())
            setTextColor(0xFFCCCCCC.toInt())
            textSize = 13f
        }
    }

    private fun setNotifListenerBtn(btn: Button) {
        val on = isNotifListenerEnabled()
        btn.text = if (on) "📧 Email Relay: OK" else "📧 Email Relay: OFF"
        btn.setTextColor(if (on) 0xFFAAAAAA.toInt() else 0xFF666666.toInt())
    }
}
