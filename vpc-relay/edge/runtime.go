package edge

import (
	"sync"
	"time"
)

// ApkReport is the latest status pushed by the paired APK.
type ApkReport struct {
	EdgeService            string    `json:"edge_service"`
	RamRequestMb           int       `json:"ram_request_mb"`
	RamReservedMb          int       `json:"ram_reserved_mb"`
	EdgeRamCapMb           int       `json:"edge_ram_cap_mb"`
	DeviceRamTotalMb       int       `json:"device_ram_total_mb"`
	DeviceRamAvailMb       int       `json:"device_ram_avail_mb"`
	EdgeRamMaxDeliverableMb int      `json:"edge_ram_max_deliverable_mb"`
	ModelOnDevice          bool      `json:"model_on_device"`
	Message                string    `json:"message,omitempty"`
	UpdatedAt              time.Time `json:"-"`
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
	// Legacy field from older APK builds.
	RamBudgetMb int `json:"ram_budget_mb"`
}

// SyncResponse VPC → APK.
type SyncResponse struct {
	Command      string `json:"command"` // none | wake | stop
	RamRequestMb int    `json:"ram_request_mb,omitempty"`
}

// WakeRequest optional body on POST /api/web/edge/wake.
type WakeRequest struct {
	RamRequestMb int `json:"ram_request_mb"`
	RamBudgetMb  int `json:"ram_budget_mb"` // legacy front field
}

const apkReportStale = 90 * time.Second

var (
	runtimeMu        sync.RWMutex
	apkReport        ApkReport
	pendingCommand   string
	pendingRamRequest int
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
	runtimeMu.Unlock()
	return effective, capped
}

func QueueStop() {
	runtimeMu.Lock()
	pendingCommand = "stop"
	runtimeMu.Unlock()
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
	runtimeMu.Unlock()
}

func TakeSyncResponse() SyncResponse {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	cmd := pendingCommand
	ram := pendingRamRequest
	pendingCommand = ""
	pendingRamRequest = 0
	if cmd == "" {
		return SyncResponse{Command: "none"}
	}
	return SyncResponse{Command: cmd, RamRequestMb: ram}
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
	return apkReportFresh() && r.EdgeService == "awake"
}
