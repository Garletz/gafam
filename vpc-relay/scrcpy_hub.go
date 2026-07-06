package main

import (
	"encoding/json"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================
// === SCRCPY REMOTE CONTROL HUB (Manifest 14) ===
// ============================================================

// Message types for the scrcpy protocol
const (
	MsgTypeVideo      byte = 0x01 // H.264 NAL unit (binary)
	MsgTypeInput      byte = 0x02 // Input event (JSON)
	MsgTypeDeviceInfo byte = 0x03 // Device metadata (JSON)
	MsgTypeShell      byte = 0x04 // ADB shell I/O (binary UTF-8)
	MsgTypeHeartbeat  byte = 0x05 // Keepalive ping
)

// ScrcpyDeviceInfo holds the Android device metadata sent by the bridge
type ScrcpyDeviceInfo struct {
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Rotation int    `json:"rotation"`
}

// ScrcpyHub manages the single bridge connection and multiple viewer connections via HTTP streams
type ScrcpyHub struct {
	mu            sync.RWMutex
	bridgeWriteMu sync.Mutex      // Prevents concurrent writes to the bridge websocket
	bridge        *websocket.Conn
	bridgeReady   bool
	deviceInfo    *ScrcpyDeviceInfo
	viewers       map[chan []byte]bool
	shellViewers  map[chan []byte]bool
}

var scrcpyHub = &ScrcpyHub{
	viewers:      make(map[chan []byte]bool),
	shellViewers: make(map[chan []byte]bool),
}

var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  64 * 1024, // 64KB for H.264 frames
	WriteBufferSize: 64 * 1024,
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from gafam.cloud subdomains and localhost
		origin := r.Header.Get("Origin")
		if origin == "" {
			return true // Non-browser clients (Manager bridge)
		}
		return isAllowedOrigin(origin)
	},
}

func isAllowedOrigin(origin string) bool {
	allowedSuffixes := []string{".gafam.cloud", "://gafam.cloud", "://localhost:5173", "://localhost:4173", "://localhost:1420"}
	for _, suffix := range allowedSuffixes {
		if len(origin) >= len(suffix) && origin[len(origin)-len(suffix):] == suffix {
			return true
		}
	}
	return false
}

// --- Bridge Handler ---
// The GAFAM Manager connects here to stream H.264 video and receive input commands

func scrcpyBridgeHandler(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"

	scrcpyHub.mu.RLock()
	oldBridge := scrcpyHub.bridge
	scrcpyHub.mu.RUnlock()

	if oldBridge != nil && !force {
		http.Error(w, `{"error":"A bridge is already connected"}`, http.StatusConflict)
		return
	}

	// If we are forcing, close the old connection to unblock its read loop
	if oldBridge != nil && force {
		oldBridge.Close()
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Scrcpy bridge upgrade error:", err)
		return
	}

	scrcpyHub.mu.Lock()
	scrcpyHub.bridge = conn
	scrcpyHub.bridgeReady = true
	scrcpyHub.mu.Unlock()

	log.Println("Scrcpy bridge connected")

	// Configure connection
	conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		return nil
	})

	// Start ping ticker for keepalive
	pingTicker := time.NewTicker(10 * time.Second)
	defer pingTicker.Stop()

	go func() {
		for range pingTicker.C {
			scrcpyHub.mu.RLock()
			b := scrcpyHub.bridge
			scrcpyHub.mu.RUnlock()
			if b == nil {
				return
			}
			scrcpyHub.bridgeWriteMu.Lock()
			err := b.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			scrcpyHub.bridgeWriteMu.Unlock()
			if err != nil {
				return
			}
		}
	}()

	defer func() {
		scrcpyHub.mu.Lock()
		if scrcpyHub.bridge == conn {
			scrcpyHub.bridge = nil
			scrcpyHub.bridgeReady = false
			scrcpyHub.deviceInfo = nil
		}
		scrcpyHub.mu.Unlock()
		conn.Close()
		log.Println("Scrcpy bridge disconnected")
	}()

	for {
		msgType, data, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Println("Scrcpy bridge read error:", err)
			}
			break
		}

		// Reset read deadline on any message
		conn.SetReadDeadline(time.Now().Add(30 * time.Second))

		if len(data) == 0 {
			continue
		}

		switch data[0] {
		case MsgTypeVideo:
			// Binary H.264 frame — broadcast to all viewers
			if msgType == websocket.BinaryMessage {
				scrcpyHub.broadcastToViewers(data)
			}

		case MsgTypeDeviceInfo:
			// JSON device info — parse and store, then broadcast
			var info ScrcpyDeviceInfo
			if err := json.Unmarshal(data[1:], &info); err == nil {
				scrcpyHub.mu.Lock()
				scrcpyHub.deviceInfo = &info
				scrcpyHub.mu.Unlock()
				log.Printf("Scrcpy device: %s (%dx%d)", info.Name, info.Width, info.Height)
				scrcpyHub.broadcastToViewers(data)
			}

		case MsgTypeShell:
			// Shell output from bridge — forward to shell viewers
			scrcpyHub.broadcastToShellViewers(data)

		case MsgTypeHeartbeat:
			// Just a keepalive, already handled by read deadline reset
		}
	}
}

