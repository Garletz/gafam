package com.gafam.relay

import android.content.Context
import android.util.Log
import okhttp3.MediaType.Companion.toMediaType
import okhttp3.Request
import okhttp3.RequestBody.Companion.toRequestBody
import org.json.JSONObject

/**
 * Polls VPC for edge wake/stop/infer commands and reports local edge state.
 */
object EdgeClient {
    private const val TAG = "GAFAM_EdgeClient"

    fun syncOnce(context: Context) {
        val prefs = context.getSharedPreferences("GAFAM_PREFS", Context.MODE_PRIVATE)
        val apiUrl = prefs.getString("apiUrl", null) ?: return
        val jwtSecret = prefs.getString("jwtSecret", null) ?: return
        val client = ApiClient.getClient(context) ?: return

        val snap = EdgeRamPolicy.snapshot(context)
        val inferReport = EdgeInferenceService.takeInferReport()

        val body = JSONObject().apply {
            put("edge_service", EdgeInferenceService.edgeService)
            put("ram_request_mb", EdgeInferenceService.ramRequestMb)
            put("ram_reserved_mb", EdgeInferenceService.ramReservedMb)
            put("edge_ram_cap_mb", snap.capMb)
            put("device_ram_total_mb", snap.totalMb)
            put("device_ram_avail_mb", snap.availMb)
            put("edge_ram_max_deliverable_mb", snap.maxDeliverableMb)
            put("model_on_device", EdgeInferenceService.modelOnDevice)
            put("message", EdgeInferenceService.statusMessage)
            if (inferReport != null) {
                put("infer_job_id", inferReport.jobId)
                put("infer_content", inferReport.content)
                put("infer_error", inferReport.error)
                put("infer_latency_ms", inferReport.latencyMs)
            }
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
                handleSyncResponse(context, respStr)
            }
        } catch (e: Exception) {
            Log.w(TAG, "edge sync error", e)
        }
    }

    private fun handleSyncResponse(context: Context, respStr: String) {
        val resp = JSONObject(respStr)
        val command = resp.optString("command", "none")
        when (command) {
            "wake" -> {
                val requestMb = resp.optInt("ram_request_mb", 512)
                Log.i(TAG, "VPC command: wake request=$requestMb")
                LogShipper.event(context, "I", "edge", "Wake from VPC ($requestMb MB requested)")
                EdgeInferenceService.startWake(context.applicationContext, requestMb)
            }
            "stop" -> {
                Log.i(TAG, "VPC command: stop")
                LogShipper.event(context, "I", "edge", "Stop command from VPC")
                EdgeInferenceService.requestStop(context.applicationContext)
            }
            "infer" -> {
                val jobId = resp.optString("job_id", "")
                val prompt = resp.optString("prompt", "")
                if (jobId.isBlank() || prompt.isBlank()) {
                    Log.w(TAG, "infer command missing job_id or prompt")
                    return
                }
                Log.i(TAG, "VPC command: infer job=$jobId prompt=${prompt.take(40)}")
                LogShipper.event(context, "I", "edge", "Infer from VPC ($jobId)")
                EdgeInferenceService.runInfer(context.applicationContext, jobId, prompt) {
                    syncOnce(context.applicationContext)
                }
            }
        }
    }
}
