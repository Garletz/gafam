package suparna

import (
	"os"
	"path/filepath"
)

func dataRoot() string {
	if os.Getenv("ENV") == "development" {
		return "."
	}
	return "/app/data"
}

func ModelDir() string {
	if p := os.Getenv("SUPARNA_MODEL_DIR"); p != "" {
		return p
	}
	return filepath.Join(dataRoot(), "suparna", "model")
}

func ModelDirReady() bool {
	dir := ModelDir()
	st, err := os.Stat(dir)
	if err != nil || !st.IsDir() {
		return false
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		name := e.Name()
		if name == "genai_config.json" || filepath.Ext(name) == ".onnx" {
			return true
		}
	}
	return len(entries) > 2
}

func RunnerScript() string {
	if p := os.Getenv("SUPARNA_RUNNER"); p != "" {
		return p
	}
	return "/app/suparna/runner.py"
}
