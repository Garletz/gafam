package karaka

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"
)

var internalClient = &http.Client{Timeout: 30 * time.Second}

func postJSON(url string, body interface{}) (map[string]interface{}, error) {
	payload, _ := json.Marshal(body)
	resp, err := internalClient.Post(url, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{"raw": string(raw)}, nil
	}
	return out, nil
}

func getJSON(url string) (map[string]interface{}, error) {
	resp, err := internalClient.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return map[string]interface{}{"raw": string(raw)}, nil
	}
	return out, nil
}

// ─── Browser tools (Vātāyana) ───

func browserStatusHandler(params map[string]interface{}) (interface{}, error) {
	return getJSON(browserURL() + "/status")
}

func browserScreenshotHandler(params map[string]interface{}) (interface{}, error) {
	return map[string]interface{}{
		"url":    browserURL() + "/screenshot",
		"method": "GET",
	}, nil
}

func browserInputHandler(params map[string]interface{}) (interface{}, error) {
	return postJSON(browserURL()+"/input", params)
}

// browserFetchHandler — Khadyota: read any page as markdown-ish text + links.
func browserFetchHandler(params map[string]interface{}) (interface{}, error) {
	u, _ := params["url"].(string)
	if u == "" {
		return nil, fmt.Errorf("missing 'url'")
	}
	return getJSON(browserURL() + "/fetch?url=" + url.QueryEscape(u))
}

// browserNavigateHandler — drive the visible Firefox to a URL.
func browserNavigateHandler(params map[string]interface{}) (interface{}, error) {
	u, _ := params["url"].(string)
	if u == "" {
		return nil, fmt.Errorf("missing 'url'")
	}
	return postJSON(browserURL()+"/navigate", map[string]interface{}{"url": u})
}

// browserWindowHandler — current window title: "what am I looking at".
func browserWindowHandler(params map[string]interface{}) (interface{}, error) {
	return getJSON(browserURL() + "/window")
}

func browserURL() string {
	if u := os.Getenv("BROWSER_URL"); u != "" {
		return u
	}
	return "http://gafam-browser:6080"
}

// ─── Sandbox tools (Yantraśālā) ───

func sandboxExecHandler(params map[string]interface{}) (interface{}, error) {
	return postJSON(sandboxURL()+"/exec", params)
}

// sandboxShellHandler — persistent shell: cwd/env survive between calls.
// The same session_id can be shared with the human web terminal.
func sandboxShellHandler(params map[string]interface{}) (interface{}, error) {
	return postJSON(sandboxURL()+"/shell/exec", params)
}

// sandboxTreeHandler — filesystem tree (JSON or ASCII "vision" of the fs).
func sandboxTreeHandler(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	if path == "" {
		path = "/"
	}
	depth := "4"
	if d, ok := params["depth"]; ok {
		depth = fmt.Sprintf("%v", d)
	}
	format, _ := params["format"].(string)
	if format == "" {
		format = "json"
	}
	q := url.Values{}
	q.Set("path", path)
	q.Set("depth", depth)
	q.Set("format", format)
	return getJSON(sandboxURL() + "/tree?" + q.Encode())
}

func sandboxFileListHandler(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	if path == "" {
		path = "/files"
	}
	return getJSON(sandboxURL() + path)
}

func sandboxFileReadHandler(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	if path == "" {
		return nil, fmt.Errorf("missing 'path'")
	}
	resp, err := internalClient.Get(sandboxURL() + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"path":    path,
		"content": string(raw),
		"size":    len(raw),
	}, nil
}

func sandboxFileWriteHandler(params map[string]interface{}) (interface{}, error) {
	path, _ := params["path"].(string)
	content, _ := params["content"].(string)
	if path == "" {
		return nil, fmt.Errorf("missing 'path'")
	}
	req, _ := http.NewRequest("PUT", sandboxURL()+path, bytes.NewReader([]byte(content)))
	req.Header.Set("Content-Type", "text/plain")
	resp, err := internalClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return map[string]interface{}{
		"path":    path,
		"written": len(content),
		"raw":     string(raw),
	}, nil
}

