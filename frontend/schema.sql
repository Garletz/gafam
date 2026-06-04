CREATE TABLE IF NOT EXISTS directory (
  phone_number TEXT PRIMARY KEY,
  vpc_url TEXT NOT NULL,
  session_token TEXT,
  cert_fingerprint TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
