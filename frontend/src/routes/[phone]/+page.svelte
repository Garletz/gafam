<script lang="ts">
  import { page } from '$app/state';
  import { onMount } from 'svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  // The phone number from the URL
  let phone = $derived(page.params.phone);

  // Connection state
  let state = $state<'setup' | 'waiting' | 'challenge' | 'connected'>('setup');
  
  // Challenge variables
  let inputTime = $state(''); // User inputs "18:36" or "1836"
  let challengeTimeStr = $state(''); // Normalized to "1836"
  let encryptedSafe = $state('');
  let safeSalt = $state('');
  let safeIv = $state('');
  let timeRemaining = $state(0);
  let challengeRemaining = $state(30);
  let challengeClicks = $state(0);

  let sessionToken = $state(data.sessionToken || '');
  let vpcUrl = $state(data.savedVpcUrl || '');
  let certFingerprint = $state(data.certFingerprint || '');
  let smsList = $state<any[]>([]);
  let pollInterval: ReturnType<typeof setInterval>;
  let countdownInterval: ReturnType<typeof setInterval>;
  let statusMsg = $state('');
  
  // Sending SMS state
  let outboxRecipient = $state('');
  let outboxBody = $state('');
  let outboxStatus = $state('');

  // Web Crypto API Helpers
  async function derivePBKDF2Key(passphrase: string, saltBase64: string) {
    const enc = new TextEncoder();
    const keyMaterial = await window.crypto.subtle.importKey(
      "raw",
      enc.encode(passphrase),
      { name: "PBKDF2" },
      false,
      ["deriveKey"]
    );
    const salt = base64ToArrayBuffer(saltBase64);
    return window.crypto.subtle.deriveKey(
      {
        name: "PBKDF2",
        salt: new Uint8Array(salt),
        iterations: 500000,
        hash: "SHA-256"
      },
      keyMaterial,
      { name: "AES-GCM", length: 256 },
      false,
      ["encrypt", "decrypt"]
    );
  }

  async function deriveKey(secret: string) {
    const enc = new TextEncoder();
    const hashBuffer = await window.crypto.subtle.digest('SHA-256', enc.encode(secret));
    return window.crypto.subtle.importKey(
      "raw",
      hashBuffer,
      { name: "AES-GCM" },
      false,
      ["encrypt", "decrypt"]
    );
  }

  function base64ToArrayBuffer(base64: string) {
    const binary_string = window.atob(base64);
    const len = binary_string.length;
    const bytes = new Uint8Array(len);
    for (let i = 0; i < len; i++) {
      bytes[i] = binary_string.charCodeAt(i);
    }
    return bytes.buffer;
  }

  function arrayBufferToBase64(buffer: ArrayBuffer) {
    let binary = '';
    const bytes = new Uint8Array(buffer);
    const len = bytes.byteLength;
    for (let i = 0; i < len; i++) {
      binary += String.fromCharCode(bytes[i]);
    }
    return window.btoa(binary);
  }

  async function decryptAESGCM(encryptedBase64: string, ivBase64: string, secret: string) {
    const key = await deriveKey(secret);
    const iv = base64ToArrayBuffer(ivBase64);
    const ciphertext = base64ToArrayBuffer(encryptedBase64);
    const decrypted = await window.crypto.subtle.decrypt(
      { name: "AES-GCM", iv: new Uint8Array(iv) },
      key,
      ciphertext
    );
    return new TextDecoder().decode(decrypted);
  }

  async function encryptAESGCM(plaintext: string, secret: string) {
    const key = await deriveKey(secret);
    const iv = window.crypto.getRandomValues(new Uint8Array(12));
    const encoded = new TextEncoder().encode(plaintext);
    const ciphertext = await window.crypto.subtle.encrypt(
      { name: "AES-GCM", iv },
      key,
      encoded
    );
    return {
      encrypted_data: arrayBufferToBase64(ciphertext),
      iv: arrayBufferToBase64(iv.buffer)
    };
  }

  onMount(() => {
    const saved = localStorage.getItem(`gafam_auth_${phone}`);
    if (saved) {
      try {
        const parsed = JSON.parse(saved);
        if (parsed.vpcUrl && parsed.sessionToken) {
          vpcUrl = parsed.vpcUrl;
          sessionToken = parsed.sessionToken;
          certFingerprint = parsed.certFingerprint || '';
        }
      } catch(e) {}
    }

    if (vpcUrl && sessionToken) {
      state = 'connected';
      loadSms();
      pollInterval = setInterval(loadSms, 5000);
    } else {
      state = 'setup';
    }

    return () => {
      if (pollInterval) clearInterval(pollInterval);
      if (countdownInterval) clearInterval(countdownInterval);
    };
  });

  async function startChallengeFlow(e: Event) {
    e.preventDefault();
    if (!inputTime) return;

    // Normalize time (e.g. "18:36" -> "1836")
    challengeTimeStr = inputTime.replace(/[^0-9]/g, '');
    if (challengeTimeStr.length !== 4) {
      statusMsg = 'Please enter time as HH:MM or HHMM';
      return;
    }

    statusMsg = '';
    startWaitingForTime();
  }

  function startWaitingForTime() {
    state = 'waiting';
    updateCountdown();
    countdownInterval = setInterval(updateCountdown, 1000);
  }

  function updateCountdown() {
    const now = new Date();
    const targetHour = parseInt(challengeTimeStr.substring(0, 2), 10);
    const targetMin = parseInt(challengeTimeStr.substring(2, 4), 10);
    
    const targetTime = new Date();
    targetTime.setHours(targetHour, targetMin, 0, 0);

    const diff = Math.floor((targetTime.getTime() - now.getTime()) / 1000);

    if (diff <= 0) {
      // Time has arrived
      clearInterval(countdownInterval);
      startActiveChallenge();
    } else {
      timeRemaining = diff;
    }
  }

  function startActiveChallenge() {
    state = 'challenge';
    challengeClicks = 0;
    challengeRemaining = 30;

    countdownInterval = setInterval(() => {
      challengeRemaining -= 1;
      if (challengeRemaining <= 0) {
        clearInterval(countdownInterval);
        processChallenge();
      }
    }, 1000);
  }

  function registerClick() {
    if (state === 'challenge') {
      challengeClicks += 1;
    }
  }

  async function processChallenge() {
    statusMsg = 'Retrieving & Deciphering safe... (this will take a moment)';
    
    // We wait a tiny bit so the UI updates
    setTimeout(async () => {
      try {
        const res = await fetch(`/api/directory?phone=${phone}&time=${challengeTimeStr}`);
        
        if (res.status === 429) {
           state = 'error';
           try {
             const errData = await res.json();
             statusMsg = errData.error || 'Rate limit exceeded. Try again later.';
           } catch(e) {
             statusMsg = 'Rate limit exceeded. Try again later.';
           }
           return;
        }

        const result = await res.json();
        if (!result.success) {
           state = 'error';
           statusMsg = 'Decryption failed (bad time or clicks).';
           return;
        }

        encryptedSafe = result.encrypted_safe;
        safeSalt = result.salt;
        safeIv = result.iv;
        const lockedDownCorrectTime = result.locked_down_correct_time;

        const passphrase = `${challengeTimeStr}-${challengeClicks}`;
        const aesKey = await derivePBKDF2Key(passphrase, safeSalt);
        
        const ivBuffer = base64ToArrayBuffer(safeIv);
        const ciphertext = base64ToArrayBuffer(encryptedSafe);
        const decrypted = await window.crypto.subtle.decrypt(
          { name: "AES-GCM", iv: new Uint8Array(ivBuffer) },
          aesKey,
          ciphertext
        );
        
        const plaintext = new TextDecoder().decode(decrypted);
        const safeData = JSON.parse(plaintext);
        
        if (safeData.vpcUrl && safeData.sessionToken) {
          vpcUrl = safeData.vpcUrl;
          sessionToken = safeData.sessionToken;
          localStorage.setItem(`gafam_auth_${phone}`, JSON.stringify({ vpcUrl, sessionToken, certFingerprint }));
          
          state = 'connected';
          statusMsg = '';
          loadSms();
          pollInterval = setInterval(loadSms, 5000);
        } else {
          throw new Error('Invalid safe contents');
        }
      } catch (err) {
        state = 'setup';
        if (typeof lockedDownCorrectTime !== 'undefined' && lockedDownCorrectTime === true) {
            statusMsg = 'SECURITY ALERT: Massive brute-force detected. Safe locked down. Please wait 24h or use a different phone number.';
        } else {
            statusMsg = 'Challenge failed. Wrong clicks or honeypot (fake safe).';
        }
      }
    }, 50);
  }

  async function loadSms() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy?${proxyParams.toString()}`);
      if (res.ok) {
        const payload = await res.json();
        if (payload.error) {
           statusMsg = 'VPC returned an error: ' + payload.error;
        } else if (payload.encrypted_data && payload.iv) {
           try {
             const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
             smsList = JSON.parse(plaintext);
             statusMsg = '';
           } catch (decErr: any) {
             statusMsg = 'Decryption failed: ' + decErr.message;
           }
        } else {
           smsList = payload;
           statusMsg = '';
        }
      } else if (res.status === 403) {
        if (pollInterval) clearInterval(pollInterval);
        state = 'setup';
        sessionToken = '';
        vpcUrl = '';
        statusMsg = 'Session expired. Please reauthorize from your phone.';
      } else {
        const errorData = await res.json().catch(() => ({}));
        statusMsg = errorData.error ? `Proxy error: ${errorData.error}` : `HTTP Error ${res.status}`;
      }
    } catch (e: any) {
      statusMsg = 'Cannot reach Cloudflare proxy: ' + e.message;
    }
  }

  async function sendSms(e: Event) {
    e.preventDefault();
    if (!outboxRecipient || !outboxBody) return;
    
    outboxStatus = 'Encrypting & Sending...';
    try {
      const plaintext = JSON.stringify({ recipient: outboxRecipient, body: outboxBody });
      const encryptedPayload = await encryptAESGCM(plaintext, sessionToken);
      
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy?${proxyParams.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(encryptedPayload)
      });
      
      if (res.ok) {
        outboxStatus = 'Sent to VPC Outbox! (Waiting for Android relay)';
        outboxBody = '';
        outboxRecipient = '';
        setTimeout(() => outboxStatus = '', 3000);
      } else {
        outboxStatus = 'Failed to send: HTTP ' + res.status;
      }
    } catch (err: any) {
      outboxStatus = 'Error: ' + err.message;
    }
  }

  async function logout() {
    if (vpcUrl && sessionToken) {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      // Fire and forget
      fetch(`/api/proxy?${proxyParams.toString()}`, { method: 'DELETE' }).catch(() => {});
    }

    state = 'setup';
    sessionToken = '';
    vpcUrl = '';
    certFingerprint = '';
    smsList = [];
    localStorage.removeItem(`gafam_auth_${phone}`);
  }

  function formatTime(ts: number) {
    return new Date(ts).toLocaleString();
  }
</script>

<svelte:head>
  <title>{phone} — GAFAM Relay</title>
</svelte:head>

<main class="relay-page">
  <div class="relay-page__glow"></div>

  <header class="relay-header">
    <a href="/" class="relay-header__logo">
      <span class="logo-g">G</span><span class="logo-rest">AFAM</span>
    </a>
    <div class="relay-header__phone">{phone}</div>
    {#if state === 'connected'}
      <button class="relay-header__logout" onclick={logout}>Logout</button>
    {/if}
  </header>

  <div class="relay-content">
    {#if state === 'setup'}
      <!-- SETUP CHALLENGE -->
      <div class="login-card">
        <h2 class="login-card__title">Authorization Required</h2>
        <p class="login-card__desc">Press <strong>"Authorize Web Login"</strong> on your phone and enter the challenge time below.</p>

        <form class="login-card__field" onsubmit={startChallengeFlow}>
          <label>Challenge Time</label>
          <input type="text" placeholder="e.g. 18:36" bind:value={inputTime} required />
          <button type="submit" class="login-card__btn" style="width:100%; margin-top:16px;">Next</button>
        </form>

        {#if statusMsg}
          <p class="login-card__status">{statusMsg}</p>
        {/if}
      </div>

    {:else if state === 'waiting'}
      <!-- WAITING FOR TARGET TIME -->
      <div class="login-card">
        <h2 class="login-card__title">Safe Retrieved</h2>
        <p class="login-card__desc">Waiting for {challengeTimeStr.substring(0,2)}:{challengeTimeStr.substring(2,4)} to open the safe.</p>

        <div class="countdown-display">
          {timeRemaining}s
        </div>

        <div class="waiting-dots">
          <span></span><span></span><span></span>
        </div>
      </div>

    {:else if state === 'challenge'}
      <!-- ACTIVE CHALLENGE (CLICKS) -->
      <div class="login-card challenge-card">
        <h2 class="login-card__title">Challenge Active</h2>
        <p class="login-card__desc">Click the button below the exact number of times shown on your phone.</p>

        <div class="challenge-timer">Time left: {challengeRemaining}s</div>
        
        <button class="challenge-btn" onclick={registerClick}>
          <div class="btn-pulse"></div>
          IMPULSE
        </button>
        
        <div class="challenge-counter">Registered impulses: {challengeClicks}</div>
      </div>

    {:else}
      <!-- CONNECTED: SMS DASHBOARD -->
      <div class="dashboard">
        <div class="dashboard__header">
          <h2>Messages</h2>
          <button class="btn-refresh" onclick={loadSms}>↻ Refresh</button>
        </div>
        
        {#if statusMsg}
          <div class="error-banner">{statusMsg}</div>
        {/if}

        <form class="outbox-form" onsubmit={sendSms}>
          <div class="outbox-form__inputs">
            <input type="text" placeholder="Recipient Number (e.g. 0611223344)" bind:value={outboxRecipient} required />
            <input type="text" placeholder="Message text..." bind:value={outboxBody} required />
          </div>
          <button type="submit" class="btn-send">Send SMS</button>
        </form>
        {#if outboxStatus}
          <div class="outbox-status">{outboxStatus}</div>
        {/if}

        {#if smsList.length === 0}
          <div class="dashboard__empty">
            <p>No messages yet.</p>
            <p class="text-muted">SMS received on your relay will appear here in real-time.</p>
          </div>
        {:else}
          <div class="sms-list">
            {#each smsList as sms}
              <div class="sms-card">
                <div class="sms-card__sender">{sms.sender}</div>
                <div class="sms-card__body">{sms.body}</div>
                <div class="sms-card__time">{formatTime(sms.timestamp)}</div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  </div>
</main>

<style>
  .relay-page {
    min-height: 100vh;
    position: relative;
  }

  .relay-page__glow {
    display: none;
  }

  /* Header */
  .relay-header {
    display: flex;
    align-items: center;
    padding: 20px 32px;
    gap: 16px;
    border-bottom: 1px solid var(--border);
    backdrop-filter: blur(20px);
    position: sticky;
    top: 0;
    z-index: 10;
    background: rgba(255, 255, 255, 0.8);
  }

  .relay-header__logo {
    font-size: 24px;
    font-weight: 900;
    letter-spacing: -1px;
    text-decoration: none;
  }

  .relay-header__phone {
    flex: 1;
    font-size: 15px;
    color: var(--text-secondary);
    font-weight: 500;
    letter-spacing: 1.5px;
    font-variant-numeric: tabular-nums;
  }

  .relay-header__logout {
    padding: 8px 20px;
    border-radius: 8px;
    background: transparent;
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 13px;
    transition: all var(--transition);
  }

  .relay-header__logout:hover {
    border-color: var(--danger);
    color: var(--danger);
  }

  .relay-content {
    max-width: 700px;
    margin: 0 auto;
    padding: 40px 24px;
  }

  /* Login Card */
  .login-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 48px 40px;
    text-align: center;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 16px;
  }

  .login-card__title {
    font-size: 24px;
    font-weight: 700;
  }

  .login-card__desc {
    color: var(--text-secondary);
    font-size: 14px;
    line-height: 1.6;
    max-width: 380px;
  }

  .login-card__field {
    width: 100%;
    max-width: 380px;
    text-align: left;
  }

  .login-card__field label {
    display: block;
    font-size: 12px;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 1px;
    margin-bottom: 8px;
    font-weight: 600;
  }

  .login-card__field input {
    width: 100%;
    padding: 14px 18px;
    border-radius: var(--radius-sm);
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    color: var(--text-primary);
    font-size: 15px;
    transition: border-color var(--transition);
  }

  .login-card__field input:focus {
    border-color: var(--accent);
    box-shadow: 0 0 0 3px var(--accent-glow);
  }

  .login-card__btn {
    position: relative;
    padding: 16px 48px;
    border-radius: 60px;
    background: var(--text-primary);
    color: white;
    font-size: 16px;
    font-weight: 700;
    letter-spacing: 0.5px;
    transition: all var(--transition);
    overflow: hidden;
    cursor: pointer;
  }

  .login-card__btn:hover {
    transform: translateY(-2px);
    box-shadow: 0 8px 30px var(--accent-glow);
  }

  .login-card__status {
    color: var(--warning);
    font-size: 13px;
    margin-top: 8px;
  }

  /* Waiting state */
  .countdown-display {
    font-size: 48px;
    font-weight: 900;
    color: var(--accent);
    font-variant-numeric: tabular-nums;
    margin: 20px 0;
  }

  .waiting-dots {
    display: flex;
    gap: 8px;
  }

  .waiting-dots span {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--text-muted);
    animation: dot-pulse 1.4s ease-in-out infinite;
  }

  .waiting-dots span:nth-child(2) { animation-delay: 0.2s; }
  .waiting-dots span:nth-child(3) { animation-delay: 0.4s; }

  @keyframes dot-pulse {
    0%, 80%, 100% { transform: scale(0.6); opacity: 0.4; }
    40% { transform: scale(1); opacity: 1; }
  }

  /* Challenge state */
  .challenge-card {
    border-color: var(--accent);
    box-shadow: 0 0 40px var(--accent-glow);
  }

  .challenge-timer {
    font-size: 20px;
    font-weight: 600;
    color: var(--danger);
    margin: 10px 0;
    font-variant-numeric: tabular-nums;
  }

  .challenge-btn {
    position: relative;
    width: 200px;
    height: 200px;
    border-radius: 50%;
    background: var(--accent);
    color: white;
    font-size: 24px;
    font-weight: 900;
    letter-spacing: 2px;
    border: none;
    cursor: pointer;
    box-shadow: 0 10px 30px var(--accent-glow);
    transition: transform 0.1s;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .challenge-btn:active {
    transform: scale(0.95);
  }

  .btn-pulse {
    position: absolute;
    inset: 0;
    border-radius: 50%;
    border: 4px solid rgba(255,255,255,0.4);
    animation: pulse-ring 2s ease-out infinite;
  }

  @keyframes pulse-ring {
    0% { transform: scale(1); opacity: 1; }
    100% { transform: scale(1.3); opacity: 0; }
  }

  .challenge-counter {
    font-size: 18px;
    font-weight: 500;
    margin-top: 20px;
  }

  /* Dashboard */
  .dashboard__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 24px;
  }

  .dashboard__header h2 {
    font-size: 22px;
    font-weight: 700;
  }

  .btn-refresh {
    padding: 8px 20px;
    border-radius: var(--radius-sm);
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 13px;
    transition: all var(--transition);
    cursor: pointer;
  }

  .btn-refresh:hover {
    border-color: var(--accent);
    color: var(--accent-light);
  }

  .dashboard__empty {
    text-align: center;
    padding: 60px 20px;
    color: var(--text-secondary);
  }

  .text-muted {
    color: var(--text-muted);
    font-size: 13px;
    margin-top: 8px;
  }

  .sms-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .sms-card {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    padding: 20px 24px;
    transition: all var(--transition);
  }

  .sms-card:hover {
    border-color: var(--border-hover);
    background: var(--bg-card-hover);
  }

  .sms-card__sender {
    font-size: 14px;
    font-weight: 700;
    color: var(--accent-light);
    margin-bottom: 6px;
    letter-spacing: 0.5px;
  }

  .sms-card__body {
    font-size: 15px;
    color: var(--text-primary);
    line-height: 1.5;
    margin-bottom: 8px;
    word-break: break-word;
  }

  .sms-card__time {
    font-size: 12px;
    color: var(--text-muted);
  }

  .outbox-form {
    display: flex;
    gap: 12px;
    margin-bottom: 24px;
    background: var(--bg-card);
    padding: 16px;
    border-radius: var(--radius-sm);
    border: 1px solid var(--border);
  }

  .outbox-form__inputs {
    display: flex;
    flex-direction: column;
    gap: 8px;
    flex: 1;
  }

  .outbox-form__inputs input {
    width: 100%;
    padding: 12px;
    border-radius: 8px;
    background: var(--bg-secondary);
    border: 1px solid var(--border);
    color: var(--text-primary);
  }

  .btn-send {
    padding: 0 24px;
    background: var(--accent);
    color: white;
    border-radius: 8px;
    font-weight: 600;
    transition: background 0.2s;
    border: none;
    cursor: pointer;
  }
  
  .btn-send:hover {
    background: var(--accent-light);
  }

  .outbox-status {
    font-size: 13px;
    color: var(--accent);
    margin-bottom: 24px;
    text-align: center;
  }

  .error-banner {
    background: rgba(255,59,48,0.1);
    color: var(--danger);
    padding: 12px;
    border-radius: 8px;
    margin-bottom: 16px;
    font-size: 14px;
    border: 1px solid rgba(255,59,48,0.2);
  }
</style>
