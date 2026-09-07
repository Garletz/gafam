package browser

// ─── Vātāyana CDP view plane ───────────────────────────────────────────────
// The "remote browser" the human watches is no longer an X11 crop of a window
// (fragile: popups change the window list, the geometry can drift between the
// displayed frame and the click, decorations skew coordinates). Instead the
// dashboard shows the page viewport captured straight from Chrome via CDP:
//
//   - capture:  Page.captureScreenshot on a loop (works headed and headless,
//               unlike Page.startScreencast which is not reliable on headful)
//   - viewport: Emulation.setDeviceMetricsOverride sets the layout viewport to
//               the dashboard frame size, so the capture is exactly the page
//               the human interacts with — coordinates are 1:1, no crop maths
//   - input:    Input.dispatchMouseEvent / dispatchKeyEvent in that same
//               viewport coordinate space
//   - control:  Page.navigate, Runtime.evaluate for title/url
//
// The previous ffmpeg/xdotool pipeline in the container stays as a fallback:
// when CDP is unreachable (or VATAYANA_VIEW=x11) the handlers proxy to the
// container's stream server exactly as before. No browser image change is
// required — this plane only talks to the already-exposed CDP endpoint.

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// cdpViewBase returns the CDP HTTP endpoint of the browser container.
// Overridable (tests, local Chrome) via BROWSER_CDP_URL.
func cdpViewBase() string {
	if v := os.Getenv("BROWSER_CDP_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://" + browserStaticIP + ":9223"
}

// ─── mode decision ─────────────────────────────────────────────────────────

var (
	viewModeMu   sync.Mutex
	viewModeVal  bool
	viewModeTime time.Time
)

// cdpViewEnabled reports whether the CDP view plane should serve the human
// browser. VATAYANA_VIEW=x11 forces the legacy container pipeline; otherwise
// CDP is preferred when the endpoint answers. Reachability is cached briefly
// so the dashboard's 8s status polls don't hammer the container.
func cdpViewEnabled() bool {
	if v := os.Getenv("VATAYANA_VIEW"); v != "" && v != "cdp" {
		return false
	}
	viewModeMu.Lock()
	defer viewModeMu.Unlock()
	if time.Since(viewModeTime) < 10*time.Second {
		return viewModeVal
	}
	viewModeVal = cdpReachable()
	viewModeTime = time.Now()
	return viewModeVal
}

func cdpReachable() bool {
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	resp, err := client.Get(cdpViewBase() + "/json/version")
	if err != nil {
		return false
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// ─── target selection ──────────────────────────────────────────────────────

type cdpTarget struct {
	ID  string `json:"id"`
	URL string `json:"url"`
	WS  string `json:"webSocketDebuggerUrl"`
}

var (
	targetMu     sync.Mutex
	pinnedTarget string
	oneShotSeq   atomic.Int64
)

// pickPageTarget returns the page target the human view is pinned to. The
// pinned target ID is stable across same-tab navigations; if it disappears
// (tab closed / Chrome restarted) the first available page is adopted.
func pickPageTarget() (cdpTarget, error) {
	targetMu.Lock()
	defer targetMu.Unlock()

	resp, err := http.Get(cdpViewBase() + "/json/list")
	if err != nil {
		return cdpTarget{}, fmt.Errorf("cdp: list targets: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return cdpTarget{}, fmt.Errorf("cdp: list targets: %s", strings.TrimSpace(string(body)))
	}
	var list []struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		URL  string `json:"url"`
		WS   string `json:"webSocketDebuggerUrl"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		return cdpTarget{}, fmt.Errorf("cdp: decode targets: %w", err)
	}

	var pages []cdpTarget
	for _, t := range list {
		if t.Type != "page" || t.WS == "" {
			continue
		}
		pages = append(pages, cdpTarget{ID: t.ID, URL: t.URL, WS: t.WS})
	}
	if len(pages) == 0 {
		return cdpTarget{}, errors.New("cdp: no page target")
	}
	if pinnedTarget != "" {
		for _, p := range pages {
			if p.ID == pinnedTarget {
				return p, nil
			}
		}
	}
	t := pages[0]
	pinnedTarget = t.ID
	return t, nil
}

// cdpPageOneShot opens a short-lived WebSocket to the page target, issues a
// single command and returns its result. Fine for controls (input, navigate);
// the capture loop owns its own long-lived session.
func cdpPageOneShot(method string, params map[string]interface{}) (map[string]interface{}, error) {
	target, err := pickPageTarget()
	if err != nil {
		return nil, err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(target.WS, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp: ws %s: %w", method, err)
	}
	defer conn.Close()

	// CDP ids are JavaScript numbers: keep them small (V8 loses precision
	// past 2^53, so a time-nanosecond id would never match the echo).
	id := oneShotSeq.Add(1)
	cmd := map[string]interface{}{"id": id, "method": method}
	if params != nil {
		cmd["params"] = params
	}
	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteJSON(cmd); err != nil {
		return nil, fmt.Errorf("cdp: write %s: %w", method, err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, fmt.Errorf("cdp: read %s: %w", method, err)
		}
		var env struct {
			ID     int64                  `json:"id"`
			Result map[string]interface{} `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(msg, &env) != nil {
			continue
		}
		if env.ID != id {
			continue // event, not our response
		}
		if env.Error != nil {
			return nil, fmt.Errorf("cdp: %s: %s", method, env.Error.Message)
		}
		return env.Result, nil
	}
}

// ─── viewport state & capture broadcast ────────────────────────────────────

const (
	defaultViewW = 1280
	defaultViewH = 720
	minViewW     = 480
	minViewH     = 320
	maxViewW     = 1920
	maxViewH     = 1080
)

var (
	viewCfgMu    sync.Mutex
	viewCfgW     int = defaultViewW
	viewCfgH     int = defaultViewH
	viewCfgDirty     = false

	captureMu     sync.Mutex
	captureActive bool
	captureSubs   = map[chan cdpFrame]struct{}{}
)

// cdpFrame is one JPEG capture plus the viewport it was captured at.
type cdpFrame struct {
	jpeg []byte
	w, h int
}

func clampView(w, h int) (int, int) {
	if w < minViewW {
		w = minViewW
	}
	if w > maxViewW {
		w = maxViewW
	}
	if h < minViewH {
		h = minViewH
	}
	if h > maxViewH {
		h = maxViewH
	}
	return w, h
}

// setViewport records the requested viewport size. The capture loop applies
// it (Emulation lives on the capture session, so resizing must happen there).
func setViewport(w, h int) {
	w, h = clampView(w, h)
	viewCfgMu.Lock()
	viewCfgW, viewCfgH = w, h
	viewCfgDirty = true
	viewCfgMu.Unlock()
}

func viewportSize() (int, int) {
	viewCfgMu.Lock()
	defer viewCfgMu.Unlock()
	return viewCfgW, viewCfgH
}

func takeViewportDirty() bool {
	viewCfgMu.Lock()
	defer viewCfgMu.Unlock()
	if viewCfgDirty {
		viewCfgDirty = false
		return true
	}
	return false
}

func viewFPS() int {
	if v := os.Getenv("BROWSER_VIEW_FPS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 30 {
			return n
		}
	}
	return 8
}

func viewJPEGQuality() int {
	if v := os.Getenv("BROWSER_VIEW_Q"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 20 && n <= 95 {
			return n
		}
	}
	return 60
}

func viewCaptureDelay() time.Duration {
	return time.Second / time.Duration(viewFPS())
}

// ensureCapture starts the (single) capture loop when nobody is capturing yet.
func ensureCapture() error {
	captureMu.Lock()
	if captureActive {
		captureMu.Unlock()
		return nil
	}
	target, err := pickPageTarget()
	if err != nil {
		captureMu.Unlock()
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(target.WS, nil)
	if err != nil {
		captureMu.Unlock()
		return fmt.Errorf("cdp: capture ws: %w", err)
	}
	captureActive = true
	captureMu.Unlock()

	go captureLoop(conn, target.ID)
	return nil
}

// captureLoop owns one CDP session: it applies the emulated viewport and takes
// a screenshot at the configured rate, fanning frames out to subscribers. It
// exits when it has had no subscriber for a short grace period.
func captureLoop(conn *websocket.Conn, targetID string) {
	defer func() {
		_ = conn.Close()
		captureMu.Lock()
		captureActive = false
		for ch := range captureSubs {
			close(ch)
			delete(captureSubs, ch)
		}
		captureMu.Unlock()
		log.Printf("vatayana: cdp capture loop stopped (target %s)", targetID)
	}()

	var (
		nextID     int64
		noSubTicks int
		consecErr  int
	)

	// Apply the desired viewport before the first capture.
	applyOverride := func() error {
		w, h := viewportSize()
		nextID++
		id := nextID
		if err := conn.WriteJSON(map[string]interface{}{
			"id": id, "method": "Emulation.setDeviceMetricsOverride",
			"params": map[string]interface{}{
				"width": w, "height": h, "deviceScaleFactor": 1, "mobile": false,
			},
		}); err != nil {
			return err
		}
		_, err := waitResult(conn, id)
		return err
	}

	ticker := time.NewTicker(viewCaptureDelay())
	defer ticker.Stop()

	// Give the very first override a moment to take effect on the renderer.
	_ = applyOverride()

	for range ticker.C {
		captureMu.Lock()
		n := len(captureSubs)
		captureMu.Unlock()
		if n == 0 {
			noSubTicks++
			if noSubTicks >= 3 {
				return
			}
			continue
		}
		noSubTicks = 0

		if takeViewportDirty() {
			if err := applyOverride(); err != nil {
				consecErr++
				if consecErr >= 3 {
					log.Printf("vatayana: cdp capture viewport override failed: %v", err)
					return
				}
				continue
			}
		}

		w, h := viewportSize()
		nextID++
		id := nextID
		if err := conn.WriteJSON(map[string]interface{}{
			"id": id, "method": "Page.captureScreenshot",
			"params": map[string]interface{}{
				"format": "jpeg", "quality": viewJPEGQuality(),
			},
		}); err != nil {
			consecErr++
			if consecErr >= 3 {
				return
			}
			continue
		}
		res, err := waitResult(conn, id)
		if err != nil {
			consecErr++
			if consecErr >= 3 {
				log.Printf("vatayana: cdp capture failed: %v", err)
				return
			}
			continue
		}
		consecErr = 0

		data, _ := res["data"].(string)
		if data == "" {
			continue
		}
		jpeg, err := base64.StdEncoding.DecodeString(data)
		if err != nil || len(jpeg) < 100 {
			continue
		}
		frame := cdpFrame{jpeg: jpeg, w: w, h: h}

		captureMu.Lock()
		for ch := range captureSubs {
			select {
			case ch <- frame:
			default:
				// Slow subscriber: drop the frame rather than stall capture.
			}
		}
		captureMu.Unlock()
	}
}

// waitResult reads CDP messages until the response matching id arrives,
// discarding events in between.
func waitResult(conn *websocket.Conn, id int64) (map[string]interface{}, error) {
	_ = conn.SetReadDeadline(time.Now().Add(20 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		var env struct {
			ID     int64                  `json:"id"`
			Result map[string]interface{} `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(msg, &env) != nil {
			continue
		}
		if env.ID != id {
			continue
		}
		if env.Error != nil {
			return nil, errors.New(env.Error.Message)
		}
		return env.Result, nil
	}
}

// ─── page commands (controls: navigate, window, screenshot) ────────────────

func cdpNavigate(url string) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") && !strings.HasPrefix(url, "about:") {
		return errors.New("url must start with http://, https:// or about:")
	}
	_, err := cdpPageOneShot("Page.navigate", map[string]interface{}{"url": url})
	return err
}

// cdpEvalJS returns the value of a JS expression on the page.
func cdpEvalJS(expression string) (string, error) {
	res, err := cdpPageOneShot("Runtime.evaluate", map[string]interface{}{
		"expression":    expression,
		"returnByValue": true,
		"awaitPromise":  true,
	})
	if err != nil {
		return "", err
	}
	if v, ok := res["result"].(map[string]interface{}); ok {
		if val, ok := v["value"]; ok {
			if s, ok := val.(string); ok {
				return s, nil
			}
			b, _ := json.Marshal(val)
			return string(b), nil
		}
	}
	return "", nil
}

func cdpWindowInfo() (map[string]interface{}, error) {
	title, err := cdpEvalJS("document.title")
	if err != nil {
		return nil, err
	}
	url, _ := cdpEvalJS("location.href")
	w, h := viewportSize()
	return map[string]interface{}{
		"title":  title,
		"url":    url,
		"width":  w,
		"height": h,
		"x":      0,
		"y":      0,
	}, nil
}

func cdpSingleScreenshot() ([]byte, error) {
	res, err := cdpPageOneShot("Page.captureScreenshot", map[string]interface{}{
		"format": "jpeg", "quality": viewJPEGQuality(),
	})
	if err != nil {
		return nil, err
	}
	data, _ := res["data"].(string)
	if data == "" {
		return nil, errors.New("cdp: empty screenshot")
	}
	return base64.StdEncoding.DecodeString(data)
}

// ─── input translation ─────────────────────────────────────────────────────
// BrowserView sends xdotool-style events; here they become CDP Input commands
// in the SAME coordinate space as the captured JPEGs (the emulated viewport),
// so what you click is exactly the pixel you saw — no crop offset to apply.

func fnum(m map[string]interface{}, key string, def float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return def
}

// cdpButton maps the 1-based button index used by the frontend to the CDP
// button name and its bitmask for the "buttons" field (1=left, 2=right,
// 4=middle in CDP bitfield order).
func cdpButton(button float64) (string, int) {
	switch int(button) {
	case 2:
		return "middle", 4
	case 3:
		return "right", 2
	default:
		return "left", 1
	}
}

var pressedMu sync.Mutex
var pressedBits int

// lastMouse tracks the last viewport position the pointer was dispatched to,
// so wheel events scroll the element actually under the cursor.
var lastMouseMu sync.Mutex
var lastMouseX, lastMouseY int

func setButtonState(bit int, down bool) {
	pressedMu.Lock()
	if down {
		pressedBits |= bit
	} else {
		pressedBits &^= bit
	}
	pressedMu.Unlock()
}

func currentButtonBits() int {
	pressedMu.Lock()
	defer pressedMu.Unlock()
	return pressedBits
}

func rememberMouse(x, y int) {
	lastMouseMu.Lock()
	lastMouseX, lastMouseY = x, y
	lastMouseMu.Unlock()
}

func lastMousePos() (int, int) {
	lastMouseMu.Lock()
	defer lastMouseMu.Unlock()
	return lastMouseX, lastMouseY
}

func cdpDispatchMouse(e map[string]interface{}) error {
	etype, _ := e["type"].(string)
	x := int(fnum(e, "x", 0))
	y := int(fnum(e, "y", 0))

	switch etype {
	case "mouse_move":
		rememberMouse(x, y)
		_, err := cdpPageOneShot("Input.dispatchMouseEvent", map[string]interface{}{
			"type": "mouseMoved", "x": x, "y": y,
			"buttons": currentButtonBits(),
		})
		return err

	case "mouse_down":
		rememberMouse(x, y)
		btn, bit := cdpButton(fnum(e, "button", 1))
		setButtonState(bit, true)
		_, err := cdpPageOneShot("Input.dispatchMouseEvent", map[string]interface{}{
			"type": "mousePressed", "x": x, "y": y,
			"button": btn, "buttons": currentButtonBits(), "clickCount": 1,
		})
		return err

	case "mouse_up":
		btn, bit := cdpButton(fnum(e, "button", 1))
		_, err := cdpPageOneShot("Input.dispatchMouseEvent", map[string]interface{}{
			"type": "mouseReleased", "x": x, "y": y,
			"button": btn, "buttons": currentButtonBits() &^ bit, "clickCount": 1,
		})
		setButtonState(bit, false)
		return err

	case "mouse_click":
		rememberMouse(x, y)
		btn, bit := cdpButton(fnum(e, "button", 1))
		setButtonState(bit, true)
		_, err1 := cdpPageOneShot("Input.dispatchMouseEvent", map[string]interface{}{
			"type": "mousePressed", "x": x, "y": y, "button": btn,
			"buttons": currentButtonBits(), "clickCount": 1,
		})
		setButtonState(bit, false)
		_, err2 := cdpPageOneShot("Input.dispatchMouseEvent", map[string]interface{}{
			"type": "mouseReleased", "x": x, "y": y, "button": btn,
			"buttons": currentButtonBits(), "clickCount": 1,
		})
		if err1 != nil {
			return err1
		}
		return err2

	case "scroll":
		wx, wy := lastMousePos()
		dy := fnum(e, "dy", 0)
		dx := fnum(e, "dx", 0)
		_, err := cdpPageOneShot("Input.dispatchMouseEvent", map[string]interface{}{
			"type": "mouseWheel", "x": wx, "y": wy, "deltaX": dx, "deltaY": dy,
		})
		return err

	default:
		return fmt.Errorf("cdp: unknown mouse event %q", etype)
	}
}

// keyVK maps the key names BrowserView sends (xdotool vocabulary) to Windows
// virtual key codes for CDP. Printable single characters fall through to their
// code point.
func keyVK(key string) (int, bool) {
	switch key {
	case "Return", "Enter":
		return 13, true
	case "BackSpace":
		return 8, true
	case "Tab":
		return 9, true
	case "Escape":
		return 27, true
	case "Delete":
		return 46, true
	case "Home":
		return 36, true
	case "End":
		return 35, true
	case "Insert":
		return 45, true
	case "space", " ":
		return 32, true
	case "PageUp", "Page_Up":
		return 33, true
	case "PageDown", "Page_Down":
		return 34, true
	case "Left", "ArrowLeft":
		return 37, true
	case "Up", "ArrowUp":
		return 38, true
	case "Right", "ArrowRight":
		return 39, true
	case "Down", "ArrowDown":
		return 40, true
	}
	r := []rune(key)
	if len(r) == 1 {
		return int(r[0]), true
	}
	return 0, false
}

// cdpDispatchKey sends a keyDown/keyUp pair. Pure modifier presses are ignored
// (the frontend never sends keyup, so chord state cannot be tracked reliably;
// text entry and shortcuts that matter are handled via insertText/Page.*).
func cdpDispatchKey(e map[string]interface{}) error {
	key, _ := e["key"].(string)
	if key == "" {
		return nil
	}
	switch key {
	case "Control_L", "Control_R", "Shift_L", "Shift_R", "Alt_L", "Alt_R", "Super_L", "Super_R", "Meta":
		return nil
	}
	vk, ok := keyVK(key)
	if !ok {
		return nil
	}

	// Printable characters go through keyDown with text so the focused element
	// receives them; control keys use a bare rawKeyDown.
	if len([]rune(key)) == 1 {
		_, err := cdpPageOneShot("Input.dispatchKeyEvent", map[string]interface{}{
			"type": "keyDown", "text": key,
			"windowsVirtualKeyCode": vk, "nativeVirtualKeyCode": vk,
		})
		if err != nil {
			return err
		}
		_, err = cdpPageOneShot("Input.dispatchKeyEvent", map[string]interface{}{
			"type":                  "keyUp",
			"windowsVirtualKeyCode": vk, "nativeVirtualKeyCode": vk,
		})
		return err
	}

	_, err := cdpPageOneShot("Input.dispatchKeyEvent", map[string]interface{}{
		"type":                  "rawKeyDown",
		"windowsVirtualKeyCode": vk, "nativeVirtualKeyCode": vk,
	})
	if err != nil {
		return err
	}
	_, err = cdpPageOneShot("Input.dispatchKeyEvent", map[string]interface{}{
		"type":                  "keyUp",
		"windowsVirtualKeyCode": vk, "nativeVirtualKeyCode": vk,
	})
	return err
}

// cdpHandleInput is the single entry point for one frontend input event.
func cdpHandleInput(e map[string]interface{}) error {
	etype, _ := e["type"].(string)
	switch etype {
	case "mouse_move", "mouse_down", "mouse_up", "mouse_click", "scroll":
		return cdpDispatchMouse(e)
	case "key":
		return cdpDispatchKey(e)
	case "type":
		text, _ := e["text"].(string)
		if text == "" {
			return nil
		}
		_, err := cdpPageOneShot("Input.insertText", map[string]interface{}{"text": text})
		return err
	default:
		return fmt.Errorf("cdp: unknown event type %q", etype)
	}
}
