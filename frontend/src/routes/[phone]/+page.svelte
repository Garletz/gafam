<script lang="ts">
  import { page } from '$app/state';
  import { onMount } from 'svelte';
  import type { PageData } from './$types';

  let { data }: { data: PageData } = $props();

  // The phone number from the URL
  let phone = $derived(page.params.phone);

  // Connection state
  let state = $state<'waiting' | 'connected'>('waiting');
  let sessionToken = $state(data.sessionToken || '');
  let vpcUrl = $state(data.savedVpcUrl || '');
  let certFingerprint = $state(data.certFingerprint || '');
  let smsList = $state<any[]>([]);
  let pollInterval: ReturnType<typeof setInterval>;
  let statusMsg = $state('');
  
  // Sending SMS state
  let outboxRecipient = $state('');
  let outboxBody = $state('');
  let outboxStatus = $state('');

  // Web Crypto API Helpers
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
    // 1. Check if we have a saved session in localStorage
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

    // 2. If server provided new data (before it was consumed), we use it and save it
    if (data.savedVpcUrl && data.sessionToken) {
      vpcUrl = data.savedVpcUrl;
      sessionToken = data.sessionToken;
      certFingerprint = data.certFingerprint || '';
      localStorage.setItem(`gafam_auth_${phone}`, JSON.stringify({ vpcUrl, sessionToken, certFingerprint }));
    }

    // 3. Decide what to do
    if (vpcUrl && sessionToken) {
      state = 'connected';
      loadSms();
    } else {
      // Start polling Cloudflare directory for VPC IP
      startPollingDirectory();
    }

    return () => {
      if (pollInterval) clearInterval(pollInterval);
    };
  });

  function startPollingDirectory() {
    statusMsg = 'Waiting for device confirmation...';
    
    pollInterval = setInterval(async () => {
      try {
        const res = await fetch(`/api/directory?phone=${phone}`);
        if (res.ok) {
          const result = await res.json();
          if (result.success && result.vpcUrl && result.sessionToken) {
            clearInterval(pollInterval);
            vpcUrl = result.vpcUrl;
            sessionToken = result.sessionToken;
            certFingerprint = result.certFingerprint || '';
            localStorage.setItem(`gafam_auth_${phone}`, JSON.stringify({ vpcUrl, sessionToken, certFingerprint }));
            state = 'connected';
            statusMsg = '';
            loadSms();
          }
        }
      } catch (e) {
        // Just retry silently
      }
    }, 2000);
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
           // Decrypt the payload
           try {
             const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
             smsList = JSON.parse(plaintext);
             statusMsg = ''; // clear error
           } catch (decErr: any) {
             statusMsg = 'Decryption failed: ' + decErr.message;
           }
        } else {
           // Fallback if not encrypted (during migration)
           smsList = payload;
           statusMsg = '';
        }
      } else if (res.status === 403) {
        // Session expired or invalid
        state = 'waiting';
        sessionToken = '';
        vpcUrl = '';
        startPollingDirectory();
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

  function logout() {
    state = 'waiting';
    sessionToken = '';
    vpcUrl = '';
    certFingerprint = '';
    smsList = [];
    localStorage.removeItem(`gafam_auth_${phone}`);
    startPollingDirectory();
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
    {#if state === 'waiting'}
      <!-- WAITING FOR DEVICE CONFIRMATION -->
      <div class="login-card">
        <h2 class="login-card__title">Waiting for Device</h2>
        <p class="login-card__desc">Press <strong>"Authorize Web Login"</strong> on your GAFAM Relay app now.</p>

        <div class="waiting-dots">
          <span></span><span></span><span></span>
        </div>

        <p class="login-card__status">{statusMsg}</p>
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

        <!-- New SMS Form -->
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

  .login-card__icon {
    font-size: 48px;
    margin-bottom: 8px;
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
    margin-top: 8px;
    overflow: hidden;
  }

  .login-card__btn:hover {
    transform: translateY(-2px);
    box-shadow: 0 8px 30px var(--accent-glow);
  }

  .btn-pulse {
    position: absolute;
    inset: 0;
    border-radius: 60px;
    border: 2px solid rgba(255,255,255,0.3);
    animation: pulse-ring 2s ease-out infinite;
  }

  @keyframes pulse-ring {
    0% { transform: scale(1); opacity: 1; }
    100% { transform: scale(1.15); opacity: 0; }
  }

  .login-card__status {
    color: var(--warning);
    font-size: 13px;
    margin-top: 8px;
  }

  /* Waiting animation */
  .waiting-pulse {
    animation: bounce 1.5s ease-in-out infinite;
  }

  @keyframes bounce {
    0%, 100% { transform: translateY(0); }
    50% { transform: translateY(-10px); }
  }

  .waiting-dots {
    display: flex;
    gap: 8px;
  }

  .waiting-dots span {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--accent);
    animation: dot-pulse 1.4s ease-in-out infinite;
  }

  .waiting-dots span:nth-child(2) { animation-delay: 0.2s; }
  .waiting-dots span:nth-child(3) { animation-delay: 0.4s; }

  @keyframes dot-pulse {
    0%, 80%, 100% { transform: scale(0.6); opacity: 0.4; }
    40% { transform: scale(1); opacity: 1; }
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

  .dashboard__refresh {
    padding: 8px 20px;
    border-radius: var(--radius-sm);
    background: var(--bg-card);
    border: 1px solid var(--border);
    color: var(--text-secondary);
    font-size: 13px;
    transition: all var(--transition);
  }

  .dashboard__refresh:hover {
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

  @media (max-width: 480px) {
    .relay-header { padding: 16px 20px; }
    .relay-content { padding: 24px 16px; }
    .login-card { padding: 32px 24px; }
    .outbox-form { flex-direction: column; }
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
</style>
