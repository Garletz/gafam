package main

import (
	"sync"
	"time"
)

const apkRelayOnlineWindow = 45 * time.Second

var (
	apkRelayMu       sync.RWMutex
	apkRelayLastSeen time.Time
)

// touchApkRelay records recent HTTP activity from the paired APK relay (outbox, logs, SMS).
func touchApkRelay() {
	apkRelayMu.Lock()
	apkRelayLastSeen = time.Now()
	apkRelayMu.Unlock()
}

func apkRelayOnline() bool {
	apkRelayMu.RLock()
	defer apkRelayMu.RUnlock()
	if apkRelayLastSeen.IsZero() {
		return false
	}
	return time.Since(apkRelayLastSeen) <= apkRelayOnlineWindow
}

func apkRelayLastSeenUTC() string {
	apkRelayMu.RLock()
	defer apkRelayMu.RUnlock()
	if apkRelayLastSeen.IsZero() {
		return ""
	}
	return apkRelayLastSeen.UTC().Format(time.RFC3339)
}
