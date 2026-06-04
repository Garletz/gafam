# 10. Architecture: End-to-End Encryption (AES-GCM) & Remote SMS

## The Architectural Question: Are we making a mistake?

As GAFAM Relay evolves, the introduction of Cloudflare Workers and raw HTTP TCP Sockets raises a fundamental architectural question: **Is our architecture flawed, or are we building something robust like Matrix?**

The user expressed a concern: *We know the current system is vulnerable in plaintext, but if we implement Application Layer Encryption, is there a fundamental difference between us and Matrix? Must we remain cloud-agnostic?*

### The Answer: It's Exactly Like Matrix (Zero-Trust Architecture)

Our proposed architecture is **not an error**, it is actually the industry standard for secure, decentralized communication (Zero-Trust). 

In the Matrix protocol, "Homeservers" route messages between clients. These homeservers are **untrusted**. Matrix solves this by implementing Olm/Megolm End-to-End (E2E) encryption. The homeserver only sees encrypted ciphertext and routes it blindly.

Our architecture is fundamentally the same:
1. **The Relay Proxy (Cloudflare / Cloudron / Vercel)** is our "Homeserver". We treat it as an **untrusted pipe**.
2. **The Go VPC** is our ephemeral data store.
3. **The Android APK** (Client A) and **Svelte Web Client** (Client B) hold the cryptographic keys.

By implementing AES-256-GCM at the application layer, the intermediate nodes (whether it's Cloudflare's TCP Sockets, a self-hosted Node.js server, or the open internet) will only see binary ciphertext.

### Cloud-Agnosticism

We are **not locked into Cloudflare**.
The only Cloudflare-specific code we wrote is `import('cloudflare:sockets')`. We only did this because Cloudflare specifically blocks `fetch()` to raw IPv4 addresses (Error 1003). 
If this exact same SvelteKit code is deployed on a self-hosted VPS, Docker, Cloudron, or Vercel (using Node.js adapter), standard `fetch("http://IP:5150")` works natively! Our architecture remains 100% portable and cloud-agnostic.

---

## Technical Implementation Plan

### 1. End-to-End Encryption (AES-GCM)
Because we cannot use TLS with public CAs (as we have no domain name for the VPC), we use symmetric AES-256-GCM.

- **Keys**: 
  - `jwtSecret` (scanned via QR) is the Pre-Shared Key between Android and VPC.
  - `session_token` (generated on Web Login) is the Pre-Shared Key between VPC and Web Browser.
- **Android -> VPC**: Android encrypts the SMS JSON payload with a SHA-256 hash of `jwtSecret`.
- **VPC -> Web Client**: The VPC encrypts the SMS JSON array with a SHA-256 hash of `session_token`. The Svelte Web Client decrypts it locally using `window.crypto.subtle`. The proxy is blind.

### 2. 2-Way SMS (Outbox Polling)
Since the phone is behind a mobile NAT, the Web Client cannot push SMS directly to it.
- **Web Client**: Encrypts the outgoing SMS and POSTs it to the VPC via the proxy.
- **Go VPC**: Stores the encrypted SMS in a new `gafam_outbox` SQLite table.
- **Android App**: A background worker polls `GET /api/sms/outbox` every 10 seconds. When it finds a message, it decrypts it, uses Android's `SmsManager` to physically send the SMS, and then deletes it from the VPC outbox.
