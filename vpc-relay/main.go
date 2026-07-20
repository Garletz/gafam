package main

// Triggering a clean GitHub Action build

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	mrand "math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Garletz/gafam/vpc-relay/browser"
	"github.com/Garletz/gafam/vpc-relay/karaka"
	"github.com/Garletz/gafam/vpc-relay/moksa"
	"github.com/Garletz/gafam/vpc-relay/sandbox"
	_ "modernc.org/sqlite"
)

var db *sql.DB
var jwtSecret []byte

// CertFingerprint is the SHA-256 fingerprint of the self-signed TLS certificate.
// It is announced to the Cloudflare directory so the Worker can verify the VPC identity.
func initDB() {
	var err error
	// Database path
	dbPath := "/app/data/gafam_relay.sqlite"
	if os.Getenv("ENV") == "development" {
		dbPath = "gafam_relay.sqlite"
	}

	db, err = sql.Open("sqlite", dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}

	// Enable WAL mode to prevent "database is locked" errors during concurrent read/writes
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		log.Println("Warning: Failed to enable WAL mode:", err)
	}

	// Create tables if they don't exist
	createDevicesTable := `
	CREATE TABLE IF NOT EXISTS gafam_devices (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		device_name TEXT,
		device_id TEXT UNIQUE,
		is_primary INTEGER,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createDevicesTable); err != nil {
		log.Fatal("Failed to create gafam_devices table:", err)
	}

	createSmsTable := `
	CREATE TABLE IF NOT EXISTS gafam_sms (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		sender TEXT,
		body TEXT,
		timestamp INTEGER,
		status TEXT DEFAULT 'inbox',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createSmsTable); err != nil {
		log.Fatal("Failed to create gafam_sms table:", err)
	}

	// Try to add status column if upgrading an existing DB
	db.Exec("ALTER TABLE gafam_sms ADD COLUMN status TEXT DEFAULT 'inbox';")
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_gafam_sms_dedup ON gafam_sms(sender, body, timestamp)`)

	createSessionsTable := `
	CREATE TABLE IF NOT EXISTS gafam_sessions (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT UNIQUE,
		phone TEXT,
		status TEXT DEFAULT 'pending',
		session_token TEXT,
		web_requested_at DATETIME,
		device_confirmed_at DATETIME,
		expires_at DATETIME,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createSessionsTable); err != nil {
		log.Fatal("Failed to create gafam_sessions table:", err)
	}

	createOutboxTable := `
	CREATE TABLE IF NOT EXISTS gafam_outbox (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		recipient TEXT,
		body TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createOutboxTable); err != nil {
		log.Fatal("Failed to create gafam_outbox table:", err)
	}

	createContactsTable := `
	CREATE TABLE IF NOT EXISTS gafam_contacts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		phone TEXT UNIQUE,
		name TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createContactsTable); err != nil {
		log.Fatal("Failed to create gafam_contacts table:", err)
	}

	createGuardiansTable := `
	CREATE TABLE IF NOT EXISTS trusted_guardians (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		name TEXT,
		phone_number TEXT UNIQUE,
		keyword TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createGuardiansTable); err != nil {
		log.Fatal("Failed to create trusted_guardians table:", err)
	}

	createWebClientsTable := `
	CREATE TABLE IF NOT EXISTS gafam_web_clients (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT UNIQUE,
		device_name TEXT,
		os_signature TEXT,
		ip_address TEXT,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createWebClientsTable); err != nil {
		log.Fatal("Failed to create gafam_web_clients table:", err)
	}

	createSettingsTable := `
	CREATE TABLE IF NOT EXISTS gafam_settings (
		key TEXT PRIMARY KEY,
		value TEXT
	);`
	if _, err := db.Exec(createSettingsTable); err != nil {
		log.Fatal("Failed to create gafam_settings table:", err)
	}

	log.Println("Database initialized successfully.")
}

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			http.Error(w, "Missing Authorization header", http.StatusUnauthorized)
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			http.Error(w, "Invalid Authorization format", http.StatusUnauthorized)
			return
		}

		tokenString := parts[1]
		if subtle.ConstantTimeCompare([]byte(tokenString), jwtSecret) != 1 {
			http.Error(w, "Invalid Token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	}
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		// Allow gafam.cloud and subdomains, plus local dev
		if strings.HasSuffix(origin, ".gafam.cloud") || origin == "https://gafam.cloud" || origin == "http://localhost:5173" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		secret = "development_secret"
		log.Println("WARNING: JWT_SECRET not set, using development secret.")
	}
	jwtSecret = []byte(secret)

	// TLS removed to allow Cloudflare Workers TCP Socket to connect

	initDB()
	initFeedTables()
	initLogsStore()
	initCredentialTables()
	initEmailTables()
	initVault()
	initMissionStore()
	registerVaultTools()
	sandbox.EnsureDirs()

	karaka.RegisterAllTools()
	karaka.RegisterDefaultKarakas()
	registerDelegationTool()
	registerSandboxTools()
	registerCDPTools()
	// llm.chat routes through the active orchestration engine (Suparna → Provider tab).
	// Registered here because it lives in package main (chatWithActiveEngine).
	karaka.RegisterTool(karaka.Tool{
		ID:          "llm.chat",
		Description: "Chat with the active orchestration engine. Uses scope-based routing with automatic failover (orchestrator → light_task → read_only → VPC Qwen).",
		Category:    "llm",
		Params: map[string]karaka.ParamSpec{
			"prompt":     {Type: "string", Required: true, Description: "User prompt"},
			"system":     {Type: "string", Required: false, Description: "System prompt"},
			"scope":      {Type: "string", Required: false, Description: "Scope: orchestrator (heavy reasoning), light_task (fast), read_only (search/classify)", Default: "light_task"},
			"max_tokens": {Type: "int", Required: false, Description: "Max completion tokens", Default: 512},
		},
		Returns: "{ content: string, engine: string, model: string, latency_ms: int }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			prompt, _ := params["prompt"].(string)
			if prompt == "" {
				return nil, fmt.Errorf("missing 'prompt'")
			}
			system, _ := params["system"].(string)
			scope, _ := params["scope"].(string)
			if scope == "" {
				scope = "light_task"
			}
			maxTokens := 512
			if mt, ok := params["max_tokens"].(float64); ok && mt > 0 {
				maxTokens = int(mt)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
			defer cancel()
			return chatWithEngine(ctx, scope, system, prompt, maxTokens)
		},
	})
	karaka.RegisterTool(karaka.Tool{
		ID:          "browser.sense",
		Description: "Fetch a URL AND ask the LLM to extract/answer a question about its content. Single call — fetch + understand.",
		Category:    "browser",
		Params: map[string]karaka.ParamSpec{
			"url":      {Type: "string", Required: true, Description: "Page URL"},
			"question": {Type: "string", Required: true, Description: "What to extract (e.g. 'main headlines', 'summarize in 2 sentences')"},
		},
		Returns: "{ answer: string, engine: string }",
		Handler: func(params map[string]interface{}) (interface{}, error) {
			url, _ := params["url"].(string)
			question, _ := params["question"].(string)
			if url == "" || question == "" {
				return nil, fmt.Errorf("url and question required")
			}
			// Fetch the page via browser sidecar
			browserBase := "http://gafam-browser:6080"
			if u := os.Getenv("BROWSER_URL"); u != "" {
				browserBase = strings.TrimRight(u, "/")
			}
			fetchURL := browserBase + "/fetch?url=" + url
			resp, err := http.Get(fetchURL)
			if err != nil {
				return nil, fmt.Errorf("fetch failed: %w", err)
			}
			defer resp.Body.Close()
			var fetchResult map[string]interface{}
			json.NewDecoder(resp.Body).Decode(&fetchResult)
			text, _ := fetchResult["text"].(string)
			if len(text) > 8000 {
				text = text[:8000]
			}
			// Ask LLM to answer the question about the page
			prompt := fmt.Sprintf("Page content from %s:\n\n%s\n\nQuestion: %s\nAnswer concisely in French.", url, text, question)
			ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
			defer cancel()
			result, err := chatWithEngine(ctx, "light_task", "", prompt, 1024)
			if err != nil {
				return nil, fmt.Errorf("llm: %w", err)
			}
			return map[string]interface{}{
				"answer": strings.TrimSpace(result.Content),
				"engine": result.Engine,
			}, nil
		},
	})
	log.Println("Kāraka tool registry initialized.")

	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("GET /api/_ping", pingHandler)
	mux.HandleFunc("GET /feed", publicFeedHandler)

	// Protected Routes (Bearer token from APK)
	mux.HandleFunc("POST /api/gafam/pair-device", authMiddleware(pairDeviceHandler))
	mux.HandleFunc("POST /api/auth/sms/", authMiddleware(smsHandler))
	mux.HandleFunc("POST /api/auth/sms/sync", authMiddleware(syncSmsHistoryHandler))
	mux.HandleFunc("GET /api/auth/sms/outbox", authMiddleware(getOutboxHandler))
	mux.HandleFunc("DELETE /api/auth/sms/outbox", authMiddleware(deleteOutboxHandler))
	mux.HandleFunc("POST /api/gafam/contacts", authMiddleware(syncContactsHandler))
	mux.HandleFunc("POST /api/auth/logs", authMiddleware(postLogsHandler))
	mux.HandleFunc("POST /api/auth/edge/sync", authMiddleware(edgeApkSyncHandler))
	mux.HandleFunc("GET /api/auth/edge/model", authMiddleware(edgeModelManifestHandler))
	mux.HandleFunc("GET /api/auth/edge/model/{file}", authMiddleware(edgeModelFileHandler))
	mux.HandleFunc("POST /api/auth/email/notif", authMiddleware(emailNotifHandler))

	// Auth Routes for Web Client handshake
	mux.HandleFunc("POST /api/auth/challenge", authMiddleware(challengeAuthHandler))
	mux.HandleFunc("DELETE /api/auth/logout", logoutHandler)

	// Settings API (protected by authMiddleware)
	mux.HandleFunc("GET /api/settings", authMiddleware(handleSettings))
	mux.HandleFunc("POST /api/settings", authMiddleware(handleSettings))

	// Session-protected routes for Web Client
	mux.HandleFunc("GET /api/web/sms", sessionMiddleware(getSmsHandler))
	mux.HandleFunc("POST /api/web/sms/outbox", sessionMiddleware(queueOutboxHandler))
	mux.HandleFunc("GET /api/web/logs", sessionMiddleware(getWebLogsHandler))
	mux.HandleFunc("DELETE /api/web/logs", sessionMiddleware(deleteWebLogsHandler))
	mux.HandleFunc("GET /api/web/logs/suparna/reading", sessionMiddleware(suparnaReadingHandler))
	mux.HandleFunc("POST /api/web/logs/suparna", sessionMiddleware(suparnaReadLogsHandler))
	mux.HandleFunc("GET /api/web/suparna/status", sessionMiddleware(suparnaStatusHandler))
	mux.HandleFunc("GET /api/web/edge/status", sessionMiddleware(edgeStatusHandler))
	mux.HandleFunc("POST /api/web/edge/infer", sessionMiddleware(edgeInferHandler))
	mux.HandleFunc("POST /api/web/edge/wake", sessionMiddleware(edgeWakeHandler))
	mux.HandleFunc("POST /api/web/edge/stop", sessionMiddleware(edgeStopHandler))
	mux.HandleFunc("GET /api/web/edge/model", sessionMiddleware(edgeWebModelStatusHandler))
	mux.HandleFunc("POST /api/web/edge/model", sessionMiddleware(edgeWebModelInstallHandler))

	mux.HandleFunc("GET /api/proxy/contacts", sessionMiddleware(getContactsHandler))
	mux.HandleFunc("POST /api/proxy/contacts", sessionMiddleware(syncContactsHandler))

	mux.HandleFunc("GET /api/settings/guardians", sessionMiddleware(getGuardiansHandler))
	mux.HandleFunc("POST /api/settings/guardians", sessionMiddleware(addGuardianHandler))
	mux.HandleFunc("DELETE /api/settings/guardians", sessionMiddleware(deleteGuardianHandler))
	mux.HandleFunc("POST /api/web/settings", sessionMiddleware(handleSettings))
	mux.HandleFunc("GET /api/web/settings", sessionMiddleware(handleSettings))
	
	mux.HandleFunc("GET /api/web/vpc-info", sessionMiddleware(vpcInfoHandler))
	mux.HandleFunc("POST /api/web/vpc-update", sessionMiddleware(triggerUpdateHandler))

	mux.HandleFunc("GET /api/gafam/contacts", authMiddleware(getContactsHandler))

	// Scrcpy Remote Control HTTP Streams (Manifest 14)
	mux.HandleFunc("GET /ws/scrcpy/bridge", authMiddleware(scrcpyBridgeHandler))
	mux.HandleFunc("GET /api/scrcpy/video_stream", sessionMiddleware(scrcpyVideoStreamHandler))
	mux.HandleFunc("POST /api/scrcpy/input", sessionMiddleware(scrcpyInputHandler))
	mux.HandleFunc("OPTIONS /api/scrcpy/input", sessionMiddleware(scrcpyInputHandler))
	mux.HandleFunc("GET /api/scrcpy/shell_stream", sessionMiddleware(scrcpyShellStreamHandler))
	mux.HandleFunc("POST /api/scrcpy/shell_input", sessionMiddleware(scrcpyShellInputHandler))
	mux.HandleFunc("OPTIONS /api/scrcpy/shell_input", sessionMiddleware(scrcpyShellInputHandler))
	mux.HandleFunc("GET /api/scrcpy/status", sessionMiddleware(scrcpyStatusHandler))

	// Browser Remote Control (Manifest 22 — Vātāyana)
	mux.HandleFunc("GET /api/web/browser/status", sessionMiddleware(browser.StatusHandler))
	mux.HandleFunc("POST /api/web/browser/wake", sessionMiddleware(browser.WakeHandler))
	mux.HandleFunc("POST /api/web/browser/stop", sessionMiddleware(browser.StopHandler))
	mux.HandleFunc("GET /api/web/browser/stream", sessionMiddleware(browser.StreamHandler))
	mux.HandleFunc("POST /api/web/browser/input", sessionMiddleware(browser.InputHandler))
	mux.HandleFunc("OPTIONS /api/web/browser/input", sessionMiddleware(browser.InputHandler))
	// Vātāyana kāraka endpoints (Manifest 23 — Khadyota): read the web as text, drive Firefox
	mux.HandleFunc("GET /api/web/browser/fetch", sessionMiddleware(browser.FetchHandler))
	mux.HandleFunc("POST /api/web/browser/navigate", sessionMiddleware(browser.NavigateHandler))
	mux.HandleFunc("GET /api/web/browser/window", sessionMiddleware(browser.WindowHandler))
	mux.HandleFunc("/browser/", sessionMiddleware(browser.ProxyHandler))

	// Sandbox (Manifest 24 — Yantraśālā)
	mux.HandleFunc("GET /api/web/sandbox/status", sessionMiddleware(sandbox.StatusHandler))
	mux.HandleFunc("POST /api/web/sandbox/wake", sessionMiddleware(sandbox.WakeHandler))
	mux.HandleFunc("POST /api/web/sandbox/stop", sessionMiddleware(sandbox.StopHandler))
	mux.HandleFunc("GET /api/web/sandbox/storage-vpc", sessionMiddleware(sandbox.VpcStorageHandler))
	mux.HandleFunc("/api/web/sandbox/", sessionMiddleware(sandbox.FilesHandler))
	mux.HandleFunc("POST /api/web/sandbox/exec", sessionMiddleware(sandbox.ExecHandler))
	mux.HandleFunc("POST /api/web/sandbox-exec", sessionMiddleware(sandbox.ExecHandler))

	// Kāraka — tool registry & execution (Manifest 25/26)
	mux.HandleFunc("GET /api/web/karaka/tools", sessionMiddleware(karaka.ToolsListHandler))
	mux.HandleFunc("GET /api/web/karaka/status", sessionMiddleware(karaka.KarakasListHandler))
	mux.HandleFunc("POST /api/web/karaka/execute", sessionMiddleware(karaka.ExecuteHandler))

	// LLM providers & orchestration engine (Suparna → Provider tab)
	mux.HandleFunc("GET /api/web/llm/providers", sessionMiddleware(llmProvidersHandler))
	mux.HandleFunc("POST /api/web/llm/providers", sessionMiddleware(llmProvidersHandler))
	mux.HandleFunc("DELETE /api/web/llm/providers", sessionMiddleware(llmProvidersHandler))
	mux.HandleFunc("GET /api/web/llm/engine", sessionMiddleware(llmEngineHandler))
	mux.HandleFunc("POST /api/web/llm/engine", sessionMiddleware(llmEngineHandler))
	mux.HandleFunc("POST /api/web/llm/test", sessionMiddleware(llmTestHandler))
	mux.HandleFunc("POST /api/web/llm/chat", sessionMiddleware(llmChatHandler))
	mux.HandleFunc("GET /api/web/llm/scopes", sessionMiddleware(llmScopesHandler))
	mux.HandleFunc("POST /api/web/llm/scopes", sessionMiddleware(llmScopesHandler))

	// The Vault — research memory (notes = markdown truth, FTS5 = cache)
	mux.HandleFunc("GET /api/web/research/search", sessionMiddleware(researchSearchHandler))
	mux.HandleFunc("GET /api/web/research/notes", sessionMiddleware(researchNotesHandler))
	mux.HandleFunc("GET /api/web/research/note", sessionMiddleware(researchNoteHandler))

	// Saṃyojaka — autonomous orchestrator loop (Manifest 25)
	mux.HandleFunc("POST /api/web/orchestrator/run", sessionMiddleware(orchestratorRunHandler))
	mux.HandleFunc("GET /api/web/orchestrator/status", sessionMiddleware(orchestratorStatusHandler))

	// VPC↔VPC Federation (Manifest 17 — Poneglyph Part 3)
	mux.HandleFunc("GET /api/web/links", sessionMiddleware(linksHandler))
	mux.HandleFunc("POST /api/web/links", sessionMiddleware(linksHandler))
	mux.HandleFunc("DELETE /api/web/links", sessionMiddleware(linksHandler))
	mux.HandleFunc("GET /api/web/links/{phone}/scan", sessionMiddleware(feedScanHandler))
	mux.HandleFunc("POST /api/web/feed/publish", sessionMiddleware(feedPublishHandler))
	mux.HandleFunc("GET /api/web/feed", sessionMiddleware(feedOwnHandler))
	mux.HandleFunc("GET /api/web/inbox", sessionMiddleware(inboxHandler))
	mux.HandleFunc("GET /api/web/circles", sessionMiddleware(circlesHandler))
	mux.HandleFunc("POST /api/web/circles", sessionMiddleware(circlesHandler))
	mux.HandleFunc("DELETE /api/web/circles", sessionMiddleware(circlesHandler))
	mux.HandleFunc("GET /api/web/circles/{name}", sessionMiddleware(circleFeedHandler))

	// Credential vault + TOTP (encrypted at rest)
	mux.HandleFunc("GET /api/web/credentials", sessionMiddleware(credentialsHandler))
	mux.HandleFunc("POST /api/web/credentials", sessionMiddleware(credentialsHandler))
	mux.HandleFunc("DELETE /api/web/credentials", sessionMiddleware(credentialsHandler))
	mux.HandleFunc("GET /api/web/credentials/otp", sessionMiddleware(credentialsOTPHandler))
	mux.HandleFunc("GET /api/web/credentials/password", sessionMiddleware(credentialsPasswordHandler))

	// Email inbox (IMAP + Android Notification relay)
	mux.HandleFunc("GET /api/web/email/inbox", sessionMiddleware(emailInboxHandler))
	mux.HandleFunc("POST /api/web/email/fetch", sessionMiddleware(emailFetchHandler))
	mux.HandleFunc("GET /api/web/email/settings", sessionMiddleware(emailSettingsHandler))
	mux.HandleFunc("POST /api/web/email/settings", sessionMiddleware(emailSettingsHandler))

	// Mokṣa — Method4 quest board (Organic Tools)
	mux.HandleFunc("GET /api/web/mission/world-card", sessionMiddleware(moksa.WorldCardHandler))
	mux.HandleFunc("POST /api/web/mission", sessionMiddleware(moksa.CreateHandler))
	mux.HandleFunc("GET /api/web/mission", sessionMiddleware(moksa.ListHandler))
	mux.HandleFunc("GET /api/web/mission/{id}", sessionMiddleware(moksa.GetHandler))
	mux.HandleFunc("DELETE /api/web/mission/{id}", sessionMiddleware(moksa.DeleteHandler))
	mux.HandleFunc("POST /api/web/mission/{id}/quest/{qid}/claim", sessionMiddleware(moksa.ClaimHandler))
	mux.HandleFunc("POST /api/web/mission/{id}/quest/{qid}/run", sessionMiddleware(moksa.RunHandler))
	mux.HandleFunc("POST /api/web/mission/{id}/quest/{qid}/reward", sessionMiddleware(moksa.RewardHandler))
	mux.HandleFunc("POST /api/web/mission/{id}/quest", sessionMiddleware(moksa.AddQuestHandler))
	mux.HandleFunc("POST /api/web/mission/{id}/synthesize", sessionMiddleware(moksa.SynthesizeHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "5150"
	}

	server := &http.Server{
		Addr:      "0.0.0.0:" + port,
		Handler:   corsMiddleware(mux),
	}

	tlsPort := os.Getenv("TLS_PORT")
	tlsCert := os.Getenv("TLS_CERT")
	tlsKey := os.Getenv("TLS_KEY")

	// Start OPSEC Honeypot generator (Manifest 12)
	mrand.Seed(time.Now().UnixNano())
	startHoneypotGenerator()
	log.Println("Honeypot generator started (OPSEC)")

	if tlsPort != "" && tlsCert != "" && tlsKey != "" {
		tlsServer := &http.Server{
			Addr:    "0.0.0.0:" + tlsPort,
			Handler: corsMiddleware(mux),
			TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)), // Disable HTTP/2
		}
		log.Printf("GAFAM VPC Relay starting on 0.0.0.0:%s (HTTPS SNI Spoofing)", tlsPort)
		go func() {
			if err := tlsServer.ListenAndServeTLS(tlsCert, tlsKey); err != nil && err != http.ErrServerClosed {
				log.Fatalf("TLS Server error: %v", err)
			}
		}()
	}

	log.Printf("GAFAM VPC Relay starting on 0.0.0.0:%s (HTTP for Cloudflare)", port)

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal("Server error:", err)
	}
}

// Helpers
func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		http.Error(w, "Failed to marshal JSON", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Length", strconv.Itoa(len(jsonData)))
	w.WriteHeader(status)
	w.Write(jsonData)
}