func sandboxStorageHandler(params map[string]interface{}) (interface{}, error) {
	return getJSON(sandboxURL() + "/storage")
}

func sandboxURL() string {
	if u := os.Getenv("SANDBOX_URL"); u != "" {
		return u
	}
	return "http://gafam-sandbox:6091"
}

// ─── Registration ───

// RegisterAllTools enregistre tous les outils GAFAM dans le registre.
// À appeler depuis main.go après initDB().
func RegisterAllTools() {
	RegisterTool(Tool{
		ID:          "browser.status",
		Description: "Check if the remote browser (Vātāyana) is running",
		Category:    "browser",
		Params:      map[string]ParamSpec{},
		Returns:     "{ running: bool }",
		Handler:     browserStatusHandler,
	})
	RegisterTool(Tool{
		ID:          "browser.screenshot",
		Description: "Get a screenshot URL from the remote browser",
		Category:    "browser",
		Params:      map[string]ParamSpec{},
		Returns:     "{ url: string }",
		Handler:     browserScreenshotHandler,
	})
	RegisterTool(Tool{
		ID:          "browser.input",
		Description: "Send input (mouse, keyboard) to the remote browser",
		Category:    "browser",
		Params: map[string]ParamSpec{
			"type": {Type: "string", Required: true, Description: "mouse_move, mouse_down, mouse_up, key, type, scroll"},
			"x":    {Type: "int", Required: false, Description: "X coordinate for mouse events"},
			"y":    {Type: "int", Required: false, Description: "Y coordinate for mouse events"},
			"key":  {Type: "string", Required: false, Description: "Key name (Return, Tab, Escape...)"},
			"text": {Type: "string", Required: false, Description: "Text to type"},
		},
		Returns: "{ ok: bool }",
		Handler: browserInputHandler,
	})
	RegisterTool(Tool{
		ID:          "browser.fetch",
		Description: "Fetch a web page and read it as markdown-ish text with links (Khadyota — no GUI parsing needed)",
		Category:    "browser",
		Params: map[string]ParamSpec{
			"url": {Type: "string", Required: true, Description: "Page URL (http:// or https://)"},
		},
		Returns: "{ title: string, text: string, links: [{text, href}], final_url: string, status: int }",
		Handler: browserFetchHandler,
	})
	RegisterTool(Tool{
		ID:          "browser.navigate",
		Description: "Navigate the visible Firefox window to a URL",
		Category:    "browser",
		Params: map[string]ParamSpec{
			"url": {Type: "string", Required: true, Description: "Destination URL"},
		},
		Returns: "{ ok: bool }",
		Handler: browserNavigateHandler,
	})
	RegisterTool(Tool{
		ID:          "browser.window",
		Description: "Get the current browser window title — what page is being shown",
		Category:    "browser",
		Params:      map[string]ParamSpec{},
		Returns:     "{ title: string, width: int, height: int }",
		Handler:     browserWindowHandler,
	})

	RegisterTool(Tool{
		ID:          "sandbox.exec",
		Description: "Execute a shell command in the sandbox (Alpine Linux)",
		Category:    "sandbox",
		Params: map[string]ParamSpec{
			"command": {Type: "string", Required: true, Description: "Shell command to execute"},
			"timeout": {Type: "int", Required: false, Description: "Timeout in seconds", Default: 30},
		},
		Returns: "{ stdout: string, stderr: string, exit_code: int }",
		Handler: sandboxExecHandler,
	})
	RegisterTool(Tool{
		ID:          "sandbox.shell",
		Description: "Run a command in a persistent shell session — cwd and env survive between calls. Use the same session_id as the human (e.g. 'main') to share context, or your own.",
		Category:    "sandbox",
		Params: map[string]ParamSpec{
			"command":    {Type: "string", Required: true, Description: "Shell command (curl, jq, git, python3...)"},
			"session_id": {Type: "string", Required: false, Description: "Shell session id", Default: "main"},
			"timeout":    {Type: "int", Required: false, Description: "Timeout in seconds (max 600)", Default: 60},
		},
		Returns: "{ output: string, exit_code: int, cwd: string, session_id: string }",
		Handler: sandboxShellHandler,
	})
	RegisterTool(Tool{
		ID:          "sandbox.tree",
		Description: "Get the sandbox filesystem as a tree. format=ascii returns a single text block (├── └──) — the fastest way to 'see' the whole filesystem at once.",
		Category:    "sandbox",
		Params: map[string]ParamSpec{
			"path":   {Type: "string", Required: false, Description: "Root path (/ , /files, /downloads...)", Default: "/"},
			"depth":  {Type: "int", Required: false, Description: "Recursion depth (0-8)", Default: 4},
			"format": {Type: "string", Required: false, Description: "json or ascii", Default: "json"},
		},
		Returns: "{ root: {name, path, type, size, children[]}, truncated: bool } or { ascii: string }",
		Handler: sandboxTreeHandler,
	})
	RegisterTool(Tool{
		ID:          "sandbox.file_list",
		Description: "List files in a sandbox directory",
		Category:    "sandbox",
		Params: map[string]ParamSpec{
			"path": {Type: "string", Required: false, Description: "Directory path", Default: "/files"},
		},
		Returns: "{ entries: [{name, type, size}] }",
		Handler: sandboxFileListHandler,
	})
	RegisterTool(Tool{
		ID:          "sandbox.file_read",
		Description: "Read the content of a file in the sandbox",
		Category:    "sandbox",
		Params: map[string]ParamSpec{
			"path": {Type: "string", Required: true, Description: "File path (e.g. /files/notes.md)"},
		},
		Returns: "{ path: string, content: string, size: int }",
		Handler: sandboxFileReadHandler,
	})
	RegisterTool(Tool{
		ID:          "sandbox.file_write",
		Description: "Write content to a file in the sandbox",
		Category:    "sandbox",
		Params: map[string]ParamSpec{
			"path":    {Type: "string", Required: true, Description: "File path"},
			"content": {Type: "string", Required: true, Description: "File content"},
		},
		Returns: "{ path: string, written: int }",
		Handler: sandboxFileWriteHandler,
	})
	RegisterTool(Tool{
		ID:          "sandbox.storage",
		Description: "Get storage usage for each sandbox directory",
		Category:    "sandbox",
		Params:      map[string]ParamSpec{},
		Returns:     "{ files: int, tmp: int, downloads: int } (bytes)",
		Handler:     sandboxStorageHandler,
	})
}

