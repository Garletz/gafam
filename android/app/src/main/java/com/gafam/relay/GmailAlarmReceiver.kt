package com.gafam.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent

class GmailAlarmReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        if (!prefs.getBoolean("gmail_scrape_enabled", true)) return
        val activity = Intent(context, GmailScrapeActivity::class.java).apply {
            addFlags(Intent.FLAG_ACTIVITY_NEW_TASK)
            putExtra("setup", false)
        }
        context.startActivity(activity)
    }
}
