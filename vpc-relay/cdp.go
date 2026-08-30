package main

// Browser CDP (Chrome DevTools Protocol) control — vpc-relay/cdp.go
// Talks to Chromium sidecar port 9222 via WebSocket.
// Enables real browser automation: click, type, screenshot, JavaScript.

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Garletz/gafam/vpc-relay/karaka"
	"github.com/gorilla/websocket"
)

var cdpReqID atomic.Int64

func chromiumCDPURL() string {
	if u := os.Getenv("CHROMIUM_CDP_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	// Chrome 136+ binds DevTools to loopback and only accepts IP-shaped Host
	// headers — go through the container's socat bridge on 9223 (see
	// entrypoint.sh) and the browser's static IP on gafam-net.
	return "http://172.18.0.10:9223"
}

// cdpCommand sends a CDP command and returns the result.
func cdpCommand(method string, params map[string]interface{}) (map[string]interface{}, error) {
	// Get WebSocket URL from /json endpoint
	versionResp, err := http.Get(chromiumCDPURL() + "/json/version")
	if err != nil {
		return nil, fmt.Errorf("cdp: cannot reach chromium: %w", err)
	}
	defer versionResp.Body.Close()
	var versionInfo struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	json.NewDecoder(versionResp.Body).Decode(&versionInfo)
	if versionInfo.WebSocketDebuggerURL == "" {
		return nil, fmt.Errorf("cdp: no WebSocket URL in /json/version")
	}

	// Connect via WebSocket
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.Dial(versionInfo.WebSocketDebuggerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("cdp: WebSocket connect: %w", err)
	}
	defer conn.Close()

	// Send command
	id := int(cdpReqID.Add(1))
	cmd := map[string]interface{}{
		"id":     id,
		"method": method,
	}
	if params != nil {
		cmd["params"] = params
	}
	if err := conn.WriteJSON(cmd); err != nil {
		return nil, fmt.Errorf("cdp: write: %w", err)
	}

	// Read response with timeout
	conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			if err, ok := err.(net.Error); ok && err.Timeout() {
				return nil, fmt.Errorf("cdp: timeout waiting for response to %s", method)
			}
			return nil, fmt.Errorf("cdp: read: %w", err)
		}
		var resp struct {
			ID     int                    `json:"id"`
			Result map[string]interface{} `json:"result"`
			Error  *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if json.Unmarshal(msg, &resp) != nil {
			continue
		}
		if resp.ID == id {
			if resp.Error != nil {
				return nil, fmt.Errorf("cdp: %s", resp.Error.Message)
			}
			return resp.Result, nil
		}
	}
}

// evaluateJS runs JavaScript in the page and returns the result.
func cdpEvaluateJS(expression string) (string, error) {
	result, err := cdpCommand("Runtime.evaluate", map[string]interface{}{
		"expression":            expression,
		"returnByValue":         true,
		"awaitPromise":          true,
		"includeCommandLineAPI": true,
	})
	if err != nil {
		return "", err
	}
	if v, ok := result["result"].(map[string]interface{}); ok {
		if val, ok := v["value"]; ok {
			return fmt.Sprintf("%v", val), nil
		}
	}
	return "", nil
}

// ─── CDP Kāraka tools ───

func registerCDPTools() {
	karaka.RegisterTool(karaka.Tool{
		ID:          "browser.cdp_click",
		Description: "Click an element in the browser by CSS selector (e.g. 'button.login', '#submit', 'a[href=\"/login\"]'). Uses Chrome DevTools Protocol.",
		Category:    "browser",
		Params: map[string]karaka.ParamSpec{
			"selector": {Type: "string", Required: true, Description: "CSS selector of the element to click"},
		},
		Returns: "{ ok: bool, error: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			selector, _ := params["selector"].(string)
			if selector == "" {
				return nil, fmt.Errorf("selector required")
			}
			// Click via JS click()
			js := fmt.Sprintf("(function(){var e=document.querySelector('%s');if(!e)return'not found';e.click();return'clicked'})()", escapeJS(selector))
			result, err := cdpEvaluateJS(js)
			if err != nil {
				return map[string]interface{}{"ok": false, "error": err.Error()}, nil
			}
			return map[string]interface{}{"ok": result == "clicked", "result": result}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "browser.cdp_type",
		Description: "Type text into a browser input element by CSS selector.",
		Category:    "browser",
		Params: map[string]karaka.ParamSpec{
			"selector": {Type: "string", Required: true, Description: "CSS selector of the input element"},
			"text":     {Type: "string", Required: true, Description: "Text to type"},
		},
		Returns: "{ ok: bool }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			selector, _ := params["selector"].(string)
			text, _ := params["text"].(string)
			if selector == "" || text == "" {
				return nil, fmt.Errorf("selector and text required")
			}
			js := fmt.Sprintf("(function(){var e=document.querySelector('%s');if(!e)return'not found';e.focus();e.value='%s';e.dispatchEvent(new Event('input',{bubbles:true}));return'typed'})()",
				escapeJS(selector), escapeJS(text))
			result, err := cdpEvaluateJS(js)
			if err != nil {
				return map[string]interface{}{"ok": false, "error": err.Error()}, nil
			}
			return map[string]interface{}{"ok": result == "typed", "result": result}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "browser.cdp_eval",
		Description: "Execute arbitrary JavaScript in the browser page and return the result. Use for scraping, form filling, or complex interactions.",
		Category:    "browser",
		Params: map[string]karaka.ParamSpec{
			"code": {Type: "string", Required: true, Description: "JavaScript code to execute"},
		},
		Returns: "{ result: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			code, _ := params["code"].(string)
			if code == "" {
				return nil, fmt.Errorf("code required")
			}
			result, err := cdpEvaluateJS(code)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"result": result}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "browser.cdp_text",
		Description: "Get the text content of an element by CSS selector.",
		Category:    "browser",
		Params: map[string]karaka.ParamSpec{
			"selector": {Type: "string", Required: true, Description: "CSS selector"},
		},
		Returns: "{ text: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			selector, _ := params["selector"].(string)
			if selector == "" {
				return nil, fmt.Errorf("selector required")
			}
			js := fmt.Sprintf("(function(){var e=document.querySelector('%s');return e?e.textContent.trim():'not found'})()", escapeJS(selector))
			result, err := cdpEvaluateJS(js)
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"text": result}, nil
		},
	})

	karaka.RegisterTool(karaka.Tool{
		ID:          "browser.cdp_nav",
		Description: "Navigate the browser to a URL using CDP (faster than Firefox navigate).",
		Category:    "browser",
		Params: map[string]karaka.ParamSpec{
			"url": {Type: "string", Required: true, Description: "Destination URL"},
		},
		Returns: "{ ok: bool }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			url, _ := params["url"].(string)
			if url == "" {
				return nil, fmt.Errorf("url required")
			}
			_, err := cdpCommand("Page.navigate", map[string]interface{}{"url": url})
			if err != nil {
				return nil, err
			}
			time.Sleep(2 * time.Second)
			return map[string]interface{}{"ok": true}, nil
		},
	})

	log.Println("CDP browser tools registered (cdp_click, cdp_type, cdp_eval, cdp_text, cdp_nav)")
}

func escapeJS(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	return s
}
