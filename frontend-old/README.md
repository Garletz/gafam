# GAFAM Frontend — 3D Command Deck Client

The GAFAM frontend is a responsive client cockpit built on **Svelte + Vite (TypeScript)** to handle high-performance CSS 3D canvas physics, dynamic mouse tilt parallax, and real-time device coordinate synchronization.

---

## 📐 3D Platform Geometry & Parallax Physics

The spatial visual platform is designed entirely in vanilla CSS and responsive calculations, ensuring extremely fast page load times and butter-smooth rendering without the overhead of WebGL/Three.js:

*   **Platform Perspective:** Employs CSS 3D Transforms (`perspective()` and `rotateX()`) to establish a premium tapered silver board deck with a plunging front face.
*   **Parallax Mouse Tilt:** Dampens and interpolates mouse pointer coordinates across Svelte's reactive state to tilt the platform deck naturally:
    ```typescript
    // ny represents standard offset values between -1.0 and 1.0
    const ny = (e.clientY / window.innerHeight - 0.5) * 2;
    targetRotateX = baseRotateX - ny * parallaxStrength * 7;
    
    // Animate transition using simple linear interpolation (lerp)
    currentRotateX += (targetRotateX - currentRotateX) * 0.07;
    ```
*   **Encapsulated Drag & Drop:** Card dragging uses pointer coordinates bounded strictly within the active parent deck width and height.

---

## 📋 Features & Roadmap (Backlog)

### 🟩 Phase 1: Local POC (Completed)
*   [x] Initialized lightweight Svelte Vite framework.
*   [x] Enlarged CSS 3D platform styled as floating silver/marble brushed aluminum deck.
*   [x] Programmed mouse move parallax and card drag boundary limits.
*   [x] Built the **"Device Node [00_LOC]"** console displaying live POS tracking and cryptographic metadata.

### 🟨 Phase 2: Circle of Trust & P2P Backup (Planned / Next Steps)
*   [ ] **Trust Delegation Integration:** Link the **"Request Backup P2P Token"** button to dispatch real-time notifications to your partner's active terminal.
*   [ ] **Partner Verification View:** Create a dedicated popup overlay to allow the partner to view, approve, and decrypt incoming delegated authentication challenges.
*   [ ] **Localstorage trusted-key exchange:** Implement asymmetric P2P key pair signatures (WebCrypto API) to securely whitelist new device fingerprints without passwords.

---

## 🚀 Local Execution

Spin up the Vite development server:

```bash
# Install dependencies
npm install

# Start Vite client dev server
npm run dev
```
*Vite client dev server will boot on `http://localhost:5173`*
