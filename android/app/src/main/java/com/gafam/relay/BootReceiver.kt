package com.gafam.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.util.Log

/** Restart foreground relay after reboot if already paired. */
class BootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        val action = intent?.action ?: return
        if (action != Intent.ACTION_BOOT_COMPLETED && action != Intent.ACTION_LOCKED_BOOT_COMPLETED) {
            return
        }
        Log.d("GAFAM_Relay", "Boot — starting relay service if paired")
        RelayForegroundService.start(context.applicationContext)
    }
}
