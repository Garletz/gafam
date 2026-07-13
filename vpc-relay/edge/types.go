package edge

// HubSnapshot is filled by vpc-relay from APK relay + scrcpy state.
type HubSnapshot struct {
	ApkRelayOnline        bool
	ApkRelayLastSeen      string
	ScrcpyBridgeConnected bool
	ScrcpyBlocking        bool
}

// Status is returned by GET /api/web/edge/status.
type Status struct {
	ApkRelayOnline        bool   `json:"apk_relay_online"`
	ApkRelayLastSeen      string `json:"apk_relay_last_seen,omitempty"`
	ScrcpyBridgeConnected bool   `json:"scrcpy_bridge_connected"`
	EdgeReady             bool   `json:"edge_ready"`
	EdgeService           string `json:"edge_service"` // not_deployed | idle | loading | inferring
	ScrcpyBlocking        bool   `json:"scrcpy_blocking"`
	RamReservedMb         int    `json:"ram_reserved_mb"`
	ModelOnDevice         bool   `json:"model_on_device"`
	Phase                 string `json:"phase"`
	EdgeMessage           string `json:"edge_message,omitempty"`
	RamBudgetMb           int    `json:"ram_budget_mb,omitempty"`
	// Deprecated: use apk_relay_online. Kept for older front builds.
	PhoneReachable bool `json:"phone_reachable"`
}

// InferRequest POST /api/web/edge/infer
type InferRequest struct {
	Prompt      string `json:"prompt"`
	Tier        string `json:"tier"` // auto | light | deep
	RamBudgetMb int    `json:"ram_budget_mb"`
}

// InferResponse one-shot test reply (no session history).
type InferResponse struct {
	Content   string `json:"content"`
	Engine    string `json:"engine"`
	TierUsed  string `json:"tier_used"`
	RamPeakMb int    `json:"ram_peak_mb"`
	LatencyMs int    `json:"latency_ms"`
	Status    string `json:"status"` // ok | stub | error
	Error     string `json:"error,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
}
