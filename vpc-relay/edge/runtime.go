package edge

import (
	"sync"
	"time"
)

// ApkReport is the latest status pushed by the paired APK.
type ApkReport struct {
	EdgeService             string    `json:"edge_service"`
	RamRequestMb            int       `json:"ram_request_mb"`
	RamReservedMb           int       `json:"ram_reserved_mb"`
	EdgeRamCapMb            int       `json:"edge_ram_cap_mb"`
	DeviceRamTotalMb        int       `json:"device_ram_total_mb"`
	DeviceRamAvailMb        int       `json:"device_ram_avail_mb"`
	EdgeRamMaxDeliverableMb int       `json:"edge_ram_max_deliverable_mb"`
	ModelOnDevice           bool      `json:"model_on_device"`
	Message                 string    `json:"message,omitempty"`
	UpdatedAt               time.Time `json:"-"`
}

// SyncRequest APK → VPC on POST /api/auth/edge/sync.
type SyncRequest struct {
	EdgeService             string `json:"edge_service"`
	RamRequestMb            int    `json:"ram_request_mb"`
	RamReservedMb           int    `json:"ram_reserved_mb"`
	EdgeRamCapMb            int    `json:"edge_ram_cap_mb"`
	DeviceRamTotalMb        int    `json:"device_ram_total_mb"`
	DeviceRamAvailMb        int    `json:"device_ram_avail_mb"`
	EdgeRamMaxDeliverableMb int    `json:"edge_ram_max_deliverable_mb"`
	ModelOnDevice           bool   `json:"model_on_device"`
	Message                 string `json:"message,omitempty"`
	InferJobID              string `json:"infer_job_id,omitempty"`
	InferContent            string `json:"infer_content,omitempty"`
	InferError              string `json:"infer_error,omitempty"`
	InferLatencyMs          int    `json:"infer_latency_ms,omitempty"`
	RamBudgetMb             int    `json:"ram_budget_mb"`
}

// SyncResponse VPC → APK.
type SyncResponse struct {
	Command      string `json:"command"` // none | wake | stop | infer
	RamRequestMb int    `json:"ram_request_mb,omitempty"`
	JobID        string `json:"job_id,omitempty"`
	Prompt       string `json:"prompt,omitempty"`
}

// WakeRequest optional body on POST /api/web/edge/wake.
type WakeRequest struct {
	RamRequestMb int `json:"ram_request_mb"`
	RamBudgetMb  int `json:"ram_budget_mb"`
}

// InferResult delivered when APK completes a queued job.
type InferResult struct {
	JobID     string
	Content   string
	Error     string
	LatencyMs int
}

const apkReportStale = 90 * time.Second
const inferWaitTimeout = 180 * time.Second

var (
	runtimeMu         sync.RWMutex
	apkReport         ApkReport
	pendingCommand    string
	pendingRamRequest int
	pendingJobID      string
	pendingPrompt     string
	inferWaiters      = map[string]chan InferResult{}
)

func clampRequestToPhone(requested int) (int, bool) {
	report := currentApkReport()
	if !apkReportFresh() || report.EdgeRamMaxDeliverableMb <= 0 {
		if requested < 512 {
			requested = 512
		}
		if requested > 4096 {
			requested = 4096
		}
		return requested, true
	}
	if requested < 512 {
		requested = 512
	}
	if requested > report.EdgeRamMaxDeliverableMb {
		return report.EdgeRamMaxDeliverableMb, false
	}
	return requested, true
}

func QueueWake(ramRequestMb int) (int, bool) {
	effective, capped := clampRequestToPhone(ramRequestMb)
	runtimeMu.Lock()
	pendingCommand = "wake"
	pendingRamRequest = effective
	pendingJobID = ""
	pendingPrompt = ""
	runtimeMu.Unlock()
	return effective, capped
}

func QueueStop() {
	runtimeMu.Lock()
	pendingCommand = "stop"
	pendingJobID = ""
	pendingPrompt = ""
	runtimeMu.Unlock()
}

