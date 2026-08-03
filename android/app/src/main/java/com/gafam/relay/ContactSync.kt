package com.gafam.relay

import android.Manifest
import android.content.Context
import android.content.pm.PackageManager
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
 * Pushes the full device contact list to the VPC.
 *
 * Used by MainActivity (manual toggle / pairing) and RelayForegroundService
 * (periodic + ContentObserver-triggered). The payload is a *full snapshot*:
 * the VPC replaces its table with it, so contacts deleted on the phone
 * disappear from the web client too.
 */
object ContactSync {
    private const val TAG = "GAFAM_Relay"
    private const val MIN_INTERVAL_MS = 5 * 60 * 1000L // 5 min between auto syncs

    private val syncing = AtomicBoolean(false)
    private val lastSyncAt = AtomicLong(0)

    fun syncAsync(context: Context, force: Boolean = false) {
        thread(name = "gafam-contact-sync", isDaemon = true) {
            sync(context.applicationContext, force)
        }
    }

    fun sync(context: Context, force: Boolean = false) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        if (!prefs.getBoolean("contacts_sync_enabled", true)) {
            Log.d(TAG, "Contact sync disabled, skipping.")
            return
        }
        if (!force) {
            val last = lastSyncAt.get()
            if (last > 0 && System.currentTimeMillis() - last < MIN_INTERVAL_MS) return
        }
        if (!syncing.compareAndSet(false, true)) return
        try {
            doSync(context)
            lastSyncAt.set(System.currentTimeMillis())
        } catch (e: Exception) {
            Log.e(TAG, "Contact sync failed", e)
            LogShipper.event(context, "E", "contacts", "Sync failed: ${e.message}")
        } finally {
            syncing.set(false)
        }
    }

    private fun doSync(context: Context) {
        if (ContextCompat.checkSelfPermission(context, Manifest.permission.READ_CONTACTS)
            != PackageManager.PERMISSION_GRANTED
        ) {
            Log.w(TAG, "READ_CONTACTS missing — skip contact sync")
            return
        }

        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return

        val contacts = JSONArray()
        val cursor = context.contentResolver.query(
            android.provider.ContactsContract.CommonDataKinds.Phone.CONTENT_URI,
            null, null, null, null
        )

        cursor?.use {
            val nameIdx = it.getColumnIndex(android.provider.ContactsContract.CommonDataKinds.Phone.DISPLAY_NAME)
            val phoneIdx = it.getColumnIndex(android.provider.ContactsContract.CommonDataKinds.Phone.NUMBER)
            while (it.moveToNext()) {
                val name = it.getString(nameIdx) ?: "Unknown"
                val phone = it.getString(phoneIdx)?.replace(" ", "") ?: ""
                if (phone.isNotEmpty()) {
                    contacts.put(JSONObject().apply {
                        put("phone_number", phone)
                        put("display_name", name)
                        put("is_verified", 1)
                    })
                }
            }
        }

        // Full snapshot — the VPC replaces its contact table with this list,
        // which propagates deletions made on the phone.
        val envelope = JSONObject().apply {
            put("full", true)
            put("contacts", contacts)
        }
        val plaintext = envelope.toString().toByteArray(Charsets.UTF_8)

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

        val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/gafam/contacts")
        val client = ApiClient.getClient(context) ?: return

        val requestBody = encryptedPayload.toString().toRequestBody("application/json".toMediaType())
        val request = Request.Builder()
            .url(spoofedUrl)
            .post(requestBody)
            .addHeader("Authorization", "Bearer $jwtSecret")
            .build()

        val response = client.newCall(request).execute()
        val code = response.code
        response.close()
        if (code in 200..299) {
            Log.d(TAG, "Contacts synced (${contacts.length()} contacts)")
            LogShipper.event(context, "I", "contacts", "Synced ${contacts.length()} contacts to VPC")
        } else {
            Log.w(TAG, "Contact sync HTTP $code")
            LogShipper.event(context, "W", "contacts", "Sync HTTP $code")
        }
    }
}
