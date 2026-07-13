package com.gafam.relay

import android.app.ActivityManager
import android.content.Context
import kotlin.math.max
import kotlin.math.min

/**
 * One-time edge RAM cap on device + live OS memory checks.
 * The phone declares max deliverable; clients request within that cap.
 */
object EdgeRamPolicy {
    private const val PREF_EDGE_RAM_CAP_MB = "edge_ram_cap_mb"
    private const val OS_MARGIN_MB = 768
    private const val MIN_CAP_MB = 512
    private const val MAX_CAP_MB = 8192

    data class MemorySnapshot(
        val totalMb: Int,
        val availMb: Int,
        val capMb: Int,
        val maxDeliverableMb: Int
    )

    data class WakeDecision(
        val ok: Boolean,
        val effectiveMb: Int,
        val message: String
    )

    fun getCapMb(context: Context): Int {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        var cap = prefs.getInt(PREF_EDGE_RAM_CAP_MB, -1)
        if (cap < 0) {
            cap = defaultCapMb(context)
            prefs.edit().putInt(PREF_EDGE_RAM_CAP_MB, cap).apply()
        }
        return cap.coerceIn(MIN_CAP_MB, MAX_CAP_MB)
    }

    fun setCapMb(context: Context, capMb: Int) {
        val clamped = capMb.coerceIn(MIN_CAP_MB, MAX_CAP_MB)
        context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
            .edit()
            .putInt(PREF_EDGE_RAM_CAP_MB, clamped)
            .apply()
    }

    fun snapshot(context: Context): MemorySnapshot {
        val am = context.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
        val info = ActivityManager.MemoryInfo()
        am.getMemoryInfo(info)
        val totalMb = (info.totalMem / (1024 * 1024)).toInt()
        val availMb = (info.availMem / (1024 * 1024)).toInt()
        val capMb = getCapMb(context)
        val maxDeliverable = max(
            0,
            min(capMb, availMb - OS_MARGIN_MB)
        )
        return MemorySnapshot(totalMb, availMb, capMb, maxDeliverable)
    }

    fun resolveWakeBudget(context: Context, requestedMb: Int): WakeDecision {
        val snap = snapshot(context)
        if (snap.maxDeliverableMb < MIN_CAP_MB) {
            return WakeDecision(
                ok = false,
                effectiveMb = 0,
                message = "RAM insuffisante (${snap.availMb} Mo libres, marge OS ${OS_MARGIN_MB} Mo)"
            )
        }
        val effective = min(requestedMb.coerceAtLeast(MIN_CAP_MB), snap.maxDeliverableMb)
        val msg = if (effective < requestedMb) {
            "Demande ${requestedMb} Mo → effectif ${effective} Mo (cap tel ${snap.capMb}, dispo ${snap.availMb})"
        } else {
            "Budget effectif ${effective} Mo (cap ${snap.capMb}, dispo ${snap.availMb})"
        }
        return WakeDecision(ok = true, effectiveMb = effective, message = msg)
    }

    private fun defaultCapMb(context: Context): Int {
        val am = context.getSystemService(Context.ACTIVITY_SERVICE) as ActivityManager
        val info = ActivityManager.MemoryInfo()
        am.getMemoryInfo(info)
        val totalMb = (info.totalMem / (1024 * 1024)).toInt()
        return min(2048, max(MIN_CAP_MB, totalMb / 3))
    }
}
