package com.gafam.relay

import android.content.Context
import android.os.Process
import android.util.Log
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONArray
import org.json.JSONObject
import java.io.BufferedReader
import java.io.InputStreamReader
import java.util.concurrent.ConcurrentLinkedQueue
import java.util.concurrent.atomic.AtomicBoolean
import kotlin.concurrent.thread

/**
 * Ships structured app events + best-effort process logcat to the VPC.
 * System-wide logcat requires ADB (READ_LOGS blocked for normal APKs).
 */
object LogShipper {
    private const val TAG = "GAFAM_Logs"
    private const val FLUSH_INTERVAL_MS = 5000L
    private const val MAX_QUEUE = 400
    private const val LOGCAT_INTERVAL_MS = 8000L

    private val queue = ConcurrentLinkedQueue<JSONObject>()
    private val started = AtomicBoolean(false)
    private var lastLogcatLine: String? = null

    fun start(context: Context) {
        if (!started.compareAndSet(false, true)) return
        event(context, "I", "shipper", "LogShipper started")
        thread(name = "gafam-log-flush", isDaemon = true) {
            while (started.get()) {
                try {
                    flush(context.applicationContext)
                } catch (e: Exception) {
                    Log.e(TAG, "flush error", e)
                }
                Thread.sleep(FLUSH_INTERVAL_MS)
            }
        }
        thread(name = "gafam-logcat-poll", isDaemon = true) {
            while (started.get()) {
                try {
                    harvestOwnLogcat(context.applicationContext)
                } catch (e: Exception) {
                    // Expected on some ROMs without permission
                }
                Thread.sleep(LOGCAT_INTERVAL_MS)
            }
        }
    }

    fun stop() {
        started.set(false)
    }

    fun event(context: Context?, level: String, tag: String, message: String, source: String = "event") {
        enqueue(level, tag, message, source)
        // Also mirror to Android logcat for local debugging
        when (level.uppercase()) {
            "E" -> Log.e("GAFAM_$tag", message)
            "W" -> Log.w("GAFAM_$tag", message)
            "D" -> Log.d("GAFAM_$tag", message)
            else -> Log.i("GAFAM_$tag", message)
        }
        // Opportunistic flush on errors
        if (level.equals("E", true) && context != null) {
            thread(isDaemon = true) {
                try {
                    flush(context.applicationContext)
                } catch (_: Exception) {
                }
            }
        }
    }

    private fun enqueue(level: String, tag: String, message: String, source: String) {
        while (queue.size >= MAX_QUEUE) {
            queue.poll()
        }
        queue.offer(JSONObject().apply {
            put("ts", System.currentTimeMillis())
            put("source", source)
            put("level", level.uppercase().take(1))
            put("tag", tag.take(64))
            put("message", message.take(4000))
        })
    }

    private fun harvestOwnLogcat(context: Context) {
        // Own-PID logcat is often allowed; full system logcat is not.
        val pid = Process.myPid()
        val pb = ProcessBuilder("logcat", "-d", "--pid=$pid", "-v", "threadtime", "*:V")
        pb.redirectErrorStream(true)
        val proc = pb.start()
        val reader = BufferedReader(InputStreamReader(proc.inputStream))
        var line: String?
        var sawNew = false
        val collected = mutableListOf<String>()
        while (reader.readLine().also { line = it } != null) {
            val l = line ?: continue
            if (lastLogcatLine != null && !sawNew) {
                if (l == lastLogcatLine) {
                    sawNew = true
                }
                continue
            }
            if (lastLogcatLine == null) {
                // First run: skip backlog, only keep last few
                collected.add(l)
                if (collected.size > 40) collected.removeAt(0)
            } else if (sawNew) {
                collected.add(l)
            }
        }
        reader.close()
        proc.waitFor()
        if (collected.isEmpty()) return
        lastLogcatLine = collected.last()
        for (l in collected) {
            parseAndEnqueueLogcat(l)
        }
    }

    private fun parseAndEnqueueLogcat(line: String) {
        // threadtime: "01-02 12:34:56.789  1234  5678 I Tag: message"
        val parts = line.split(" ", limit = 6)
        if (parts.size < 6) {
            enqueue("I", "logcat", line, "apk")
            return
        }
        // Find level letter
        var level = "I"
        var tagMsg = line
        val levelIdx = listOf(" V ", " D ", " I ", " W ", " E ", " F ").firstOrNull { line.contains(it) }
        if (levelIdx != null) {
            level = levelIdx.trim()
            val idx = line.indexOf(levelIdx)
            tagMsg = line.substring(idx + levelIdx.length).trim()
        }
        val colon = tagMsg.indexOf(':')
        val tag = if (colon > 0) tagMsg.substring(0, colon).trim() else "logcat"
        val msg = if (colon > 0) tagMsg.substring(colon + 1).trim() else tagMsg
        enqueue(level, tag, msg, "apk")
    }

    fun flush(context: Context) {
        if (queue.isEmpty()) return
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
        val client = ApiClient.getClient(context) ?: return

        val batch = JSONArray()
        while (true) {
            val item = queue.poll() ?: break
            batch.put(item)
            if (batch.length() >= 200) break
        }
        if (batch.length() == 0) return

        val payloadObj = JSONObject().put("entries", batch)
        val plaintext = payloadObj.toString().toByteArray(Charsets.UTF_8)

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

        val spoofedUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/logs")
        val request = Request.Builder()
            .url(spoofedUrl)
            .post(encryptedPayload.toString().toRequestBody("application/json".toMediaType()))
            .addHeader("Authorization", "Bearer $jwtSecret")
            .build()

        val response = client.newCall(request).execute()
        if (!response.isSuccessful) {
            Log.w(TAG, "VPC rejected logs: HTTP ${response.code}")
            // Re-queue on failure (best effort)
            for (i in 0 until batch.length()) {
                queue.offer(batch.getJSONObject(i))
            }
        } else {
            Log.d(TAG, "Shipped ${batch.length()} log entries")
        }
        response.close()
    }
}
