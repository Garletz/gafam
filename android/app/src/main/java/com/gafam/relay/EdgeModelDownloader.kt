package com.gafam.relay

import android.content.Context
import android.util.Log
import okhttp3.Request
import org.json.JSONObject
import java.io.File
import java.io.FileOutputStream

object EdgeModelDownloader {
    private const val TAG = "GAFAM_EdgeDL"

    fun modelDir(context: Context): File =
        File(context.filesDir, EdgeModelConfig.MODEL_DIR_NAME)

    fun isReady(context: Context): Boolean {
        val dir = modelDir(context)
        return EdgeModelConfig.FILES.all { File(dir, it).exists() && File(dir, it).length() > 0 }
    }

    fun ensureDownloaded(
        context: Context,
        onProgress: ((Int) -> Unit)? = null
    ) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null)
            ?: throw IllegalStateException("APK not paired with VPC")
        val jwt = prefs.getString("jwtSecret", null)
            ?: throw IllegalStateException("Missing JWT secret")
        val client = ApiClient.getDownloadClient(context)
            ?: throw IllegalStateException("Cannot build HTTP client")

        val dir = modelDir(context)
        if (!dir.exists()) dir.mkdirs()

        val missing = EdgeModelConfig.FILES.filter { !File(dir, it).exists() }
        if (missing.isEmpty()) {
            onProgress?.invoke(100)
            return
        }

        val manifestUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/edge/model")
        val manifestReq = Request.Builder()
            .url(manifestUrl)
            .addHeader("Authorization", "Bearer $jwt")
            .get()
            .build()
        client.newCall(manifestReq).execute().use { manifestResp ->
            if (!manifestResp.isSuccessful) {
                throw IllegalStateException("VPC model manifest HTTP ${manifestResp.code}")
            }
            val manifestBody = manifestResp.body?.string() ?: throw IllegalStateException("empty manifest")
            val manifest = JSONObject(manifestBody)
            if (!manifest.optBoolean("ready", false)) {
                throw IllegalStateException(
                    "Modèle ONNX absent sur le VPC — lance edge-model-install.sh sur le serveur"
                )
            }

            val sizes = mutableMapOf<String, Long>()
            val filesArr = manifest.optJSONArray("files") ?: throw IllegalStateException("bad manifest")
            for (i in 0 until filesArr.length()) {
                val entry = filesArr.getJSONObject(i)
                sizes[entry.getString("name")] = entry.optLong("size", 0)
            }

            val totalBytes = missing.sumOf { sizes[it] ?: 0L }.coerceAtLeast(1L)
            var doneBytes = 0L
            onProgress?.invoke(0)

            for (name in missing) {
                val dest = File(dir, name)
                val fileUrl = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/edge/model/$name")
                Log.i(TAG, "Downloading $name from VPC")
                val req = Request.Builder()
                    .url(fileUrl)
                    .addHeader("Authorization", "Bearer $jwt")
                    .get()
                    .build()
                client.newCall(req).execute().use { response ->
                    if (!response.isSuccessful) {
                        throw IllegalStateException("Download $name failed HTTP ${response.code}")
                    }
                    val body = response.body ?: throw IllegalStateException("empty body for $name")
                    val tmp = File(dir, "$name.partial")
                    val fileSize = sizes[name] ?: body.contentLength().coerceAtLeast(0L)
                    FileOutputStream(tmp).use { output ->
                        body.byteStream().use { input ->
                            val buf = ByteArray(8192)
                            var fileDone = 0L
                            while (true) {
                                val n = input.read(buf)
                                if (n <= 0) break
                                output.write(buf, 0, n)
                                fileDone += n
                                doneBytes += n
                                val pct = ((100 * doneBytes) / totalBytes).toInt().coerceIn(0, 99)
                                onProgress?.invoke(pct)
                            }
                            if (fileSize > 0 && fileDone < fileSize) {
                                Log.w(TAG, "$name: expected $fileSize got $fileDone")
                            }
                        }
                    }
                    if (!tmp.renameTo(dest)) {
                        throw IllegalStateException("Failed to finalize $name")
                    }
                }
            }
        }
        onProgress?.invoke(100)
        Log.i(TAG, "Model files ready in ${dir.absolutePath}")
    }
}
