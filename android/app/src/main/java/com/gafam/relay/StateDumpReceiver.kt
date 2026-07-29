package com.gafam.relay

import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Environment
import android.util.Log
import java.io.File
import java.io.FileInputStream
import java.io.FileOutputStream
import java.util.zip.GZIPOutputStream

/**
 * GAFAM Ghost State Dump Receiver for GAFAM Relay.
 * Listens for: am broadcast -a com.gafam.relay.DUMP_STATE
 * Exports app databases and preferences to /sdcard/ghost_relay_dump.tar.gz
 */
class StateDumpReceiver : BroadcastReceiver() {

    override fun onReceive(context: Context, intent: Intent) {
        if (intent.action == "com.gafam.relay.DUMP_STATE") {
            Log.d("GhostRelay", "👻 Snapshot Broadcast Received in GAFAM Relay!")
            val targetPkg = intent.getStringExtra("package") ?: context.packageName
            val parentDir = context.dataDir.parentFile
            val targetDir = File(parentDir, targetPkg)
            val outputFile = File(Environment.getExternalStorageDirectory(), "ghost_relay_dump.tar.gz")

            try {
                if (targetDir.exists() && targetDir.canRead()) {
                    createSimpleZip(targetDir, outputFile)
                    Log.d("GhostRelay", "✅ Ghost Snapshot saved to ${outputFile.absolutePath}")
                } else {
                    // Export internal databases of GAFAM Relay itself
                    createSimpleZip(context.dataDir, outputFile)
                    Log.d("GhostRelay", "✅ Internal GAFAM Relay Snapshot saved to ${outputFile.absolutePath}")
                }
            } catch (e: Exception) {
                Log.e("GhostRelay", "❌ Ghost Snapshot failed: ${e.message}", e)
            }
        }
    }

    private fun createSimpleZip(sourceDir: File, outputFile: File) {
        FileOutputStream(outputFile).use { fos ->
            GZIPOutputStream(fos).use { gzos ->
                sourceDir.walkTopDown().forEach { file ->
                    if (file.isFile) {
                        FileInputStream(file).use { fis ->
                            fis.copyTo(gzos)
                        }
                    }
                }
            }
        }
    }
}