// broadcastToViewers sends a message to all connected viewer channels
func (hub *ScrcpyHub) broadcastToViewers(data []byte) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	for viewerCh := range hub.viewers {
		select {
		case viewerCh <- data:
		default:
			// Viewer is too slow or buffer full, drop frame
		}
	}
}

// broadcastToShellViewers sends a message to all connected shell viewer channels
func (hub *ScrcpyHub) broadcastToShellViewers(data []byte) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	for viewerCh := range hub.shellViewers {
		select {
		case viewerCh <- data:
		default:
		}
	}
}

// --- Viewer HTTP Stream Handlers ---
// Web browsers connect here to receive H.264 stream and send input events via pure HTTP

func scrcpyVideoStreamHandler(w http.ResponseWriter, r *http.Request) {
	scrcpyHub.mu.RLock()
	bridgeReady := scrcpyHub.bridgeReady
	deviceInfo := scrcpyHub.deviceInfo
	scrcpyHub.mu.RUnlock()

	if !bridgeReady {
		http.Error(w, `{"error":"No bridge connected"}`, http.StatusServiceUnavailable)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if deviceInfo != nil {
		infoJSON, _ := json.Marshal(deviceInfo)
		w.Header().Set("X-Scrcpy-Device", string(infoJSON))
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Create channel with buffer for 100 frames to absorb network jitter
	ch := make(chan []byte, 100)
	scrcpyHub.mu.Lock()
	scrcpyHub.viewers[ch] = true
	scrcpyHub.mu.Unlock()

	log.Println("Scrcpy viewer connected (HTTP stream)")

	defer func() {
		scrcpyHub.mu.Lock()
		delete(scrcpyHub.viewers, ch)
		scrcpyHub.mu.Unlock()
		log.Println("Scrcpy viewer disconnected (HTTP stream)")
	}()

	notify := r.Context().Done()

	// Flush headers immediately
	flusher.Flush()

	// If device info is already present, push it immediately as a frame
	if deviceInfo != nil {
		infoJSON, _ := json.Marshal(deviceInfo)
		msg := make([]byte, 1+len(infoJSON))
		msg[0] = MsgTypeDeviceInfo
		copy(msg[1:], infoJSON)
		
		frame := make([]byte, 4+len(msg))
		frame[0] = byte(len(msg) >> 24)
		frame[1] = byte(len(msg) >> 16)
		frame[2] = byte(len(msg) >> 8)
		frame[3] = byte(len(msg))
		copy(frame[4:], msg)
		w.Write(frame)
		flusher.Flush()
	}

	for {
		select {
		case <-notify:
			return
		case data := <-ch:
			// Format: [4 bytes uint32 length] [payload]
			frame := make([]byte, 4+len(data))
			frame[0] = byte(len(data) >> 24)
			frame[1] = byte(len(data) >> 16)
			frame[2] = byte(len(data) >> 8)
			frame[3] = byte(len(data))
			copy(frame[4:], data)

			_, err := w.Write(frame)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func scrcpyInputHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	scrcpyHub.mu.RLock()
	bridge := scrcpyHub.bridge
	scrcpyHub.mu.RUnlock()

	if bridge == nil {
		http.Error(w, "Bridge not connected", http.StatusServiceUnavailable)
		return
	}

	// Prepare payload: [MsgTypeInput] [JSON]
	msg := make([]byte, 1+len(body))
	msg[0] = MsgTypeInput
	copy(msg[1:], body)

	scrcpyHub.bridgeWriteMu.Lock()
	err = bridge.WriteMessage(websocket.BinaryMessage, msg)
	scrcpyHub.bridgeWriteMu.Unlock()

	if err != nil {
		http.Error(w, "Write error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// --- Shell HTTP Stream Handlers ---

func scrcpyShellStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	scrcpyHub.mu.RLock()
	bridgeReady := scrcpyHub.bridgeReady
	scrcpyHub.mu.RUnlock()

	if !bridgeReady {
		http.Error(w, `{"error":"No bridge connected"}`, http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	ch := make(chan []byte, 100)
	scrcpyHub.mu.Lock()
	scrcpyHub.shellViewers[ch] = true
	scrcpyHub.mu.Unlock()

	log.Println("Scrcpy shell connected (HTTP stream)")

	defer func() {
		scrcpyHub.mu.Lock()
		delete(scrcpyHub.shellViewers, ch)
		scrcpyHub.mu.Unlock()
		log.Println("Scrcpy shell disconnected (HTTP stream)")
	}()

	notify := r.Context().Done()
	flusher.Flush()

	for {
		select {
		case <-notify:
			return
		case data := <-ch:
			// Format: [4 bytes uint32 length] [payload]
			if len(data) > 1 {
				payload := data[1:] // Strip MsgTypeShell prefix
				frame := make([]byte, 4+len(payload))
				frame[0] = byte(len(payload) >> 24)
				frame[1] = byte(len(payload) >> 16)
				frame[2] = byte(len(payload) >> 8)
				frame[3] = byte(len(payload))
				copy(frame[4:], payload)

				_, err := w.Write(frame)
				if err != nil {
					return
				}
				flusher.Flush()
			}
		}
	}
}

func scrcpyShellInputHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Methods", "POST")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := ioutil.ReadAll(r.Body)
	if err != nil || len(body) == 0 {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}

	scrcpyHub.mu.RLock()
	bridge := scrcpyHub.bridge
	scrcpyHub.mu.RUnlock()

	if bridge == nil {
		http.Error(w, "Bridge not connected", http.StatusServiceUnavailable)
		return
	}

	msg := make([]byte, 1+len(body))
	msg[0] = MsgTypeShell
	copy(msg[1:], body)

	scrcpyHub.bridgeWriteMu.Lock()
	err = bridge.WriteMessage(websocket.BinaryMessage, msg)
	scrcpyHub.bridgeWriteMu.Unlock()

	if err != nil {
		http.Error(w, "Write error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

// --- Status Handler ---
// Returns the current state of the scrcpy hub

func scrcpyStatusHandler(w http.ResponseWriter, r *http.Request) {
	scrcpyHub.mu.RLock()
	defer scrcpyHub.mu.RUnlock()

	status := map[string]interface{}{
		"bridge_connected": scrcpyHub.bridgeReady,
		"viewer_count":     len(scrcpyHub.viewers),
		"shell_active":     len(scrcpyHub.shellViewers) > 0,
	}

	if scrcpyHub.deviceInfo != nil {
		status["device"] = scrcpyHub.deviceInfo
	}

	sendJSON(w, http.StatusOK, status)
}
