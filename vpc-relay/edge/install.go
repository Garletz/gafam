package edge

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const edgeModelHFBase = "https://huggingface.co/onnx-community/Qwen3-0.6B-ONNX/resolve/main/onnxruntime/cpu_and_mobile/cpu-int4-kld-block-128/"

// WebModelStatus is returned by GET /api/web/edge/model.
type WebModelStatus struct {
	Ready      bool             `json:"ready"`
	Dir        string           `json:"dir,omitempty"`
	Files      []ModelFileEntry `json:"files"`
	TotalBytes int64            `json:"total_bytes"`
	Install    InstallStatus    `json:"install"`
}

type InstallStatus struct {
	Status      string `json:"status"` // idle | downloading | ready | error
	Progress    int    `json:"progress"`
	CurrentFile string `json:"current_file,omitempty"`
	Message     string `json:"message,omitempty"`
	Error       string `json:"error,omitempty"`
}

var (
	installMu sync.Mutex
	installSt InstallStatus
)

func BuildWebModelStatus() WebModelStatus {
	manifest := buildModelManifest()
	var total int64
	for _, f := range manifest.Files {
		total += f.Size
	}
	installMu.Lock()
	st := installSt
	installMu.Unlock()
	if manifest.Ready {
		st = InstallStatus{Status: "ready", Progress: 100, Message: "Modèle ONNX prêt sur le VPC"}
	} else if st.Status == "" {
		st.Status = "idle"
	}
	return WebModelStatus{
		Ready:      manifest.Ready,
		Dir:        EdgeModelDir(),
		Files:      manifest.Files,
		TotalBytes: total,
		Install:    st,
	}
}

func StartModelInstall() (string, error) {
	if EdgeModelOnDisk() {
		installMu.Lock()
		installSt = InstallStatus{Status: "ready", Progress: 100, Message: "Déjà présent"}
		installMu.Unlock()
		return "ready", nil
	}
	installMu.Lock()
	if installSt.Status == "downloading" {
		installMu.Unlock()
		return "downloading", nil
	}
	installSt = InstallStatus{
		Status:   "downloading",
		Progress: 0,
		Message:  "Téléchargement HuggingFace → disque VPC…",
	}
	installMu.Unlock()

	go runModelInstall()
	return "started", nil
}

func runModelInstall() {
	dir := EdgeModelDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		setInstallError(fmt.Sprintf("mkdir: %v", err))
		return
	}

	client := &http.Client{Timeout: 20 * time.Minute}
	var totalExpected int64
	for _, name := range edgeModelFiles {
		path := filepath.Join(dir, name)
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			totalExpected += st.Size()
			continue
		}
		if sz, err := headContentLength(client, edgeModelHFBase+name); err == nil && sz > 0 {
			totalExpected += sz
		}
	}
	if totalExpected < 1 {
		totalExpected = 536_870_912 // ~512 Mo fallback
	}

	var done int64
	for _, name := range edgeModelFiles {
		dest := filepath.Join(dir, name)
		if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
			done += st.Size()
			setInstallProgress(name, done, totalExpected, fmt.Sprintf("%s déjà présent", name))
			continue
		}

		setInstallProgress(name, done, totalExpected, fmt.Sprintf("Téléchargement %s…", name))
		written, err := downloadFile(client, edgeModelHFBase+name, dest, func(n int64) {
			setInstallProgress(name, done+n, totalExpected, fmt.Sprintf("Téléchargement %s…", name))
		})
		if err != nil {
			setInstallError(fmt.Sprintf("%s: %v", name, err))
			return
		}
		done += written
	}

	installMu.Lock()
	installSt = InstallStatus{
		Status:   "ready",
		Progress: 100,
		Message:  "Modèle ONNX prêt — le tel peut Wake",
	}
	installMu.Unlock()
	log.Printf("edge: ONNX model install complete in %s", EdgeModelDir())
}

func headContentLength(client *http.Client, url string) (int64, error) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HEAD %d", resp.StatusCode)
	}
	return resp.ContentLength, nil
}

func downloadFile(client *http.Client, url, dest string, onProgress func(int64)) (int64, error) {
	tmp := dest + ".partial"
	resp, err := client.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	var written int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				os.Remove(tmp)
				return 0, werr
			}
			written += int64(n)
			if onProgress != nil {
				onProgress(written)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			out.Close()
			os.Remove(tmp)
			return 0, readErr
		}
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return 0, err
	}
	return written, nil
}

func setInstallProgress(currentFile string, done, total int64, msg string) {
	pct := int((100 * done) / total)
	if pct > 99 {
		pct = 99
	}
	installMu.Lock()
	installSt = InstallStatus{
		Status:      "downloading",
		Progress:    pct,
		CurrentFile: currentFile,
		Message:     msg,
	}
	installMu.Unlock()
}

func setInstallError(errMsg string) {
	log.Printf("edge: model install error: %s", errMsg)
	installMu.Lock()
	installSt = InstallStatus{Status: "error", Progress: 0, Error: errMsg}
	installMu.Unlock()
}

func WebModelStatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, http.StatusOK, BuildWebModelStatus())
	}
}

func WebModelInstallHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		status, err := StartModelInstall()
		if err != nil {
			sendJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		code := http.StatusAccepted
		if status == "ready" {
			code = http.StatusOK
		}
		sendJSON(w, code, map[string]interface{}{
			"status":  status,
			"message": "Téléchargement ONNX lancé sur le VPC (HuggingFace → disque)",
			"model":   BuildWebModelStatus(),
		})
	}
}
