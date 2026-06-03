package com.gafam.relay

import android.app.Activity
import android.os.Bundle

class ComposeSmsActivity : Activity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        // Dummy activity. Required to be Default SMS app.
        finish()
    }
}
