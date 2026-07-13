package edge

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type StatusFunc func() HubSnapshot

func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func BuildStatus(hub StatusFunc) Status {
	snap := HubSnapshot{}
	if hub != nil {
		snap = hub()
	}
	report := currentApkReport()
	svc := edgeServiceForStatus()
	phase := edgePhaseForStatus()
	return Status{
		ApkRelayOnline:          snap.ApkRelayOnline,
		ApkRelayLastSeen:        snap.ApkRelayLastSeen,
		ScrcpyBridgeConnected:   snap.ScrcpyBridgeConnected,
		PhoneReachable:          snap.ApkRelayOnline,
		EdgeReady:               edgeReadyForStatus(),
		EdgeService:             svc,
		ScrcpyBlocking:          snap.ScrcpyBlocking,
		RamReservedMb:           report.RamReservedMb,
		ModelOnDevice:           report.ModelOnDevice,
		EdgeModelOnVpc:          EdgeModelOnDisk(),
		Phase:                   phase,
		EdgeMessage:             report.Message,
		RamRequestMb:            report.RamRequestMb,
		EdgeRamCapMb:            report.EdgeRamCapMb,
		DeviceRamTotalMb:        report.DeviceRamTotalMb,
		DeviceRamAvailMb:        report.DeviceRamAvailMb,
		EdgeRamMaxDeliverableMb: report.EdgeRamMaxDeliverableMb,
		RamBudgetMb:             report.RamRequestMb,
	}
}

func StatusHandler(hub StatusFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, http.StatusOK, BuildStatus(hub))
	}
}

func resolveTier(reqTier string, snap HubSnapshot) string {
	t := strings.ToLower(strings.TrimSpace(reqTier))
	switch t {
	case "light", "deep":
		return t
	default:
		if snap.ApkRelayOnline && !snap.ScrcpyBlocking {
			return "deep"
		}
		return "light"
	}
}

func InferHandler(hub StatusFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		start := time.Now()
		snap := HubSnapshot{}
		if hub != nil {
			snap = hub()
		}

		var req InferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
			return
		}
		prompt := strings.TrimSpace(req.Prompt)
		if prompt == "" {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "empty_prompt"})
			return
		}
		if len(prompt) > 2000 {
			sendJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt_too_long"})
			return
		}

		if snap.ScrcpyBlocking {
			sendJSON(w, http.StatusConflict, InferResponse{
				Status:    "error",
				Error:     "heavy_job_busy: stop scrcpy/remote session before edge infer",
				Prompt:    prompt,
				TierUsed:  resolveTier(req.Tier, snap),
				LatencyMs: int(time.Since(start).Milliseconds()),
			})
			return
		}

		tierUsed := resolveTier(req.Tier, snap)
		latency := int(time.Since(start).Milliseconds())

		if tierUsed == "deep" {
			if !snap.ApkRelayOnline {
				sendJSON(w, http.StatusServiceUnavailable, InferResponse{
					Status:    "error",
					Error:     "apk_relay_offline: no recent APK HTTP activity (outbox/logs). Open the relay app on the phone.",
					Prompt:    prompt,
					TierUsed:  tierUsed,
					Engine:    "none",
					LatencyMs: latency,
				})
				return
			}
			jobID := QueueInfer(prompt)
			res, ok := WaitInferResult(jobID)
			latency = int(time.Since(start).Milliseconds())
			if !ok {
				sendJSON(w, http.StatusGatewayTimeout, InferResponse{
					Status:    "error",
					Error:     "infer_wait_failed",
					Prompt:    prompt,
					TierUsed:  tierUsed,
					Engine:    "qwen-phone-onnx",
					LatencyMs: latency,
				})
				return
			}
			if res.Error != "" {
				sendJSON(w, http.StatusOK, InferResponse{
					Status:    "error",
					Error:     res.Error,
					Content:   res.Content,
					Prompt:    prompt,
					TierUsed:  tierUsed,
					Engine:    "qwen-phone-onnx",
					RamPeakMb: currentApkReport().RamReservedMb,
					LatencyMs: res.LatencyMs,
				})
				return
			}
			sendJSON(w, http.StatusOK, InferResponse{
				Content:   res.Content,
				Engine:    "qwen-phone-onnx",
				TierUsed:  tierUsed,
				RamPeakMb: currentApkReport().RamReservedMb,
				LatencyMs: res.LatencyMs,
				Status:    "ok",
				Prompt:    prompt,
			})
			return
		}

		sendJSON(w, http.StatusOK, InferResponse{
			Content: fmt.Sprintf(
				"[stub L1] Tier light pour « %s ». Routage Qwen VPC à brancher.",
				prompt,
			),
			Engine:    "vpc-stub",
			TierUsed:  tierUsed,
			RamPeakMb: 0,
			LatencyMs: latency,
			Status:    "stub",
			Prompt:    prompt,
		})
	}
}

func WakeHandler(hub StatusFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		snap := HubSnapshot{}
		if hub != nil {
			snap = hub()
		}
		if !snap.ApkRelayOnline {
			sendJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "apk_relay_offline"})
			return
		}
		ram := 2048
		var body WakeRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			if body.RamRequestMb > 0 {
				ram = body.RamRequestMb
			} else if body.RamBudgetMb > 0 {
				ram = body.RamBudgetMb
			}
		}
		effective, capped := QueueWake(ram)
		msg := "wake queued for APK — poll within ~2s"
		if capped && apkReportFresh() {
			report := currentApkReport()
			msg = fmt.Sprintf("requested %d MB → queued %d MB (tel max %d MB)", ram, effective, report.EdgeRamMaxDeliverableMb)
		}
		sendJSON(w, http.StatusAccepted, map[string]interface{}{
			"status":         "queued",
			"message":        msg,
			"ram_request_mb": effective,
		})
	}
}

func StopHandler(hub StatusFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		QueueStop()
		sendJSON(w, http.StatusOK, map[string]string{
			"status":  "queued",
			"message": "stop queued for APK",
		})
	}
}
