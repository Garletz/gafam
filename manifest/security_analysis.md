# Manifest 16: Comprehensive Security & Threat Analysis

This document provides a holistic security analysis of the GAFAM architecture, focusing on the interactions between the Cloudflare Worker (Frontend/Proxy), the VPC Relay (Backend), and the Web Client. It identifies potential attack vectors, evaluates their impact, and proposes architectural mitigations.

## 1. Perimeter Defenses & The Proxy Layer

The architecture relies heavily on Cloudflare Workers acting as a TCP proxy to the VPC Relay via `cloudflare:sockets`. This design inherently shifts the attack surface.

### 1.1 The SSRF Vulnerability (Server-Side Request Forgery)
**Vulnerability:** As identified previously, the `api/proxy/*` endpoints in the Cloudflare Worker blindly trust the `vpcUrl` query parameter provided by the client.
**Attack Scenario:** An attacker crafts a URL like `https://[phone].gafam.cloud/api/proxy/contacts?vpcUrl=http://169.254.169.254:80`. The Cloudflare Worker, running in Cloudflare's infrastructure, executes the request against the internal AWS/GCP metadata service or any other internal/external target.
**Impact:** 
- **Critical**. Cloudflare infrastructure could be used to launch DDoS attacks, scan internal networks, or exfiltrate metadata.
- **Bypass of Network Segmentation:** If the VPC itself relies on IP whitelisting (allowing only Cloudflare IPs), an attacker can use this SSRF to bounce traffic through Cloudflare and hit the VPC's protected management ports.
**Mitigation:** 
- **Strict Whitelisting:** The Worker MUST validate `vpcUrl` against a known-good list of VPC IPs stored in Cloudflare D1/KV for that specific `[phone]` tenant.
- **Cryptographic Signatures:** The `vpcUrl` could be signed by the VPC during the initial pairing, preventing tampering.

### 1.2 Lack of Mutual TLS (mTLS) or Strict VPC Authentication
**Vulnerability:** The Cloudflare Worker connects to the VPC Relay via raw TCP sockets (`cloudflare:sockets`), constructing HTTP requests manually. The connection is often over plain HTTP (e.g., port 5150) or relies on SNI spoofing (Manifest 12).
**Attack Scenario:** An attacker on the same network path as the VPC (e.g., a compromised neighboring VPS on DigitalOcean) could intercept the traffic or perform a Man-in-the-Middle (MitM) attack.
**Impact:** **High**. Session tokens, JWTs, and encrypted safes could be intercepted in transit if not protected by a strong outer TLS layer.
**Mitigation:** 
- **Cloudflare Tunnels (cloudflared):** Replace the raw TCP socket proxying with a secure Cloudflare Tunnel running directly on the VPC. This guarantees encrypted, authenticated traffic from the edge to the VPC without exposing public ports.

## 2. API Authentication & Authorization (VPC Relay)

The VPC Relay (`api.go`) exposes several endpoints. The security model relies on `authMiddleware` (using a static `JWT_SECRET`) for device-to-VPC communication and `sessionMiddleware` for web-to-VPC communication.

### 2.1 The Unauthenticated Challenge Creation (DoS)
**Vulnerability:** `challengeAuthHandler` (`POST /api/auth/challenge`) does not use `authMiddleware` or any rate limiting.
**Attack Scenario:** An attacker continuously sends POST requests to `/api/auth/challenge` with random `phone` numbers and `challengeTime` values.
**Impact:** **High**. 
- **Resource Exhaustion:** Fills the `gafam_sessions` table and exhausts server memory/CPU due to repetitive PBKDF2 hashing (500,000 iterations).
- **Denial of Service (DoS):** Overwrites legitimate user sessions and encrypted safes on the Cloudflare directory, preventing legitimate users from recovering their accounts.
**Mitigation:**
- **Require Authentication:** The APK must authenticate (e.g., using a short-lived token or the static JWT) when pushing a challenge.
- **Rate Limiting:** Implement strict IP-based or phone-number-based rate limiting on this endpoint.

### 2.2 JWT Secret Management & Rotation
**Vulnerability:** The `JWT_SECRET` is passed as an environment variable and seems static for the lifetime of the VPC.
**Attack Scenario:** If the `JWT_SECRET` is leaked (e.g., via a path traversal vulnerability, memory dump, or compromised environment variables), the attacker gains full control over the VPC API.
**Impact:** **Critical**. Complete compromise of the relay. The attacker can read/write SMS, modify contacts, and initiate Scrcpy sessions.
**Mitigation:** 
- Rotate secrets periodically.
- Consider asymmetric cryptography (public/private key pairs) for device authentication instead of a shared symmetric secret.

## 3. Remote Control (Scrcpy Bridge)

The Scrcpy integration presents unique challenges due to its raw access to the device screen and input.

### 3.1 WebSocket Token Exposure
**Vulnerability:** The WebSocket connection for Scrcpy (`ws://.../ws/scrcpy/bridge?token=...`) passes the authentication token in the URL query string.
**Attack Scenario:** URLs, including query parameters, are often logged by intermediate proxies, load balancers, or browser history.
**Impact:** **Medium/High**. If an attacker obtains the token from a log file, they can hijack the WebSocket session and control the device.
**Mitigation:**
- Initiate the WebSocket connection using an HTTP header (`Authorization: Bearer ...`) or via a secure handshake protocol *after* the WebSocket upgrade is established.

### 3.2 Lack of Device-Side Indication (Stealth Mode Risk)
**Vulnerability:** Scrcpy can run silently in the background. If a session is initiated maliciously, the user has no visual indicator that their screen is being mirrored.
**Impact:** **Critical (Privacy)**. Complete loss of privacy.
**Mitigation:**
- The Android App MUST display an un-dismissible notification (Foreground Service) while a Scrcpy session is active.
- Implement an explicit authorization prompt on the device screen when a new Scrcpy connection is requested.

## 4. Cryptographic Implementation (Emergency Recovery)

The Social Recovery mechanism uses PBKDF2 and AES-256-GCM.

### 4.1 PBKDF2 Iteration Count vs. Entropy
**Vulnerability:** The passphrase is constructed as `challengeTime-challengeClicks` (e.g., `1944-5`). This has extremely low entropy (only 24 hours * 60 minutes * 8 clicks = 11,520 possible combinations).
**Attack Scenario:** An attacker intercepts the encrypted safe and the salt from Cloudflare. They can easily brute-force the passphrase offline because the keyspace is trivially small.
**Impact:** **Critical**. The attacker decrypts the safe, obtains the `sessionToken` and `vpcUrl`, and hijacks the session.
**Mitigation:**
- While 500,000 PBKDF2 iterations slow down the attack, the keyspace is too small to resist a determined attacker with GPUs.
- **Increase Entropy:** Add a random, user-memorizable word to the challenge (e.g., `1944-5-Apple`) or require a longer, truly random PIN.
- **Lockout Mechanism:** Since offline brute-forcing cannot be prevented by server-side rate limits, the only defense is a stronger passphrase.

## 5. Summary of Recommendations

1. **Fix the SSRF:** Implement strict validation and whitelisting of `vpcUrl` in all Cloudflare proxy routes.
2. **Secure the API:** Add authentication and rate limiting to the `challengeAuthHandler`.
3. **Strengthen Cryptography:** Drastically increase the entropy of the Emergency Recovery passphrase. A time and a single digit are insufficient against offline brute-force attacks.
4. **Enhance WebSocket Security:** Move authentication tokens out of URL query parameters.
5. **Protect Privacy:** Ensure the Android application visibly notifies the user during active remote control sessions.
