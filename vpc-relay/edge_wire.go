package main

import (
	"net/http"

	"github.com/Garletz/gafam/vpc-relay/edge"
)

func edgeHubSnapshot() edge.HubSnapshot {
	scrcpyHub.mu.RLock()
	defer scrcpyHub.mu.RUnlock()
	return edge.HubSnapshot{
		ApkRelayOnline:        apkRelayOnline(),
		ApkRelayLastSeen:      apkRelayLastSeenUTC(),
		ScrcpyBridgeConnected: scrcpyHub.bridgeReady,
		ScrcpyBlocking:        len(scrcpyHub.viewers) > 0 || len(scrcpyHub.shellViewers) > 0,
	}
}

func edgeStatusHandler(w http.ResponseWriter, r *http.Request) {
	edge.StatusHandler(edgeHubSnapshot)(w, r)
}

func edgeInferHandler(w http.ResponseWriter, r *http.Request) {
	edge.InferHandler(edgeHubSnapshot)(w, r)
}

func edgeWakeHandler(w http.ResponseWriter, r *http.Request) {
	edge.WakeHandler(edgeHubSnapshot)(w, r)
}

func edgeStopHandler(w http.ResponseWriter, r *http.Request) {
	edge.StopHandler(edgeHubSnapshot)(w, r)
}

func edgeApkSyncHandler(w http.ResponseWriter, r *http.Request) {
	touchApkRelay()
	edge.ApkSyncHandler()(w, r)
}
