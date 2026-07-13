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
    fun complete(prompt: String, maxTokens: Int = 128): String {
        val engine = genAI ?: throw IllegalStateException("model not loaded")
        val formatted = formatQwen3Prompt(prompt.trim())
        val params: GeneratorParams = engine.createGeneratorParams()
        params.setSearchOption("max_length", maxTokens.toDouble())
        params.setSearchOption("temperature", 0.3)
        params.setSearchOption("top_p", 0.9)
        params.setSearchOption("repetition_penalty", 1.05)
        val raw = engine.generate(params, formatted, null)
        return extractAssistantReply(raw)
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
        "<|im_start|>user\n/no_think\n$user\n\n<|im_start|>assistant\n"

    /** Strip Qwen3 chat template echo + optional thinking blocks. */
    private fun extractAssistantReply(raw: String): String {
        var text = raw.trim()
        if (text.isEmpty()) return text

        val assistantMarkers = listOf("<|im_start|>assistant", "assistant\n", "assistant\r\n")
        for (marker in assistantMarkers) {
            val idx = text.lastIndexOf(marker)
            if (idx >= 0) {
                text = text.substring(idx + marker.length).trimStart('\n', '\r', ' ')
                break
            }
        }

        text = stripThinkBlocks(text)
        text = text.replace("<|im_start|>", "")
        text = text.replace("<|im_end|>", "")
        return text.trim()
    }

    private fun stripThinkBlocks(text: String): String {
        val open = "<" + "think" + ">"
        val close = "</" + "think" + ">"
        var out = text
        while (true) {
            val start = out.indexOf(open)
            if (start < 0) break
            val end = out.indexOf(close, start)
            if (end < 0) break
            out = out.removeRange(start, end + close.length)
        }
        return out
    }
}
