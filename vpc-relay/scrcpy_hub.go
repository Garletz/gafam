package main

import (
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ============================================================
// === SCRCPY REMOTE CONTROL HUB (Manifest 14) ===
// ============================================================

// Message types for the scrcpy WebSocket protocol
const (
	MsgTypeVideo     byte = 0x01 // H.264 NAL unit (binary)
	MsgTypeInput     byte = 0x02 // Input event (JSON)
	MsgTypeDeviceInfo byte = 0x03 // Device metadata (JSON)
	MsgTypeShell     byte = 0x04 // ADB shell I/O (binary UTF-8)
	MsgTypeHeartbeat byte = 0x05 // Keepalive ping
)

// ScrcpyDeviceInfo holds the Android device metadata sent by the bridge
type ScrcpyDeviceInfo struct {
	Name     string `json:"name"`
	Width    int    `json:"width"`
	Height   int    `json:"height"`
	Rotation int    `json:"rotation"`
}

// ScrcpyHub manages the single bridge connection and multiple viewer connections
type ScrcpyHub struct {
	mu          sync.RWMutex
	bridge      *websocket.Conn
	bridgeReady bool
	deviceInfo  *ScrcpyDeviceInfo
	viewers     map[*websocket.Conn]bool
	controller  *websocket.Conn // The viewer currently allowed to send inputs
	shellBridge *websocket.Conn // Bridge connection for shell I/O
	shellViewer *websocket.Conn // Viewer connection for shell I/O
}

var scrcpyHub = &ScrcpyHub{
	viewers: make(map[*websocket.Conn]bool),
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
	allowedSuffixes := []string{".gafam.cloud", "://gafam.cloud", "://localhost:5173", "://localhost:4173"}
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
	scrcpyHub.mu.RLock()
	hasBridge := scrcpyHub.bridge != nil
	scrcpyHub.mu.RUnlock()

	if hasBridge {
		http.Error(w, `{"error":"A bridge is already connected"}`, http.StatusConflict)
		return
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
			if err := b.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
				return
			}
		}
	}()

	defer func() {
		scrcpyHub.mu.Lock()
		scrcpyHub.bridge = nil
		scrcpyHub.bridgeReady = false
		scrcpyHub.deviceInfo = nil
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
			// Shell output from bridge — forward to the shell viewer
			scrcpyHub.mu.RLock()
			sv := scrcpyHub.shellViewer
			scrcpyHub.mu.RUnlock()
			if sv != nil {
				sv.WriteMessage(websocket.BinaryMessage, data)
			}

		case MsgTypeHeartbeat:
			// Just a keepalive, already handled by read deadline reset
		}
	}
}

// broadcastToViewers sends a message to all connected viewer WebSockets
func (hub *ScrcpyHub) broadcastToViewers(data []byte) {
	hub.mu.RLock()
	defer hub.mu.RUnlock()

	for viewer := range hub.viewers {
		err := viewer.WriteMessage(websocket.BinaryMessage, data)
		if err != nil {
			// Will be cleaned up by the viewer's read loop
			log.Println("Scrcpy viewer write error:", err)
		}
	}
}

// --- Viewer Handler ---
// Web browsers connect here to receive H.264 stream and send input events

