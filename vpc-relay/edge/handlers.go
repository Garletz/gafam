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
	return Status{
		ApkRelayOnline:        snap.ApkRelayOnline,
		ApkRelayLastSeen:      snap.ApkRelayLastSeen,
		ScrcpyBridgeConnected: snap.ScrcpyBridgeConnected,
		PhoneReachable:        snap.ApkRelayOnline,
		EdgeReady:             false,
		EdgeService:           "not_deployed",
		ScrcpyBlocking:        snap.ScrcpyBlocking,
		RamReservedMb:         0,
		ModelOnDevice:         false,
		Phase:                 "2b_stub",
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
				Status:   "error",
				Error:    "heavy_job_busy: stop scrcpy/remote session before edge infer",
				Prompt:   prompt,
				TierUsed: resolveTier(req.Tier, snap),
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
			sendJSON(w, http.StatusOK, InferResponse{
				Content: fmt.Sprintf(
					"[stub L2] Prompt reçu par le VPC : « %s ». EdgeInferenceService APK pas encore déployé — prochaine étape Phase 2c.",
					prompt,
				),
				Engine:    "edge-stub",
				TierUsed:  tierUsed,
				RamPeakMb: 0,
				LatencyMs: latency,
				Status:    "stub",
				Prompt:    prompt,
			})
			return
		}

		sendJSON(w, http.StatusOK, InferResponse{
			Content: fmt.Sprintf(
				"[stub L1] Tier light sélectionné pour « %s ». Routage Qwen VPC à brancher — utilise VPC 1 RAM pour les tests L1 réels.",
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
		if snap.ScrcpyBlocking {
			sendJSON(w, http.StatusConflict, map[string]string{"error": "heavy_job_busy"})
			return
		}
		if !snap.ApkRelayOnline {
			sendJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "apk_relay_offline"})
			return
		}
		sendJSON(w, http.StatusAccepted, map[string]string{
			"status":  "stub",
			"message": "wake requested — EdgeInferenceService not deployed yet",
		})
	}
}

func StopHandler(hub StatusFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		sendJSON(w, http.StatusOK, map[string]string{
			"status":  "stub",
			"message": "stop acknowledged — nothing loaded on phone yet",
		})
	}
}
