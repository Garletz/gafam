package main

// Custom tools via sandbox scripts — no recompilation needed.
// Scripts in /files/tools/ are auto-discovered and made available as Kāraka tools.
// A tool script receives JSON params on stdin and writes JSON result to stdout.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/Garletz/gafam/vpc-relay/karaka"
)

func sandboxURL() string {
	if u := os.Getenv("SANDBOX_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "http://gafam-sandbox:6091"
}

// registerSandboxTools scans /files/tools/ in the sandbox for executable scripts
// and registers each as a Kāraka tool. Tools registered here can be used immediately
// without restarting the relay.
func registerSandboxTools() {
	// Discover tools by listing /files/tools/ directory
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
		if !strings.HasSuffix(entry.Name, ".sh") {
			continue
		}
		toolID := "custom." + strings.TrimSuffix(entry.Name, ".sh")
		scriptPath := "/files/tools/" + entry.Name

		karaka.RegisterTool(karaka.Tool{
			ID:          toolID,
			Description: fmt.Sprintf("Custom tool: %s (from sandbox /files/tools/%s)", toolID, entry.Name),
			Category:    "custom",
			Params: map[string]karaka.ParamSpec{
				"input": {Type: "string", Required: false, Description: "JSON input for the script"},
			},
			Returns: "{ result: any }",
			Handler: func(params map[string]interface{}) (interface{}, error) {
				input, _ := params["input"].(string)
				if input == "" {
					input = "{}"
				}
				// Execute the script via sandbox shell
				execURL := sandboxURL() + "/exec"
				body, _ := json.Marshal(map[string]string{
					"command": fmt.Sprintf("bash /sandbox/files/tools/%s <<'EOF'\n%s\nEOF", entry.Name, input),
				})
				resp, err := http.Post(execURL, "application/json", bytes.NewReader(body))
				if err != nil {
					return nil, fmt.Errorf("custom tool %s: %w", toolID, err)
				}
				defer resp.Body.Close()
				var execResult map[string]interface{}
				json.NewDecoder(resp.Body).Decode(&execResult)
				return map[string]interface{}{
					"stdout": execResult["stdout"],
					"stderr": execResult["stderr"],
					"exit_code": execResult["exit_code"],
				}, nil
			},
		})
		count++
		_ = scriptPath
	}

	if count > 0 {
		log.Printf("sandbox-tools: registered %d custom tools from /files/tools/", count)
	}
}
