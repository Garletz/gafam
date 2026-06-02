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

class MainActivity : AppCompatActivity() {

    private lateinit var statusText: TextView

    private val barcodeLauncher = registerForActivityResult(ScanContract()) { result ->
        if (result.contents == null) {
            Toast.makeText(this, "Scan cancelled", Toast.LENGTH_LONG).show()
        } else {
            handleScanResult(result.contents)
        }
    }

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
        
        setContentView(layout)
        updateStatus()

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECEIVE_SMS) 
            != PackageManager.PERMISSION_GRANTED) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.RECEIVE_SMS, Manifest.permission.INTERNET, Manifest.permission.CAMERA),
                101
            )
        }
    }

    private fun updateStatus() {
        val prefs = getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val url = prefs.getString("apiUrl", null)
        if (url != null) {
            statusText.text = "Relay Agent is ACTIVE\n\nConnected to:\n$url\n\nWaiting for SMS..."
        } else {
            statusText.text = "Relay Agent is INACTIVE\n\nPlease scan a VPC QR Code to connect."
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
}
