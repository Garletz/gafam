package com.gafam.relay

import android.Manifest
import android.content.pm.PackageManager
import android.os.Bundle
import androidx.appcompat.app.AppCompatActivity
import androidx.core.app.ActivityCompat
import androidx.core.content.ContextCompat
import android.widget.TextView

class MainActivity : AppCompatActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        
        val textView = TextView(this)
        textView.text = "GAFAM Relay Agent is active.\nWaiting for permissions..."
        textView.textSize = 24f
        textView.setPadding(32, 32, 32, 32)
        
        setContentView(textView)

        if (ContextCompat.checkSelfPermission(this, Manifest.permission.RECEIVE_SMS) 
            != PackageManager.PERMISSION_GRANTED) {
            ActivityCompat.requestPermissions(
                this,
                arrayOf(Manifest.permission.RECEIVE_SMS, Manifest.permission.INTERNET),
                101
            )
        } else {
            textView.text = "GAFAM Relay Agent is active.\nPermissions GRANTED."
        }
    }
}
