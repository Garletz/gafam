package main

// MCP browser client — connects the Go orchestrator to the Playwright MCP
// sidecar (gafam-mcp), which drives the SHARED Chrome instance over CDP.
// Every browser_* tool exposed by the sidecar is mirrored into the karaka
// registry as browser.mcp_* so Saṃyojaka can use the same session as the
// human (same profile, same cookies, no automation flags).

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Garletz/gafam/vpc-relay/browser"
	"github.com/Garletz/gafam/vpc-relay/karaka"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	mcpMu      sync.Mutex
	mcpSession *mcp.ClientSession
	mcpTools   map[string]*mcp.Tool
)

// initBrowserMCP starts the MCP sidecar, connects to it, and mirrors its
// browser tools into the karaka registry. Runs once at startup, retrying in
// the background until the sidecar is reachable.
func initBrowserMCP() {
	go func() {
		if err := browser.EnsureMcpContainer(context.Background()); err != nil {
			log.Println("mcp: sidecar ensure:", err)
		}
		for {
			if err := connectBrowserMCP(); err != nil {
				log.Println("mcp: connect failed (retrying in 15s):", err)
				time.Sleep(15 * time.Second)
				continue
			}
			registerMCPTools()
			log.Println("mcp: browser tools registered")
			return
		}
	}()
}

func connectBrowserMCP() error {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	if mcpSession != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	client := mcp.NewClient(&mcp.Implementation{Name: "gafam-vpc", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{
		Endpoint:   browser.MCPURL(),
		MaxRetries: 5,
	}, nil)
	if err != nil {
		return err
	}
	// Keep the standalone SSE stream alive: the sidecar heartbeats every 3s
	// and closes sessions that stop answering pings.
	mcpSession = session
	return nil
}

func mcpListTools(ctx context.Context) (map[string]*mcp.Tool, error) {
	mcpMu.Lock()
	defer mcpMu.Unlock()
	if mcpSession == nil {
		return nil, fmt.Errorf("mcp: not connected")
	}
	res, err := mcpSession.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		return nil, err
	}
	out := map[string]*mcp.Tool{}
	for _, t := range res.Tools {
		out[t.Name] = t
	}
	return out, nil
}

// mcpCallTool invokes a sidecar tool, reconnecting once on session loss.
func mcpCallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
	browser.WaitHumanIdle(ctx)

	res, err := mcpCallOnce(ctx, name, args)
	if err != nil && strings.Contains(strings.ToLower(err.Error()), "session") {
		// Session lost (404 after heartbeat failure / restart): reconnect and retry once.
		if reErr := connectBrowserMCP(); reErr == nil {
			res, err = mcpCallOnce(ctx, name, args)
		}
	}
	if err != nil {
		return "", err
	}
	var sb strings.Builder
	for _, c := range res.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			sb.WriteString(v.Text)
		case *mcp.ImageContent:
			fmt.Fprintf(&sb, "[image %s: %d bytes]", v.MIMEType, len(v.Data))
		default:
			fmt.Fprintf(&sb, "[content %T]", c)
		}
	}
	return sb.String(), nil
}

func mcpCallOnce(ctx context.Context, name string, args map[string]interface{}) (*mcp.CallToolResult, error) {
	mcpMu.Lock()
	session := mcpSession
	mcpMu.Unlock()
	if session == nil {
		return nil, fmt.Errorf("mcp: not connected")
	}
	return session.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
}

// registerMCPTools mirrors every browser_* sidecar tool as a karaka tool
// (browser.mcp_<name>), with params derived from its JSON schema.
func registerMCPTools() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	tools, err := mcpListTools(ctx)
	if err != nil {
		log.Println("mcp: list tools:", err)
		return
	}
	mcpMu.Lock()
	mcpTools = tools
	mcpMu.Unlock()

	names := make([]string, 0, len(tools))
	for name := range tools {
		if strings.HasPrefix(name, "browser_") {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	for _, name := range names {
		t := tools[name]
		id := "browser.mcp_" + strings.TrimPrefix(name, "browser_")
		params := map[string]karaka.ParamSpec{}
		if schema, ok := t.InputSchema.(map[string]interface{}); ok {
			required := map[string]bool{}
			if req, ok := schema["required"].([]interface{}); ok {
				for _, r := range req {
					if rs, ok := r.(string); ok {
						required[rs] = true
					}
				}
			}
			if props, ok := schema["properties"].(map[string]interface{}); ok {
				for pname, raw := range props {
					if pm, ok := raw.(map[string]interface{}); ok {
						spec := karaka.ParamSpec{Required: required[pname]}
						if pt, ok := pm["type"].(string); ok {
							spec.Type = pt
						}
						if pd, ok := pm["description"].(string); ok {
							spec.Description = pd
						}
						params[pname] = spec
					}
				}
			}
		}
		desc := t.Description
		if desc == "" {
			desc = "Drive the shared Chrome browser (same session as the human)."
		}
		toolName := name
		karaka.RegisterTool(karaka.Tool{
			ID:          id,
			Category:    "browser",
			Description: desc,
			Params:      params,
			Returns:     "text (snapshot, result, or status)",
			Handler: func(p map[string]interface{}) (interface{}, error) {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
				defer cancel()
				return mcpCallTool(ctx, toolName, p)
			},
		})
	}

	karaka.SetPermissions("suparna_vpc", map[string]string{"browser.mcp_*": "allow"})
	karaka.SetPermissions("edge_l2_phone", map[string]string{"browser.mcp_*": "allow"})
	log.Printf("mcp: mirrored %d browser tools into karaka", len(names))
}
