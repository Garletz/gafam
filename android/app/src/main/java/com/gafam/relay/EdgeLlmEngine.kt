package com.gafam.relay

import ai.onnxruntime.genai.GenAIException
import ai.onnxruntime.genai.GeneratorParams
import ai.onnxruntime.genai.SimpleGenAI
import android.util.Log
import java.io.File

object EdgeLlmEngine {
    private const val TAG = "GAFAM_EdgeLLM"

    @Volatile
    private var genAI: SimpleGenAI? = null

    private val lock = Any()

    fun isLoaded(): Boolean = genAI != null

    @Throws(GenAIException::class)
    fun load(modelDir: File) {
        synchronized(lock) {
            genAI?.close()
            Log.i(TAG, "Loading ONNX GenAI from ${modelDir.absolutePath}")
            genAI = SimpleGenAI(modelDir.absolutePath)
        }
    }

    @Throws(GenAIException::class)
    fun complete(prompt: String, maxTokens: Int = 64): String {
        val engine = genAI ?: throw IllegalStateException("model not loaded")
        val formatted = formatQwen3Prompt(prompt.trim())
        val params: GeneratorParams = engine.createGeneratorParams()
        params.setSearchOption("max_length", maxTokens.toDouble())
        params.setSearchOption("temperature", 0.7)
        params.setSearchOption("top_p", 0.9)
        params.setSearchOption("repetition_penalty", 1.1)
        val out = engine.generate(params, formatted, null)
        return out.trim()
    }

    fun unload() {
        synchronized(lock) {
            try {
                genAI?.close()
            } catch (e: Exception) {
                Log.w(TAG, "unload error", e)
            }
            genAI = null
        }
    }

    private fun formatQwen3Prompt(user: String): String =
        "<|im_start|>user\n$user\n<|im_start|>assistant\n"
}
