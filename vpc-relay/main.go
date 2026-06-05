package main

import (
	"database/sql"
	"encoding/json"
	"log"
	mrand "math/rand"
	"net/http"
	"os"
	"strings"
	"time"

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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createSmsTable); err != nil {
		log.Fatal("Failed to create gafam_sms table:", err)
	}

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
		if tokenString != string(jwtSecret) {
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

	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("GET /api/_ping", pingHandler)

	// Protected Routes (Bearer token from APK)
	mux.HandleFunc("POST /api/gafam/pair-device", authMiddleware(pairDeviceHandler))
	mux.HandleFunc("POST /api/auth/sms/", authMiddleware(smsHandler))
	mux.HandleFunc("GET /api/auth/sms/outbox", authMiddleware(getOutboxHandler))
	mux.HandleFunc("DELETE /api/auth/sms/outbox", authMiddleware(deleteOutboxHandler))
	
	// Auth Routes for Web Client handshake (legacy)
	mux.HandleFunc("POST /api/auth/request-session", requestSessionHandler)
	mux.HandleFunc("POST /api/auth/confirm-session", authMiddleware(confirmSessionHandler))
	mux.HandleFunc("GET /api/auth/check-session", checkSessionHandler)

	// Rendez-vous Synchrone Mécanique (Manifest 12)
	mux.HandleFunc("POST /api/auth/challenge", authMiddleware(challengeAuthHandler))
	mux.HandleFunc("DELETE /api/auth/logout", logoutHandler)

	// Session-protected routes for Web Client
	mux.HandleFunc("GET /api/web/sms", sessionMiddleware(getSmsHandler))
	mux.HandleFunc("POST /api/web/sms/outbox", sessionMiddleware(queueOutboxHandler))

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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