func QueueInfer(prompt string) string {
	jobID := newJobID()
	runtimeMu.Lock()
	pendingCommand = "infer"
	pendingJobID = jobID
	pendingPrompt = prompt
	pendingRamRequest = 0
	ch := make(chan InferResult, 1)
	inferWaiters[jobID] = ch
	runtimeMu.Unlock()
	return jobID
}

func WaitInferResult(jobID string) (InferResult, bool) {
	runtimeMu.RLock()
	ch, ok := inferWaiters[jobID]
	runtimeMu.RUnlock()
	if !ok {
		return InferResult{}, false
	}
	select {
	case res := <-ch:
		runtimeMu.Lock()
		delete(inferWaiters, jobID)
		runtimeMu.Unlock()
		return res, true
	case <-time.After(inferWaitTimeout):
		runtimeMu.Lock()
		delete(inferWaiters, jobID)
		runtimeMu.Unlock()
		return InferResult{JobID: jobID, Error: "infer_timeout: APK did not respond within 180s"}, true
	}
}

func UpdateApkReport(req SyncRequest) {
	ramReq := req.RamRequestMb
	if ramReq == 0 {
		ramReq = req.RamBudgetMb
	}
	runtimeMu.Lock()
	apkReport = ApkReport{
		EdgeService:             req.EdgeService,
		RamRequestMb:            ramReq,
		RamReservedMb:           req.RamReservedMb,
		EdgeRamCapMb:            req.EdgeRamCapMb,
		DeviceRamTotalMb:        req.DeviceRamTotalMb,
		DeviceRamAvailMb:        req.DeviceRamAvailMb,
		EdgeRamMaxDeliverableMb: req.EdgeRamMaxDeliverableMb,
		ModelOnDevice:           req.ModelOnDevice,
		Message:                 req.Message,
		UpdatedAt:               time.Now(),
	}
	if req.InferJobID != "" {
		if ch, ok := inferWaiters[req.InferJobID]; ok {
			ch <- InferResult{
				JobID:     req.InferJobID,
				Content:   req.InferContent,
				Error:     req.InferError,
				LatencyMs: req.InferLatencyMs,
			}
			delete(inferWaiters, req.InferJobID)
		}
	}
	runtimeMu.Unlock()
}

func TakeSyncResponse() SyncResponse {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	cmd := pendingCommand
	ram := pendingRamRequest
	job := pendingJobID
	prompt := pendingPrompt
	pendingCommand = ""
	pendingRamRequest = 0
	pendingJobID = ""
	pendingPrompt = ""
	if cmd == "" {
		return SyncResponse{Command: "none"}
	}
	resp := SyncResponse{Command: cmd}
	switch cmd {
	case "wake":
		resp.RamRequestMb = ram
	case "infer":
		resp.JobID = job
		resp.Prompt = prompt
	}
	return resp
}

func currentApkReport() ApkReport {
	runtimeMu.RLock()
	defer runtimeMu.RUnlock()
	return apkReport
}

func apkReportFresh() bool {
	r := currentApkReport()
	if r.UpdatedAt.IsZero() {
		return false
	}
	return time.Since(r.UpdatedAt) <= apkReportStale
}

func edgeServiceForStatus() string {
	r := currentApkReport()
	if !apkReportFresh() {
		return "not_deployed"
	}
	if r.EdgeService == "" {
		return "idle"
	}
	return r.EdgeService
}

func edgeReadyForStatus() bool {
	r := currentApkReport()
	return apkReportFresh() && r.ModelOnDevice && (r.EdgeService == "awake" || r.EdgeService == "inferring")
}

func edgePhaseForStatus() string {
	if !apkReportFresh() {
		return "2c_waiting_apk"
	}
	r := currentApkReport()
	if r.ModelOnDevice {
		return "2c-2"
	}
	return "2c"
}

func newJobID() string {
	return time.Now().UTC().Format("20060102150405") + "-" + randomHex(4)
}

func randomHex(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = hex[(now+int64(i*17))%16]
		now >>= 1
	}
	return string(b)
}
