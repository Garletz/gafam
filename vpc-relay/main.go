package main

import (
	"database/sql"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

var db *sql.DB
var jwtSecret []byte

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
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`
	if _, err := db.Exec(createSessionsTable); err != nil {
		log.Fatal("Failed to create gafam_sessions table:", err)
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
		
		// In a real system you'd use jwt.Parse. For this relay, we just check if it matches our shared secret
		// because the Desktop Manager didn't sign a standard JWT, it just sent the raw secret as the token in the QR code!
		// Wait, the Rust Manager generates: let jwt_secret: String = rand::thread_rng().sample_iter(&Alphanumeric).take(32).collect();
		// It's a raw secret string, not a JWT!
		// So we just string match it.
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
		// Allow gafam.cloud and subdomains
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

	initDB()

	mux := http.NewServeMux()

	// Public Routes
	mux.HandleFunc("GET /api/_ping", pingHandler)

	// Protected Routes (Bearer token from APK)
	mux.HandleFunc("POST /api/gafam/pair-device", authMiddleware(pairDeviceHandler))
	mux.HandleFunc("POST /api/sms/", authMiddleware(smsHandler))
	mux.HandleFunc("GET /api/sms/", authMiddleware(getSmsHandler))

	// Auth Routes for Web Client handshake
	mux.HandleFunc("POST /api/auth/request-session", requestSessionHandler)
	mux.HandleFunc("POST /api/auth/confirm-session", authMiddleware(confirmSessionHandler))
	mux.HandleFunc("GET /api/auth/check-session", checkSessionHandler)

	// Session-protected routes for Web Client
	mux.HandleFunc("GET /api/web/sms", sessionMiddleware(getSmsHandler))

	port := os.Getenv("PORT")
	if port == "" {
		port = "5150"
	}

	log.Printf("GAFAM VPC Relay starting on 0.0.0.0:%s", port)
	if err := http.ListenAndServe("0.0.0.0:"+port, corsMiddleware(mux)); err != nil {
		log.Fatal("Server error:", err)
	}
}

// Helpers
func sendJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
