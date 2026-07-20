package com.gafam.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class GmailAlarmReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val activity = Intent(context, GmailScrapeActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            putExtra("setup", false)
        }
        context.startActivity(activity)
    }
}
