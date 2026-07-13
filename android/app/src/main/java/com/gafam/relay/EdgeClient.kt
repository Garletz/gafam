package com.gafam.relay

import android.content.Context
import android.util.Log
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/**
 * Polls VPC for edge wake/stop commands and reports local edge service state.
 */
object EdgeClient {
    private const val TAG = "GAFAM_EdgeClient"

    fun syncOnce(context: Context) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
        val client = ApiClient.getClient(context) ?: return

        val body = JSONObject().apply {
            put("edge_service", EdgeInferenceService.edgeService)
            put("ram_budget_mb", EdgeInferenceService.ramBudgetMb)
            put("ram_reserved_mb", EdgeInferenceService.ramReservedMb)
            put("model_on_device", false)
            put("message", EdgeInferenceService.statusMessage)
        }

        try {
            val url = ApiClient.getSpoofedUrl(apiUrl, "/api/auth/edge/sync")
            val request = Request.Builder()
                .url(url)
                .post(body.toString().toRequestBody("application/json".toMediaType()))
                .addHeader("Authorization", "Bearer $jwtSecret")
                .build()

            client.newCall(request).execute().use { response ->
                if (!response.isSuccessful) {
                    Log.w(TAG, "edge sync HTTP ${response.code}")
                    return
                }
                val respStr = response.body?.string() ?: return
                val resp = JSONObject(respStr)
                val command = resp.optString("command", "none")
                when (command) {
                    "wake" -> {
                        val budget = resp.optInt("ram_budget_mb", EdgeInferenceService.ramBudgetMb)
                        Log.i(TAG, "VPC command: wake budget=$budget")
                        LogShipper.event(context, "I", "edge", "Wake command from VPC ($budget MB)")
                        EdgeInferenceService.startWake(context.applicationContext, budget)
                    }
                    "stop" -> {
                        Log.i(TAG, "VPC command: stop")
                        LogShipper.event(context, "I", "edge", "Stop command from VPC")
                        EdgeInferenceService.requestStop(context.applicationContext)
                    }
                }
            }
        } catch (e: Exception) {
            Log.w(TAG, "edge sync error", e)
        }
    }
}
