package browser

// Integration test for the CDP view plane against a real Chrome DevTools
// endpoint. Skipped unless BROWSER_CDP_URL points at a running Chrome, e.g.:
//
//	"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
//	  --headless=new --remote-debugging-port=9333 \
//	  --user-data-dir=$(mktemp -d) --no-first-run about:blank &
//	BROWSER_CDP_URL=http://127.0.0.1:9333 go test ./browser/ -run TestCDPView -v

import (
	"os"
	"testing"
	"time"
)

func TestCDPViewIntegration(t *testing.T) {
	if os.Getenv("BROWSER_CDP_URL") == "" {
		t.Skip("set BROWSER_CDP_URL to a running Chrome DevTools endpoint")
	}
	if !cdpReachable() {
		t.Fatal("BROWSER_CDP_URL not reachable")
	}

	targetMu.Lock()
	pinnedTarget = ""
	targetMu.Unlock()

	target, err := pickPageTarget()
	if err != nil {
		t.Fatalf("pickPageTarget: %v", err)
	}
	t.Logf("page target: %s (%s)", target.ID, target.URL)

	// 1. Capture: subscribe, start the loop, and read a real JPEG frame at the
	// emulated viewport size.
	setViewport(1000, 600)
	ch := make(chan cdpFrame, 4)
	if err := ensureCapture(); err != nil {
		t.Fatalf("ensureCapture: %v", err)
	}
	captureMu.Lock()
	captureSubs[ch] = struct{}{}
	captureMu.Unlock()
	defer func() {
		captureMu.Lock()
		delete(captureSubs, ch)
		captureMu.Unlock()
	}()

	var frame cdpFrame
	select {
	case frame = <-ch:
	case <-time.After(20 * time.Second):
		t.Fatal("timed out waiting for the first capture frame")
	}
	if frame.w != 1000 || frame.h != 600 {
		t.Errorf("frame size = %dx%d, want 1000x600", frame.w, frame.h)
	}
	if len(frame.jpeg) < 100 || frame.jpeg[0] != 0xFF || frame.jpeg[1] != 0xD8 {
		t.Errorf("first frame is not a JPEG (len=%d, head=%x)", len(frame.jpeg), frame.jpeg[:2])
	}
	t.Logf("frame ok: %dx%d, %d bytes", frame.w, frame.h, len(frame.jpeg))

	// A second frame should keep flowing at the capture rate.
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Error("no second frame after 3s — capture loop stalled")
	}

	// 2. Resize viewport mid-stream: next frames must match the new size.
	setViewport(640, 480)
	deadline := time.Now().Add(20 * time.Second)
	for {
		select {
		case f := <-ch:
			if f.w == 640 && f.h == 480 {
				t.Logf("resize applied: %dx%d", f.w, f.h)
				goto resized
			}
		case <-time.After(deadline.Sub(time.Now())):
			t.Fatal("viewport resize never produced a 640x480 frame")
		}
	}
resized:

	// 3. Input events must not error against the real page target.
	inputs := []map[string]interface{}{
		{"type": "mouse_move", "x": 10, "y": 10},
		{"type": "mouse_click", "x": 10, "y": 10, "button": 1},
		{"type": "scroll", "dy": -120},
	}
	for _, ev := range inputs {
		if err := cdpHandleInput(ev); err != nil {
			t.Errorf("input %v: %v", ev["type"], err)
		}
	}

	// 4. Window info + navigation round-trip.
	info, err := cdpWindowInfo()
	if err != nil {
		t.Fatalf("cdpWindowInfo: %v", err)
	}
	t.Logf("window info: %v", info)
	if err := cdpNavigate("about:blank"); err != nil {
		t.Fatalf("cdpNavigate: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	href, err := cdpEvalJS("location.href")
	if err != nil {
		t.Fatalf("cdpEvalJS: %v", err)
	}
	t.Logf("location after navigate: %q", href)
}