func scrcpyViewerHandler(w http.ResponseWriter, r *http.Request) {
	scrcpyHub.mu.RLock()
	bridgeReady := scrcpyHub.bridgeReady
	scrcpyHub.mu.RUnlock()

	if !bridgeReady {
		http.Error(w, `{"error":"No bridge connected"}`, http.StatusServiceUnavailable)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Scrcpy viewer upgrade error:", err)
		return
	}

	scrcpyHub.mu.Lock()
	scrcpyHub.viewers[conn] = true
	// First viewer gets controller privileges
	if scrcpyHub.controller == nil {
		scrcpyHub.controller = conn
		log.Println("Scrcpy viewer connected (controller)")
	} else {
		log.Println("Scrcpy viewer connected (spectator)")
	}
	// Send current device info if available
	deviceInfo := scrcpyHub.deviceInfo
	scrcpyHub.mu.Unlock()

	if deviceInfo != nil {
		infoJSON, _ := json.Marshal(deviceInfo)
		msg := make([]byte, 1+len(infoJSON))
		msg[0] = MsgTypeDeviceInfo
		copy(msg[1:], infoJSON)
		conn.WriteMessage(websocket.BinaryMessage, msg)
	}

	defer func() {
		scrcpyHub.mu.Lock()
		delete(scrcpyHub.viewers, conn)
		if scrcpyHub.controller == conn {
			scrcpyHub.controller = nil
			// Promote another viewer to controller if any
			for v := range scrcpyHub.viewers {
				scrcpyHub.controller = v
				break
			}
		}
		scrcpyHub.mu.Unlock()
		conn.Close()
		log.Println("Scrcpy viewer disconnected")
	}()

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		if len(data) == 0 {
			continue
		}

		// Only the controller can send input events
		if data[0] == MsgTypeInput {
			scrcpyHub.mu.RLock()
			isController := scrcpyHub.controller == conn
			bridge := scrcpyHub.bridge
			scrcpyHub.mu.RUnlock()

			if isController && bridge != nil {
				bridge.WriteMessage(websocket.BinaryMessage, data)
			}
		}
	}
}

// --- Shell Handler ---
// Web browser connects here for an ADB shell terminal

func scrcpyShellHandler(w http.ResponseWriter, r *http.Request) {
	// Check if shell is enabled in settings
	var shellEnabled string
	err := db.QueryRow(`SELECT value FROM gafam_settings WHERE key = 'scrcpy_shell_enabled'`).Scan(&shellEnabled)
	if err != nil || shellEnabled != "true" {
		http.Error(w, `{"error":"ADB Shell is disabled. Enable it in Settings."}`, http.StatusForbidden)
		return
	}

	scrcpyHub.mu.RLock()
	bridgeReady := scrcpyHub.bridgeReady
	hasShellViewer := scrcpyHub.shellViewer != nil
	scrcpyHub.mu.RUnlock()

	if !bridgeReady {
		http.Error(w, `{"error":"No bridge connected"}`, http.StatusServiceUnavailable)
		return
	}

	if hasShellViewer {
		http.Error(w, `{"error":"A shell session is already active"}`, http.StatusConflict)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Scrcpy shell upgrade error:", err)
		return
	}

	scrcpyHub.mu.Lock()
	scrcpyHub.shellViewer = conn
	scrcpyHub.mu.Unlock()

	log.Println("Scrcpy shell viewer connected")

	defer func() {
		scrcpyHub.mu.Lock()
		scrcpyHub.shellViewer = nil
		scrcpyHub.mu.Unlock()
		conn.Close()
		log.Println("Scrcpy shell viewer disconnected")
	}()

	conn.SetReadDeadline(time.Now().Add(120 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		return nil
	})

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		conn.SetReadDeadline(time.Now().Add(120 * time.Second))

		if len(data) == 0 {
			continue
		}

		// Forward shell input to bridge
		if data[0] == MsgTypeShell {
			scrcpyHub.mu.RLock()
			bridge := scrcpyHub.bridge
			scrcpyHub.mu.RUnlock()

			if bridge != nil {
				bridge.WriteMessage(websocket.BinaryMessage, data)
			}
		}
	}
}

// --- Status Handler ---
// Returns the current state of the scrcpy hub

func scrcpyStatusHandler(w http.ResponseWriter, r *http.Request) {
	scrcpyHub.mu.RLock()
	defer scrcpyHub.mu.RUnlock()

	status := map[string]interface{}{
		"bridge_connected": scrcpyHub.bridgeReady,
		"viewer_count":     len(scrcpyHub.viewers),
		"shell_active":     scrcpyHub.shellViewer != nil,
	}

	if scrcpyHub.deviceInfo != nil {
		status["device"] = scrcpyHub.deviceInfo
	}

	sendJSON(w, http.StatusOK, status)
}
