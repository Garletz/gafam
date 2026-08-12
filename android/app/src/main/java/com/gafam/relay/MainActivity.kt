package com.gafam.relay

import android.Manifest
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.content.IntentFilter
import android.content.pm.PackageManager
import android.os.Bundle
import android.telephony.SmsManager
import android.text.InputType
import android.util.Log
import android.view.Gravity
import android.view.View
import android.widget.Button
import android.widget.EditText
import android.widget.FrameLayout
import android.widget.LinearLayout
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import com.journeyapps.barcodescanner.ScanContract
import com.journeyapps.barcodescanner.ScanOptions
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject
import java.util.UUID
import kotlin.concurrent.thread

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

    private lateinit var contentFrame: FrameLayout
    private lateinit var dashboardView: View
    private var contactsView: View? = null
    private var smsView: View? = null
    private var activePanel = "dashboard"
    private lateinit var navDashboard: TextView
    private lateinit var navContacts: TextView
    private lateinit var navSms: TextView

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)

        val root = LinearLayout(this).apply {
            orientation = LinearLayout.HORIZONTAL
            setBackgroundColor(0xFF111111.toInt())
        }

        // ── Side navigation bar ──
        val navBar = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setBackgroundColor(0xFF181818.toInt())
            layoutParams = LinearLayout.LayoutParams(52.dp, LinearLayout.LayoutParams.MATCH_PARENT)
            gravity = Gravity.CENTER_HORIZONTAL
        }

        navContacts = makeNavIcon("☎")
        navDashboard = makeNavIcon("⊚")
        navSms = makeNavIcon("✉")

        navContacts.setOnClickListener { switchPanel("contacts") }
        navDashboard.setOnClickListener { switchPanel("dashboard") }
        navSms.setOnClickListener { switchPanel("sms") }

        val navTopSpacer = View(this).apply {
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f)
        }
        val navMidSpacer1 = View(this).apply {
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f)
        }
        val navMidSpacer2 = View(this).apply {
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f)
        }
        val navBtmSpacer = View(this).apply {
            layoutParams = LinearLayout.LayoutParams(LinearLayout.LayoutParams.MATCH_PARENT, 0, 1f)
        }

        navBar.addView(navTopSpacer)
        navBar.addView(navContacts)
        navBar.addView(navMidSpacer1)
        navBar.addView(navDashboard)
        navBar.addView(navMidSpacer2)
        navBar.addView(navSms)
        navBar.addView(navBtmSpacer)

        // ── Content area ──
        contentFrame = FrameLayout(this).apply {
            layoutParams = LinearLayout.LayoutParams(0, LinearLayout.LayoutParams.MATCH_PARENT, 1f)
        }

        // ── Dashboard panel (default) ──
        dashboardView = buildDashboardView()

        root.addView(navBar)
        root.addView(contentFrame)
        setContentView(root)

        contentFrame.addView(dashboardView)
        highlightNav("dashboard")

        // ── Permissions ──
        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECEIVE_SMS)
            != PackageManager.PERMISSION_GRANTED ||
            ContextCompat.checkSelfPermission(this, Manifest.permission.READ_CONTACTS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            val perms = mutableListOf(
                Manifest.permission.RECEIVE_SMS,
                Manifest.permission.READ_SMS,
                "android.permission.WRITE_SMS",
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
                arrayOf(Manifest.permission.POST_NOTIFICATIONS), 103
            )
        }

        if (ContextCompat.checkSelfPermission(this, "com.google.android.gm.permission.READ_CONTENT_PROVIDER")
            != PackageManager.PERMISSION_GRANTED
        ) {
            ActivityCompat.requestPermissions(this,
                arrayOf("com.google.android.gm.permission.READ_CONTENT_PROVIDER"), 104)
        }

        // Gmail Content Provider test
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
                            android.net.Uri.parse(uri), null, null, null, null
                        )
                        if (cursor != null) {
                            val cols = cursor.columnNames?.toList()
                            val count = cursor.count
                            Log.i("GAFAM_Relay", "GMAIL URI OK: $uri -> columns=$cols count=$count")
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

        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        if (prefs.getString("myPhoneNumber", null) == null) {
            promptForPhoneNumber()
        }

        // Setup UI Receiver for intercepted SMS preview in dashboard
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

        // Sync on start if paired
        val apiUrl = prefs.getString("apiUrl", null)
        val jwtSecret = prefs.getString("jwtSecret", null)
        if (apiUrl != null && jwtSecret != null) {
            RelayForegroundService.start(this)
            ContactSync.syncAsync(this, force = true)
            SmsHistorySync.syncAsync(this, force = true)
            LogShipper.event(this, "I", "boot", "Paired relay online — foreground service active")
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

    override fun onDestroy() {
        super.onDestroy()
        vfyReceiver?.let { unregisterReceiver(it) }
        smsUiReceiver?.let { unregisterReceiver(it) }
    }

    override fun onBackPressed() {
        if (activePanel == "sms" || activePanel == "contacts") {
            switchPanel("dashboard")
        } else {
            super.onBackPressed()
        }
    }

    private fun switchPanel(panel: String) {
        if (activePanel == panel) return
        if (activePanel == "sms") SmsPanel.onPanelHidden()
        activePanel = panel
        contentFrame.removeAllViews()
        highlightNav(panel)
        when (panel) {
            "contacts" -> {
                if (contactsView == null) contactsView = ContactsPanel.create(this)
                contentFrame.addView(contactsView)
            }
            "sms" -> {
                if (smsView == null) smsView = SmsPanel.create(this)
                SmsPanel.onPanelShown()
                contentFrame.addView(smsView)
            }
            else -> {
                contentFrame.addView(dashboardView)
            }
        }
    }

    private fun highlightNav(panel: String) {
        val active = 0xFFAAAAAA.toInt()
        val inactive = 0xFF444444.toInt()
        navContacts.setTextColor(if (panel == "contacts") active else inactive)
        navDashboard.setTextColor(if (panel == "dashboard") active else inactive)
        navSms.setTextColor(if (panel == "sms") active else inactive)
    }

    private fun makeNavIcon(symbol: String): TextView {
        return TextView(this).apply {
            text = symbol
            textSize = 22f
            setTextColor(0xFF444444.toInt())
            gravity = Gravity.CENTER
            width = 52.dp
            height = 52.dp
        }
    }

    // ── Dashboard view (existing control panel) ──

    private fun buildDashboardView(): View {
        val scroll = ScrollView(this)
        val layout = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(16, 16, 16, 80)
        }
        scroll.addView(layout)

        statusText = TextView(this).apply {
            textSize = 14f
            setTextColor(0xFFCCCCCC.toInt())
        }
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

        val gmailSetupBtn = makeBtn("Gmail Login Setup")
        gmailSetupBtn.setOnClickListener {
            startActivity(Intent(this, GmailScrapeActivity::class.java)
                .putExtra("setup", true)
                .addFlags(Intent.FLAG_ACTIVITY_NEW_TASK))
        }
        layout.addView(gmailSetupBtn)

        val sp = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val scrapeToggle = makeBtn(if (sp.getBoolean("gmail_scrape_enabled", true)) "Stop Gmail Scan" else "Start Gmail Scan")
        scrapeToggle.setOnClickListener {
            val cur = sp.getBoolean("gmail_scrape_enabled", true)
            sp.edit().putBoolean("gmail_scrape_enabled", !cur).apply()
            scrapeToggle.text = if (!cur) "Stop Gmail Scan" else "Start Gmail Scan"
            Toast.makeText(this, "Gmail scan ${if (!cur) "ON" else "OFF"}", Toast.LENGTH_SHORT).show()
        }
        layout.addView(scrapeToggle)

        val emailBtn = makeBtn("Email Relay")
        setNotifListenerBtn(emailBtn)
        emailBtn.setOnClickListener { startActivity(Intent(android.provider.Settings.ACTION_NOTIFICATION_LISTENER_SETTINGS)) }
        layout.addView(emailBtn)

        val defaultSmsBtn = makeBtn("Set as Default SMS App")
        defaultSmsBtn.setOnClickListener {
            if (android.os.Build.VERSION.SDK_INT >= android.os.Build.VERSION_CODES.Q) {
                val roleManager = getSystemService(android.app.role.RoleManager::class.java)
                if (roleManager?.isRoleAvailable(android.app.role.RoleManager.ROLE_SMS) == true) {
                    startActivityForResult(roleManager.createRequestRoleIntent(android.app.role.RoleManager.ROLE_SMS), 102)
                }
            } else {
                val intent = Intent(android.provider.Telephony.Sms.Intents.ACTION_CHANGE_DEFAULT)
                intent.putExtra(android.provider.Telephony.Sms.Intents.EXTRA_PACKAGE_NAME, packageName)
                startActivityForResult(intent, 102)
            }
        }
        layout.addView(defaultSmsBtn)

        val authWebBtn = Button(this).apply {
            setBackgroundColor(android.graphics.Color.DKGRAY)
            setTextColor(android.graphics.Color.WHITE)
            text = "Authorize Web Login"
            setOnClickListener {
                val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                val apiUrl = prefs.getString("apiUrl", null)
                val token = prefs.getString("jwtSecret", null)
                if (apiUrl == null || token == null) {
                    Toast.makeText(this@MainActivity, "Not paired with a VPC yet", Toast.LENGTH_LONG).show()
                    return@setOnClickListener
                }
                generateAndSendChallenge(apiUrl, token)
            }
        }
        layout.addView(authWebBtn)

        val syncSmsBtn = Button(this).apply {
            setBackgroundColor(android.graphics.Color.DKGRAY)
            setTextColor(android.graphics.Color.WHITE)
            text = "Sync Recent SMS History"
            setOnClickListener {
                val p = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                if (p.getString("apiUrl", null) == null) {
                    Toast.makeText(this@MainActivity, "Pair with VPC first", Toast.LENGTH_SHORT).show()
                } else {
                    Toast.makeText(this@MainActivity, "Syncing SMS history...", Toast.LENGTH_SHORT).show()
                    SmsHistorySync.syncAsync(this@MainActivity, force = true)
                }
            }
        }
        layout.addView(syncSmsBtn)

        val testSmsBtn = Button(this).apply {
            setBackgroundColor(android.graphics.Color.DKGRAY)
            setTextColor(android.graphics.Color.WHITE)
            text = "Send Test SMS to Myself"
            setOnClickListener {
                val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                val phone = prefs.getString("myPhoneNumber", null)
                if (phone == null) {
                    Toast.makeText(this@MainActivity, "No phone number registered.", Toast.LENGTH_SHORT).show()
                    return@setOnClickListener
                }
                try {
                    val smsManager = SmsManager.getDefault()
                    val testMessage = "GAFAM Test SMS - ${System.currentTimeMillis()}"
                    smsManager.sendTextMessage(phone, null, testMessage, null, null)
                    Toast.makeText(this@MainActivity, "Test SMS sent to $phone", Toast.LENGTH_SHORT).show()
                } catch (e: Exception) {
                    Toast.makeText(this@MainActivity, "Failed to send SMS.", Toast.LENGTH_SHORT).show()
                }
            }
        }
        layout.addView(testSmsBtn)

        val resetPhoneBtn = Button(this).apply {
            setBackgroundColor(0xFF5F6368.toInt())
            setTextColor(android.graphics.Color.WHITE)
            text = "Reset phone number (re-setup)"
            setOnClickListener {
                val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
                val current = prefs.getString("myPhoneNumber", null)
                android.app.AlertDialog.Builder(this@MainActivity)
                    .setTitle("Reset phone number?")
                    .setMessage("Current: ${current ?: "not set"}\n\nClears the stored number and restarts the first-launch setup.")
                    .setPositiveButton("Reset & setup") { _, _ ->
                        prefs.edit().remove("myPhoneNumber").apply()
                        vfyReceiver?.let {
                            try { unregisterReceiver(it) } catch (_: Exception) {}
                            vfyReceiver = null
                        }
                        updateStatus()
                        Toast.makeText(this@MainActivity, "Phone cleared — enter your real number", Toast.LENGTH_LONG).show()
                        promptForPhoneNumber()
                    }
                    .setNegativeButton("Cancel", null)
                    .show()
            }
        }
        layout.addView(resetPhoneBtn)

        val smsLogTitle = TextView(this).apply {
            text = "\nRecent Intercepted SMS:"
            textSize = 15f
            setTextColor(android.graphics.Color.WHITE)
            setTypeface(null, android.graphics.Typeface.BOLD)
        }
        layout.addView(smsLogTitle)

        val syncContactsSwitch = android.widget.Switch(this).apply {
            text = "Sync Contacts with VPC"
            setTextColor(android.graphics.Color.WHITE)
            val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            isChecked = prefs.getBoolean("contacts_sync_enabled", true)
            setOnCheckedChangeListener { _, isChecked ->
                prefs.edit().putBoolean("contacts_sync_enabled", isChecked).apply()
                updateVpcSettings("contacts_sync_enabled", if (isChecked) "true" else "false")
                if (isChecked) ContactSync.syncAsync(this@MainActivity, force = true)
            }
        }
        layout.addView(syncContactsSwitch)

        val edgeCapTitle = TextView(this).apply {
            text = "\nMax RAM Edge (plafond tel)"
            textSize = 15f
            setTextColor(android.graphics.Color.WHITE)
            setTypeface(null, android.graphics.Typeface.BOLD)
        }
        layout.addView(edgeCapTitle)

        val memSnap = EdgeRamPolicy.snapshot(this)
        val edgeCapLabel = TextView(this).apply {
            textSize = 12f
            setTextColor(android.graphics.Color.LTGRAY)
            text = "Cap ${memSnap.capMb} Mo · RAM dispo ${memSnap.availMb}/${memSnap.totalMb} Mo · max livrable ${memSnap.maxDeliverableMb} Mo"
        }
        layout.addView(edgeCapLabel)
        edgeCapLabelRef = edgeCapLabel

        val edgeCapSeek = android.widget.SeekBar(this).apply {
            max = ((8192 - 512) / 256)
            progress = ((EdgeRamPolicy.getCapMb(this@MainActivity) - 512) / 256)
            setOnSeekBarChangeListener(object : android.widget.SeekBar.OnSeekBarChangeListener {
                override fun onProgressChanged(seekBar: android.widget.SeekBar?, progress: Int, fromUser: Boolean) {
                    if (!fromUser) return
                    val capMb = 512 + progress * 256
                    EdgeRamPolicy.setCapMb(this@MainActivity, capMb)
                    val snap = EdgeRamPolicy.snapshot(this@MainActivity)
                    edgeCapLabel.text = "Cap ${snap.capMb} Mo · RAM dispo ${snap.availMb}/${snap.totalMb} Mo · max livrable ${snap.maxDeliverableMb} Mo"
                }
                override fun onStartTrackingTouch(seekBar: android.widget.SeekBar?) {}
                override fun onStopTrackingTouch(seekBar: android.widget.SeekBar?) {}
            })
        }
        layout.addView(edgeCapSeek)

        syncSwitchRef = syncContactsSwitch

        smsLogText = TextView(this).apply {
            textSize = 12f
            setTextColor(android.graphics.Color.LTGRAY)
            text = "No SMS intercepted yet."
        }
        layout.addView(smsLogText)

        updateStatus()
        return scroll
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

    private fun edgeStatusLine(): String {
        val snap = EdgeRamPolicy.snapshot(this)
        return when (EdgeInferenceService.edgeService) {
            EdgeInferenceService.STATE_AWAKE ->
                "GAFAM Edge: AWAKE (${EdgeInferenceService.ramRequestMb} Mo tache)\n" +
                    "Cap tel ${snap.capMb} Mo · dispo ${snap.availMb} Mo · max ${snap.maxDeliverableMb} Mo\n" +
                    EdgeInferenceService.statusMessage
            EdgeInferenceService.STATE_ERROR ->
                "GAFAM Edge: erreur - ${EdgeInferenceService.statusMessage}"
            EdgeInferenceService.STATE_WAKING, EdgeInferenceService.STATE_LOADING ->
                "GAFAM Edge: ${EdgeInferenceService.edgeService}... ${EdgeInferenceService.statusMessage}"
            EdgeInferenceService.STATE_INFERRING ->
                "GAFAM Edge: inferring... ${EdgeInferenceService.statusMessage}"
            EdgeInferenceService.STATE_STOPPING -> "GAFAM Edge: stopping..."
            else -> "GAFAM Edge: idle - cap ${snap.capMb} Mo, max livrable ${snap.maxDeliverableMb} Mo"
        }
    }

    private fun promptForPhoneNumber() {
        val input = EditText(this)
        input.inputType = InputType.TYPE_CLASS_PHONE
        input.hint = "Ex: 0612345678"
        fun looksLikeDummy(p: String): Boolean {
            val d = p.filter { it.isDigit() }
            return d.length < 9 || d.all { it == '0' } || d.matches(Regex("0*6?0{6,}"))
        }
        android.app.AlertDialog.Builder(this)
            .setTitle("Enter Your Phone Number")
            .setMessage("Type your real SIM number (e.g. 06... or +33...). We send a self-SMS and only save the number if that SMS comes back.")
            .setView(input)
            .setCancelable(false)
            .setPositiveButton("Verify") { _, _ ->
                val phone = input.text.toString().trim()
                when {
                    phone.isEmpty() -> promptForPhoneNumber()
                    looksLikeDummy(phone) -> {
                        Toast.makeText(this, "That looks like a dummy number. Enter your real number.", Toast.LENGTH_LONG).show()
                        promptForPhoneNumber()
                    }
                    else -> startSelfSmsVerification(phone)
                }
            }
            .show()
    }

    private fun startSelfSmsVerification(phone: String) {
        val secretCode = "GAFAM-VFY-${(1000..9999).random()}"
        statusText.text = "Verifying phone number via self-SMS...\nPlease wait."

        val filter = IntentFilter("com.gafam.relay.VFY_SMS")
        vfyReceiver = object : BroadcastReceiver() {
            override fun onReceive(context: Context, intent: Intent) {
                val body = intent.getStringExtra("body") ?: ""
                if (body.contains(secretCode)) {
                    Log.d("GAFAM_Relay", "Self-SMS Verification Success!")
                    val digits = phone.filter { it.isDigit() }
                    if (digits.length < 9 || digits.all { it == '0' } || digits.matches(Regex("0*6?0{6,}"))) {
                        Toast.makeText(this@MainActivity, "Rejected dummy number.", Toast.LENGTH_LONG).show()
                        promptForPhoneNumber()
                        return
                    }
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

        try {
            SmsManager.getDefault().sendTextMessage(phone, null, secretCode, null, null)
            Toast.makeText(this, "Verification SMS sent...", Toast.LENGTH_SHORT).show()
        } catch (e: Exception) {
            Log.e("GAFAM_Relay", "Error sending verification SMS", e)
            Toast.makeText(this, "Failed to send SMS. Ensure permissions are granted.", Toast.LENGTH_LONG).show()
            promptForPhoneNumber()
        }
    }

    private fun handleScanResult(contents: String) {
        statusText.text = "QR Code Scanned!\n\nConnecting to VPC and verifying secure handshake..."

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
                        statusText.text = "Successfully Paired!\n\nRelay Agent is ACTIVE\n\nConnected to:\n$apiUrl\n\nWaiting for SMS..."
                        Toast.makeText(this, "VPC Connection Secured", Toast.LENGTH_LONG).show()
                        RelayForegroundService.start(this)
                        SmsHistorySync.syncAsync(this, force = true)
                        LogShipper.event(this, "I", "pair", "Successfully paired with VPC $apiUrl")
                    } else {
                        statusText.text = "Pairing Failed.\n\nCould not reach the VPC or invalid token."
                        Toast.makeText(this, "Network or Auth Error", Toast.LENGTH_LONG).show()
                    }
                }
            }
        } catch (e: Exception) {
            Log.e("GAFAM", "QR Parse Error", e)
            statusText.text = "Invalid QR Code format."
            Toast.makeText(this, "Invalid QR Code", Toast.LENGTH_LONG).show()
        }
    }

    private fun pairDevice(apiUrl: String, token: String, deviceId: String, certFingerprint: String, callback: (Boolean) -> Unit) {
        thread {
            try {
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

        statusText.text = "Programming Challenge...\nTime: $displayTime\nImpulsions: $challengeClicks"

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
                        android.app.AlertDialog.Builder(this@MainActivity)
                            .setTitle("Challenge Programmed")
                            .setMessage("Saisissez $displayTime sur gafam.cloud et preparez-vous a cliquer $challengeClicks fois a l'heure pile.")
                            .setPositiveButton("OK", null)
                            .show()
                        statusText.text = "Challenge Ready!\n$displayTime - $challengeClicks clicks"
                        LogShipper.event(this@MainActivity, "I", "challenge", "Web login challenge programmed")
                    } else {
                        statusText.text = "Failed to program challenge. HTTP $code"
                        Toast.makeText(this@MainActivity, "Failed. Is VPC reachable?", Toast.LENGTH_LONG).show()
                    }
                }
            } catch (e: Exception) {
                Log.e("GAFAM", "Challenge auth error", e)
                runOnUiThread {
                    statusText.text = "Network error during challenge creation."
                    Toast.makeText(this@MainActivity, "Failed: " + e.message, Toast.LENGTH_LONG).show()
                }
            }
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

    private fun isNotifListenerEnabled(): Boolean {
        val flat = android.provider.Settings.Secure.getString(contentResolver, "enabled_notification_listeners") ?: return false
        return flat.contains("com.gafam.relay")
    }

    private fun makeBtn(text: String): Button {
        return Button(this).apply {
            this.text = text
            setBackgroundColor(0xFF222222.toInt())
            setTextColor(0xFFCCCCCC.toInt())
            textSize = 12f
        }
    }

    private fun setNotifListenerBtn(btn: Button) {
        val on = isNotifListenerEnabled()
        btn.text = if (on) "Email Relay: OK" else "Email Relay: OFF"
        btn.setTextColor(if (on) 0xFFAAAAAA.toInt() else 0xFF666666.toInt())
    }

    private val Int.dp: Int
        get() = (this * resources.displayMetrics.density).toInt()
}
