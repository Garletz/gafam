package com.gafam.relay

import android.content.Context
import okhttp3.Dns
import okhttp3.OkHttpClient
import java.net.InetAddress
import java.net.URL
import java.security.cert.CertificateException
import java.security.cert.X509Certificate
import javax.net.ssl.SSLContext
import javax.net.ssl.X509TrustManager
import java.util.concurrent.TimeUnit

object ApiClient {

    private var cachedClient: OkHttpClient? = null
    private var cachedUrl: String? = null
    private var cachedFingerprint: String? = null

    fun getClient(context: Context): OkHttpClient? {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return null
        val fingerprint = prefs.getString("certFingerprint", null) ?: return null

        if (cachedClient != null && apiUrl == cachedUrl && fingerprint == cachedFingerprint) {
            return cachedClient
        }

        val hostIp = try {
            URL(apiUrl).host
        } catch (e: Exception) {
            return null
        }

        val trustManager = object : X509TrustManager {
            override fun checkClientTrusted(chain: Array<out X509Certificate>?, authType: String?) {}
            override fun checkServerTrusted(chain: Array<out X509Certificate>?, authType: String?) {
                val serverCert = chain?.get(0) ?: throw CertificateException("No certificate")
                val md = java.security.MessageDigest.getInstance("SHA-256")
                val digest = md.digest(serverCert.encoded)
                val hex = digest.joinToString(":") { String.format("%02X", it) }
                if (!hex.equals(fingerprint, ignoreCase = true)) {
                    throw CertificateException("Fingerprint mismatch! Expected $fingerprint, got $hex")
                }
            }
            override fun getAcceptedIssuers(): Array<X509Certificate> = arrayOf()
        }

        val sslContext = SSLContext.getInstance("TLS")
        sslContext.init(null, arrayOf(trustManager), java.security.SecureRandom())

        cachedClient = OkHttpClient.Builder()
            .sslSocketFactory(sslContext.socketFactory, trustManager)
            .hostnameVerifier { _, _ -> true } // Trust any hostname because we pin the fingerprint
            .dns(object : Dns {
                override fun lookup(hostname: String): List<InetAddress> {
                    if (hostname == "wikipedia.org") {
                        return listOf(InetAddress.getByName(hostIp))
                    }
                    return Dns.SYSTEM.lookup(hostname)
                }
            })
            .connectTimeout(5, TimeUnit.SECONDS)
            .readTimeout(5, TimeUnit.SECONDS)
            .build()
            
        cachedUrl = apiUrl
        cachedFingerprint = fingerprint
            
        return cachedClient
    }
    
    fun getSpoofedUrl(apiUrl: String, path: String): String {
        return try {
            val originalUrl = URL(apiUrl)
            val port = if (originalUrl.port == -1) 5151 else originalUrl.port
            "https://wikipedia.org:${port}$path"
        } catch(e: Exception) {
            "https://wikipedia.org:5151$path"
        }
    }
}
