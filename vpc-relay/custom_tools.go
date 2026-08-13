package main

// Custom tools via sandbox scripts — no recompilation needed.
// Scripts in /files/tools/ are auto-discovered and made available as Kāraka tools.
// A tool script receives JSON params on stdin and writes JSON result to stdout.
//
// Conventions:
//   - *.sh runs with bash, *.py runs with python3
//   - a "# desc: ..." comment near the top becomes the tool description
//   - input is passed on stdin as JSON (base64-wrapped on the wire, so any
//     quoting survives)
//
// Discovery is refreshable: rescanCustomTools() is called at the start of
// every orchestration run, so a tool an agent just wrote mid-mission (via
// sandbox.file_write) is immediately plannable. This is the GAFAM-native
// "agents write their own tools" loop — the safe level of self-improvement:
// capabilities grow, the Go binary stays untouched.

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

func sandboxURL() string {
	if u := os.Getenv("SANDBOX_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-sandbox:6091"
}

var customToolsMu sync.Mutex // one rescan at a time

// registerSandboxTools is the boot-time scan (sandbox may be asleep — fine,
// the per-run rescan will pick tools up later).
func registerSandboxTools() {
	rescanCustomTools()
}

// rescanCustomTools scans /files/tools/ in the sandbox and (re)registers
// each script as a Kāraka tool. Idempotent: RegisterTool overwrites.
func rescanCustomTools() {
	customToolsMu.Lock()
	defer customToolsMu.Unlock()

	listURL := sandboxURL() + "/files?path=/files/tools"
	resp, err := http.Get(listURL)
	if err != nil {
		log.Printf("sandbox-tools: could not list tools dir (sandbox may be stopped): %v", err)
		return
	}
	defer resp.Body.Close()

	var result struct {
		Entries []struct {
			Name string `json:"name"`
			Type string `json:"type"`
		} `json:"entries"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return
	}

	count := 0
	for _, entry := range result.Entries {
		if entry.Type != "file" {
			continue
		}
		var toolID, runner string
		switch {
		case strings.HasSuffix(entry.Name, ".sh"):
			toolID = "custom." + strings.TrimSuffix(entry.Name, ".sh")
			runner = "bash"
		case strings.HasSuffix(entry.Name, ".py"):
			toolID = "custom." + strings.TrimSuffix(entry.Name, ".py")
			runner = "python3"
		default:
			continue
		}
		// Guard against weird filenames breaking the shell command line.
		if strings.ContainsAny(entry.Name, "\"' `$\\;|&") {
			log.Printf("sandbox-tools: skipping unsafe filename %q", entry.Name)
			continue
		}
		name := entry.Name // capture for closure
		desc := fetchToolDescription(name)
		if desc == "" {
			desc = fmt.Sprintf("Custom tool from sandbox /files/tools/%s (JSON in on stdin, JSON out on stdout)", name)
		}

		karaka.RegisterTool(karaka.Tool{
			ID:          toolID,
			Description: desc,
			Category:    "custom",
			Params: map[string]karaka.ParamSpec{
				"input": {Type: "string", Required: false, Description: "JSON input for the script"},
			},
			Returns: "{ stdout: string, stderr: string, exit_code: int }",
			Handler: func(params map[string]interface{}) (interface{}, error) {
				input, _ := params["input"].(string)
				if input == "" {
					input = "{}"
				}
				b64 := base64.StdEncoding.EncodeToString([]byte(input))
				// echo <b64> | base64 -d | runner script — immune to quoting.
				cmd := fmt.Sprintf("echo %s | base64 -d | %s /sandbox/files/tools/%s", b64, runner, name)
				execURL := sandboxURL() + "/exec"
				body, _ := json.Marshal(map[string]interface{}{
					"command": cmd,
					"timeout": 120,
				})
				resp, err := http.Post(execURL, "application/json", bytes.NewReader(body))
				if err != nil {
					return nil, fmt.Errorf("custom tool %s: %w", toolID, err)
				}
				defer resp.Body.Close()
				var execResult map[string]interface{}
				json.NewDecoder(resp.Body).Decode(&execResult)
				return map[string]interface{}{
					"stdout":    execResult["stdout"],
					"stderr":    execResult["stderr"],
					"exit_code": execResult["exit_code"],
				}, nil
			},
		})
		count++
	}

	if count > 0 {
		log.Printf("sandbox-tools: registered %d custom tools from /files/tools/", count)
	}
}

// fetchToolDescription reads the first lines of a script looking for
// "# desc: ..." — the human/agent-written doc of the tool.
func fetchToolDescription(name string) string {
	resp, err := http.Get(sandboxURL() + "/files/tools/" + name)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	for _, line := range strings.Split(string(buf[:n]), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# desc:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# desc:"))
		}
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "//") {
			break // past the header comments
		}
	}
	return ""
}
