package edge

import (
	"sync"
	"time"
)

// ApkReport is the latest status pushed by the paired APK.
type ApkReport struct {
	EdgeService     string    `json:"edge_service"`
	RamBudgetMb     int       `json:"ram_budget_mb"`
	RamReservedMb   int       `json:"ram_reserved_mb"`
	ModelOnDevice   bool      `json:"model_on_device"`
	Message         string    `json:"message,omitempty"`
	UpdatedAt       time.Time `json:"-"`
}

// SyncRequest APK → VPC on POST /api/auth/edge/sync.
type SyncRequest struct {
	EdgeService   string `json:"edge_service"`
	RamBudgetMb   int    `json:"ram_budget_mb"`
	RamReservedMb int    `json:"ram_reserved_mb"`
	ModelOnDevice bool   `json:"model_on_device"`
	Message       string `json:"message,omitempty"`
}

// SyncResponse VPC → APK.
type SyncResponse struct {
	Command     string `json:"command"` // none | wake | stop
	RamBudgetMb int    `json:"ram_budget_mb,omitempty"`
}

// WakeRequest optional body on POST /api/web/edge/wake.
type WakeRequest struct {
	RamBudgetMb int `json:"ram_budget_mb"`
}

const apkReportStale = 90 * time.Second

var (
	runtimeMu       sync.RWMutex
	apkReport       ApkReport
	pendingCommand  string
	pendingRamBudget int
)

func QueueWake(ramBudgetMb int) {
	if ramBudgetMb < 512 {
		ramBudgetMb = 2048
	}
	if ramBudgetMb > 4096 {
		ramBudgetMb = 4096
	}
	runtimeMu.Lock()
	pendingCommand = "wake"
	pendingRamBudget = ramBudgetMb
	runtimeMu.Unlock()
}

func QueueStop() {
	runtimeMu.Lock()
	pendingCommand = "stop"
	runtimeMu.Unlock()
}

func UpdateApkReport(req SyncRequest) {
	runtimeMu.Lock()
	apkReport = ApkReport{
		EdgeService:   req.EdgeService,
		RamBudgetMb:   req.RamBudgetMb,
		RamReservedMb: req.RamReservedMb,
		ModelOnDevice: req.ModelOnDevice,
		Message:       req.Message,
		UpdatedAt:     time.Now(),
	}
	runtimeMu.Unlock()
}

func TakeSyncResponse() SyncResponse {
	runtimeMu.Lock()
	defer runtimeMu.Unlock()
	cmd := pendingCommand
	ram := pendingRamBudget
	pendingCommand = ""
	pendingRamBudget = 0
	if cmd == "" {
		return SyncResponse{Command: "none"}
	}
	return SyncResponse{Command: cmd, RamBudgetMb: ram}
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
	return apkReportFresh() && currentApkReport().EdgeService == "awake"
}
