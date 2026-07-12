#!/usr/bin/env python3
"""Suparna LLM runner — Qwen via onnxruntime-genai. Reads prompt on stdin, prints JSON Reading on stdout."""
import argparse
import json
import re
import sys

def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--model", required=True, help="ONNX genai model directory")
    args = parser.parse_args()
    prompt = sys.stdin.read()
    if not prompt.strip():
        print(json.dumps({"error": "empty prompt"}), file=sys.stderr)
        sys.exit(1)

    try:
        import onnxruntime_genai as og
    except ImportError:
        print(json.dumps({"error": "onnxruntime_genai not installed"}), file=sys.stderr)
        sys.exit(2)

    model = og.Model(args.model)
    tokenizer = og.Tokenizer(model)
    params = og.GeneratorParams(model)
    params.set_search_options(max_length=768, temperature=0.4, top_p=0.9)

    system = (
        "Tu es Suparna. Analyse des logs Android GAFAM. "
        "Réponds UNIQUEMENT avec un objet JSON valide. Langue du summary: français. "
        "Ne invente pas d'événements absents des logs."
    )
    full = system + "\n\n" + prompt + "\n\nJSON:"

    input_tokens = tokenizer.encode(full)
    generator = og.Generator(model, params)
    generator.append_tokens(input_tokens)

    out_tokens = []
    while not generator.is_done():
        generator.generate_next_token()
        out_tokens.append(generator.get_next_tokens()[0])

    text = tokenizer.decode(out_tokens).strip()
    # Extract JSON object from model output
    m = re.search(r"\{[\s\S]*\}", text)
    if not m:
        print(json.dumps({"error": "no json in output", "raw": text[:500]}), file=sys.stderr)
        sys.exit(3)
    obj = json.loads(m.group())
    print(json.dumps(obj, ensure_ascii=False))

if __name__ == "__main__":
    main()
