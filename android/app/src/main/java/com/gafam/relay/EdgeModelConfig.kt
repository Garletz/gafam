package com.gafam.relay

object EdgeModelConfig {
    const val MODEL_DIR_NAME = "qwen3-edge"
    const val BASE_URL =
        "https://huggingface.co/onnx-community/Qwen3-0.6B-ONNX/resolve/main/onnxruntime/cpu_and_mobile/cpu-int4-kld-block-128/"

    val FILES = listOf(
        "chat_template.jinja",
        "config.json",
        "genai_config.json",
        "model.onnx",
        "tokenizer.json",
        "tokenizer_config.json"
    )
}