// RegisterDefaultKarakas enregistre les kāraka GAFAM connus.
func RegisterDefaultKarakas() {
	RegisterKaraka(Karaka{
		ID:           "suparna_vpc",
		Name:         "Suparna",
		Tier:         "L1",
		Status:       "idle",
		Capabilities: []string{"read_logs", "analyze_day", "suggest_action"},
		Tools: map[string]string{
			"sandbox.exec":       "ask",
			"sandbox.shell":      "ask",
			"sandbox.tree":       "allow",
			"sandbox.file_read":  "allow",
			"sandbox.file_list":  "allow",
			"sandbox.file_write": "ask",
			"sandbox.storage":    "allow",
			"browser.status":     "allow",
			"browser.screenshot": "allow",
			"browser.fetch":      "allow",
			"browser.window":     "allow",
			"browser.navigate":   "ask",
			"browser.input":      "deny",
			"llm.chat":           "ask",
		},
		MaxSteps: 10,
	})
	RegisterKaraka(Karaka{
		ID:           "edge_l2_phone",
		Name:         "Edge L2",
		Tier:         "L2",
		Status:       "sleeping",
		Capabilities: []string{"deep_analyze", "long_context", "multi_turn"},
		Tools: map[string]string{
			"sandbox.*": "allow",
			"browser.*": "allow",
			"llm.chat":  "allow",
		},
		MaxSteps: 15,
	})
}
