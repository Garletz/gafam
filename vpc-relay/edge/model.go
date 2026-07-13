package edge

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

var edgeModelFiles = []string{
	"chat_template.jinja",
	"config.json",
	"genai_config.json",
	"model.onnx",
	"tokenizer.json",
	"tokenizer_config.json",
}

func edgeDataRoot() string {
	if os.Getenv("ENV") == "development" {
		return "."
	}
	return "/app/data"
}

func EdgeModelDir() string {
	if p := os.Getenv("EDGE_ONNX_MODEL_DIR"); p != "" {
		return p
	}
	return filepath.Join(edgeDataRoot(), "edge", "qwen3-onnx")
}

func EdgeModelOnDisk() bool {
	dir := EdgeModelDir()
	for _, name := range edgeModelFiles {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || st.Size() == 0 {
			return false
		}
	}
	return true
}

type ModelManifest struct {
	Ready bool              `json:"ready"`
	Dir   string            `json:"dir,omitempty"`
	Files []ModelFileEntry  `json:"files"`
}

type ModelFileEntry struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	Sha256 string `json:"sha256,omitempty"`
}

func buildModelManifest() ModelManifest {
	dir := EdgeModelDir()
	entries := make([]ModelFileEntry, 0, len(edgeModelFiles))
	ready := true
	for _, name := range edgeModelFiles {
		path := filepath.Join(dir, name)
		st, err := os.Stat(path)
		if err != nil || st.Size() == 0 {
			ready = false
			entries = append(entries, ModelFileEntry{Name: name, Size: 0})
			continue
		}
		entries = append(entries, ModelFileEntry{Name: name, Size: st.Size()})
	}
	return ModelManifest{Ready: ready, Files: entries}
}

func ModelManifestHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, http.StatusOK, buildModelManifest())
	}
}

func ModelFileHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := filepath.Base(r.PathValue("file"))
		if !isAllowedModelFile(name) {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "unknown_file"})
			return
		}
		path := filepath.Join(EdgeModelDir(), name)
		if _, err := os.Stat(path); err != nil {
			sendJSON(w, http.StatusNotFound, map[string]string{"error": "file_missing_on_vpc"})
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, path)
	}
}

func isAllowedModelFile(name string) bool {
	for _, f := range edgeModelFiles {
		if f == name {
			return true
		}
	}
	return false
}

func modelStatusJSON() map[string]interface{} {
	m := buildModelManifest()
	raw, _ := json.Marshal(m)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}
