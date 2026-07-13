package com.gafam.relay

object EdgeModelConfig {
    const val MODEL_DIR_NAME = "qwen3-edge"

    val FILES = listOf(
        "chat_template.jinja",
        "config.json",
        "genai_config.json",
        "model.onnx",
        "tokenizer.json",
        "tokenizer_config.json"
    )
}
