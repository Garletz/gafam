package com.gafam.relay

import android.content.Context
import android.util.Log
import java.io.File
import java.io.FileOutputStream
import java.net.HttpURLConnection
import java.net.URL

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
        val dir = modelDir(context)
        if (!dir.exists()) dir.mkdirs()

        val missing = EdgeModelConfig.FILES.filter { !File(dir, it).exists() }
        if (missing.isEmpty()) return

        val pairs = missing.map { name ->
            EdgeModelConfig.BASE_URL + name to File(dir, name)
        }

        var totalSize = 0L
        val sizes = pairs.map { (url, _) ->
            val conn = (URL(url).openConnection() as HttpURLConnection).apply {
                requestMethod = "HEAD"
                connectTimeout = 30_000
                readTimeout = 30_000
            }
            try {
                val len = conn.getHeaderFieldLong("Content-Length", -1)
                if (len > 0) len else 0L
            } finally {
                conn.disconnect()
            }
        }
        totalSize = sizes.sum().coerceAtLeast(1L)

        var downloaded = 0L
        pairs.forEachIndexed { idx, (url, dest) ->
            Log.i(TAG, "Downloading ${dest.name}")
            downloadFile(url, dest)
            downloaded += sizes.getOrElse(idx) { dest.length() }
            onProgress?.invoke(((100 * downloaded) / totalSize).toInt().coerceIn(0, 99))
        }
        onProgress?.invoke(100)
        Log.i(TAG, "Model files ready in ${dir.absolutePath}")
    }

    private fun downloadFile(url: String, dest: File) {
        val tmp = File(dest.parentFile, dest.name + ".partial")
        val conn = (URL(url).openConnection() as HttpURLConnection).apply {
            connectTimeout = 60_000
            readTimeout = 300_000
        }
        conn.inputStream.use { input ->
            FileOutputStream(tmp).use { output ->
                val buf = ByteArray(8192)
                while (true) {
                    val n = input.read(buf)
                    if (n <= 0) break
                    output.write(buf, 0, n)
                }
            }
        }
        conn.disconnect()
        if (!tmp.renameTo(dest)) {
            throw IllegalStateException("Failed to finalize download: ${dest.name}")
        }
    }
}
