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
  let contacts = $state<Record<string, string>>({});
  let sidebarTab = $state<'chats' | 'contacts'>('chats');
  let contactSearchQuery = $state('');
  let syncContacts = $state(true);
  let selectedSender = $state<string | null>(null);
  let pollInterval: ReturnType<typeof setInterval>;
  let countdownInterval: ReturnType<typeof setInterval>;
  let statusMsg = $state('');
  
  let outboxRecipient = $state('');
  let outboxBody = $state('');
  let outboxStatus = $state('');
  
  // Profile menu state
  let isProfileMenuOpen = $state(false);

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

  function getRootDomain() {
    if (typeof window !== 'undefined') {
      const hostname = window.location.hostname;
      if (hostname.includes('gafam.cloud')) return '.gafam.cloud';
      return hostname;
    }
    return '';
  }

  onMount(() => {
    let saved = null;
    const match = document.cookie.match(new RegExp('(^| )' + `gafam_auth_${phone}` + '=([^;]+)'));
    if (match) saved = decodeURIComponent(match[2]);
    
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

  function getContactName(sender: string) {
    if (contacts[sender]) return contacts[sender];
    if (!sender) return 'Unknown';
    
    const normSender = sender.replace(/\D/g, '');
    if (normSender.length < 6) return sender;
    
    const suffixLen = Math.min(normSender.length, 9);
    const senderSuffix = normSender.slice(-suffixLen);
    
    for (const [phone, name] of Object.entries(contacts)) {
      const normContact = phone.replace(/\D/g, '');
      if (normContact.endsWith(senderSuffix)) {
        if (normContact.length >= suffixLen) {
          return name;
        }
      }
    }
    return sender;
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
          const authData = JSON.stringify({ vpcUrl, sessionToken, certFingerprint });
          document.cookie = `gafam_auth_${phone}=${encodeURIComponent(authData)}; domain=${getRootDomain()}; path=/; max-age=31536000`;
          window.dispatchEvent(new Event('gafam-auth-changed'));
          
          state = 'connected';
          statusMsg = '';
          loadSms();
          loadContacts();
          loadSettings();
          pollInterval = setInterval(() => {
            loadSms();
            loadContacts();
            loadSettings();
          }, 5000);
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

  
  let optimisticSms = $state<any[]>([]);

  let conversations = $derived(() => {
    const groups: Record<string, any[]> = {};
    
    for (const sms of smsList) {
      if (!groups[sms.sender]) groups[sms.sender] = [];
      groups[sms.sender].push(sms);
    }
    
    if (selectedSender && !groups[selectedSender]) {
      groups[selectedSender] = [];
    }
    
    const now = Date.now();
    for (const opt of optimisticSms) {
      if (!groups[opt.sender]) groups[opt.sender] = [];
      
      const hasRealMatch = groups[opt.sender].some(real => 
         real.direction === 'outbound' && 
         real.body === opt.body && 
         Math.abs(real.timestamp - opt.timestamp) < 120000
      );
      
      if (!hasRealMatch && (now - opt.timestamp) < 120000) {
         groups[opt.sender].push(opt);
      }
    }

    for (const k in groups) {
      groups[k].sort((a,b) => a.timestamp - b.timestamp);
    }
    return groups;
  });

  let filteredContacts = $derived(() => {
    const entries = Object.entries(contacts);
    if (!contactSearchQuery) return entries;
    const q = contactSearchQuery.toLowerCase();
    return entries.filter(([cPhone, cName]) => 
      cName.toLowerCase().includes(q) || cPhone.includes(q)
    );
  });

  async function loadContacts() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy/contacts?${proxyParams.toString()}`);
      if (res.ok) {
        const payload = await res.json();
        if (payload.encrypted_data && payload.iv) {
          try {
            const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
            const list = JSON.parse(plaintext);
            const map: Record<string, string> = {};
            for (const c of list) {
              map[c.phone_number] = c.display_name;
            }
            contacts = map;
          } catch (e) {
            console.error("Failed to decrypt contacts", e);
          }
        } else if (Array.isArray(payload)) {
          // Fallback if not yet encrypted (e.g. before VPC restart)
          const map: Record<string, string> = {};
          for (const c of payload) {
            map[c.phone_number] = c.display_name;
          }
          contacts = map;
        }
      }
    } catch(e) {}
  }

  async function loadSms() {
    loadContacts();
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
    
    const body = outboxBody;
    const recipient = outboxRecipient;
    outboxBody = ''; // Clear input immediately
    
    // Add optimistic message
    const optMsg = {
      sender: recipient,
      direction: 'outbound',
      body: body,
      timestamp: Date.now(),
      status: 'sending'
    };
    optimisticSms = [...optimisticSms, optMsg];
    
    try {
      const plaintext = JSON.stringify({ recipient, body });
      const encryptedPayload = await encryptAESGCM(plaintext, sessionToken);
      
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy?${proxyParams.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(encryptedPayload)
      });
      
      if (!res.ok) {
        outboxStatus = 'Failed to send: HTTP ' + res.status;
      }
    } catch (err: any) {
      outboxStatus = 'Error: ' + err.message;
    }
  }

  async function loadSettings() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/settings?${proxyParams.toString()}`);
      if (res.ok) {
        const payload = await res.json();
        if (payload.encrypted_data && payload.iv) {
          try {
            const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
            const settingsObj = JSON.parse(plaintext);
            if (settingsObj.contacts_sync_enabled !== undefined) {
              syncContacts = settingsObj.contacts_sync_enabled === "true";
            }
          } catch(e) {}
        }
      }
    } catch (e) {}
  }

  async function toggleContactSync() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken });
      const plaintext = JSON.stringify({ key: 'contacts_sync_enabled', value: syncContacts ? "true" : "false" });
      const encryptedPayload = await encryptAESGCM(plaintext, sessionToken);

      await fetch(`/api/proxy/settings?${proxyParams.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(encryptedPayload)
      });
    } catch (e) {}
  }

  // (logout function was moved to layout)

  function formatTime(ts: number) {
    return new Date(ts).toLocaleString();
  }
</script>

<svelte:head>
  <title>{phone} — GAFAM Relay</title>
</svelte:head>

<main class="relay-page">
  <div class="relay-content">
    {#if state === 'setup'}
      <!-- SETUP CHALLENGE -->
      <div class="login-card">
        <h2 class="login-card__title">Authorization Required</h2>
        <p class="login-card__desc">Press <strong>"Authorize Web Login"</strong> on your phone and enter the challenge time below.</p>

        <form class="login-card__field" onsubmit={startChallengeFlow}>
          <label>Challenge Time</label>
          <input type="text" placeholder="e.g. 18:36" bind:value={inputTime} required />
          <button type="submit" class="login-card__btn">Next</button>
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
          IMPULSE
        </button>
        
        <div class="challenge-counter">Registered impulses: {challengeClicks}</div>
      </div>

    {:else}
      <!-- CONNECTED: NEW MESSENGER UI -->
      <div class="messenger-ui">
        <!-- SIDEBAR -->
        <aside class="sidebar">
          <div class="sidebar__header">
            <div class="sidebar__tabs">
              <button class="tab {sidebarTab === 'chats' ? 'active' : ''}" onclick={() => sidebarTab = 'chats'}>Chats</button>
              <button class="tab {sidebarTab === 'contacts' ? 'active' : ''}" onclick={() => sidebarTab = 'contacts'}>Contacts</button>
            </div>
            <div class="sidebar__actions">
              {#if sidebarTab === 'contacts'}
                <div class="contact-search">
                  <input type="search" placeholder="Search contacts..." bind:value={contactSearchQuery} />
                </div>
              {/if}
              <label class="toggle-sync" title="Sync Contacts with Android">
                <input type="checkbox" bind:checked={syncContacts} onchange={toggleContactSync} />
                <span>Sync Contacts</span>
              </label>
            </div>
          </div>
          <div class="sidebar__list">
            {#if sidebarTab === 'chats'}
              {#each Object.keys(conversations()) as sender}
                <button class="chat-item {selectedSender === sender ? 'active' : ''}" onclick={() => selectedSender = sender}>
                  <div class="chat-item__avatar">{ getContactName(sender).charAt(0).toUpperCase() }</div>
                  <div class="chat-item__info">
                    <div class="chat-item__name">{getContactName(sender)}</div>
                    <div class="chat-item__preview">
                      {#if conversations()[sender].length > 0}
                        {conversations()[sender][conversations()[sender].length - 1].body.substring(0, 30)}...
                      {:else}
                        New conversation
                      {/if}
                    </div>
                  </div>
                </button>
              {/each}
            {:else}
              {#each filteredContacts() as [cPhone, cName]}
                <button class="chat-item" onclick={() => { selectedSender = cPhone; sidebarTab = 'chats'; }}>
                  <div class="chat-item__avatar">{ cName.charAt(0).toUpperCase() }</div>
                  <div class="chat-item__info">
                    <div class="chat-item__name">{cName}</div>
                    <div class="chat-item__preview contact-phone">{cPhone}</div>
                  </div>
                </button>
              {/each}
            {/if}
          </div>
        </aside>

        <!-- MAIN CHAT -->
        <main class="chat-main">
          {#if selectedSender}
            <div class="chat-main__header">
              <h3>{getContactName(selectedSender)}</h3>
            </div>
            <div class="chat-main__messages">
              {#each (conversations()[selectedSender] || []) as sms}
                <div class="msg {sms.direction === 'outbound' ? 'msg--out' : 'msg--in'} {sms.status === 'sending' ? 'msg--sending' : ''}">
                  <div class="msg__bubble">{sms.body}</div>
                  <div class="msg__time">
                     {formatTime(sms.timestamp)}
                     {#if sms.status === 'sending'} <span>(Sending...)</span>{/if}
                  </div>
                </div>
              {/each}
            </div>
            <div class="chat-main__input">
              <form class="outbox-form" onsubmit={sendSms}>
                <input type="text" placeholder="Send a message..." bind:value={outboxBody} required />
                <button type="submit" class="btn-send" onclick={() => outboxRecipient = selectedSender!}>Send</button>
              </form>
              {#if outboxStatus}
                <div class="outbox-status">{outboxStatus}</div>
              {/if}
            </div>
          {:else}
            <div class="chat-main__empty">
              <p>Select a chat to start messaging</p>
            </div>
          {/if}
        </main>
      </div>
    {/if}
  </div>
</main>

<style>
  :global(body) {
    background: #f8f9fa;
    color: #202124;
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  }
  .relay-page {
    flex: 1;
    display: flex;
    flex-direction: column;
  }
  .relay-content {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 20px;
  }
  .login-card {
    background: #ffffff;
    padding: 40px;
    border-radius: 12px;
    border: 1px solid #dfe1e5;
    text-align: center;
    width: 100%;
    max-width: 400px;
    box-shadow: 0 4px 6px rgba(0,0,0,0.05);
  }
  .login-card__title { margin: 0 0 10px; font-size: 20px; color: #202124; }
  .login-card__desc { color: #5f6368; font-size: 14px; margin-bottom: 24px; }
  .login-card__field label { display: block; text-align: left; margin-bottom: 8px; color: #5f6368; font-size: 12px; text-transform: uppercase; }
  .login-card__field input { width: 100%; box-sizing: border-box; padding: 12px; background: #f1f3f4; border: 1px solid #dfe1e5; color: #202124; border-radius: 6px; margin-bottom: 16px; }
  .login-card__btn { width: 100%; padding: 12px; background: #202124; color: white; font-weight: bold; border: none; border-radius: 6px; cursor: pointer; }
  .login-card__status { color: #d93025; margin-top: 16px; font-size: 13px; }
  
  .challenge-btn {
    width: 150px; height: 150px; border-radius: 50%; background: #f1f3f4; color: #202124; font-size: 18px; border: 1px solid #dfe1e5; cursor: pointer; margin: 20px auto; display: block; box-shadow: 0 4px 10px rgba(0,0,0,0.05);
  }
  .challenge-btn:active { background: #e8eaed; transform: scale(0.95); }
  .challenge-counter { font-size: 18px; font-weight: 500; margin-top: 20px; }
  
  .messenger-ui {
    display: flex;
    width: 100%;
    max-width: 1200px;
    height: 80vh;
    border: 1px solid #dfe1e5;
    border-radius: 12px;
    overflow: hidden;
    background: #ffffff;
    box-shadow: 0 8px 24px rgba(0,0,0,0.08);
  }
  .sidebar {
    width: 320px;
    background: #ffffff;
    border-right: 1px solid #dfe1e5;
    display: flex;
    flex-direction: column;
  }
  .sidebar__header {
    padding: 16px;
    border-bottom: 1px solid #dfe1e5;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .sidebar__tabs {
    display: flex;
    gap: 8px;
  }
  .tab {
    flex: 1;
    padding: 8px;
    border: none;
    background: transparent;
    font-size: 14px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
    border-bottom: 2px solid transparent;
  }
  .tab.active {
    color: #202124;
    border-bottom-color: #202124;
  }
  .sidebar__actions {
    display: flex;
    flex-direction: column;
    align-items: stretch;
    justify-content: flex-end;
    gap: 12px;
  }
  .contact-search { width: 100%; }
  .contact-search input {
    width: 100%;
    box-sizing: border-box;
    padding: 8px 12px;
    border-radius: 20px;
    border: 1px solid #dfe1e5;
    background: #f1f3f4;
    font-size: 13px;
    outline: none;
  }
  .contact-search input:focus { border-color: #bdc1c6; }
  
  .toggle-sync {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: #5f6368;
    cursor: pointer;
    font-weight: 600;
  }
  .toggle-sync input {
    accent-color: #202124;
    cursor: pointer;
  }

  .sidebar__list {
    flex: 1;
    overflow-y: auto;
    scrollbar-width: thin;
    scrollbar-color: #bdc1c6 transparent;
  }
  .sidebar__list::-webkit-scrollbar { width: 6px; }
  .sidebar__list::-webkit-scrollbar-track { background: transparent; }
  .sidebar__list::-webkit-scrollbar-thumb { background-color: #bdc1c6; border-radius: 10px; }
  .sidebar__list::-webkit-scrollbar-thumb:hover { background-color: #80868b; }
  
  .chat-item {
    display: flex;
    padding: 15px 20px;
    border: none;
    background: transparent;
    width: 100%;
    text-align: left;
    cursor: pointer;
    border-bottom: 1px solid #f1f3f4;
    gap: 12px;
    align-items: center;
    color: #202124;
    content-visibility: auto;
    contain-intrinsic-size: 71px;
  }
  .chat-item:hover, .chat-item.active { background: #e8eaed; }
  .chat-item__avatar {
    width: 40px; height: 40px; border-radius: 50%; background: #dfe1e5; display: flex; align-items: center; justify-content: center; font-weight: bold; color: #202124;
  }
  .chat-item__info { flex: 1; overflow: hidden; }
  .chat-item__name { font-weight: 600; font-size: 15px; margin-bottom: 4px; }
  .chat-item__preview { font-size: 13px; color: #5f6368; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
  
  .chat-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: #ffffff;
  }
  .chat-main__header {
    padding: 20px;
    border-bottom: 1px solid #dfe1e5;
    background: #ffffff;
  }
  .chat-main__header h3 { margin: 0; font-size: 18px; color: #202124; }
  .chat-main__messages {
    flex: 1;
    overflow-y: auto;
    padding: 20px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .msg { max-width: 70%; display: flex; flex-direction: column; }
  .msg--in { align-self: flex-start; }
  .msg--in .msg__bubble { background: #f1f3f4; color: #202124; padding: 12px 16px; border-radius: 16px; border-bottom-left-radius: 4px; font-size: 15px; line-height: 1.4; }
  .msg--in .msg__time { font-size: 11px; color: #80868b; margin-top: 4px; margin-left: 4px; }
  .msg--out { align-self: flex-end; }
  .msg--out .msg__bubble { background: #202124; color: #ffffff; padding: 12px 16px; border-radius: 16px; border-bottom-right-radius: 4px; font-size: 15px; line-height: 1.4; }
  .msg--out .msg__time { font-size: 11px; color: #80868b; margin-top: 4px; margin-right: 4px; text-align: right; }
  .msg--sending { opacity: 0.6; }
  
  .chat-main__input {
    padding: 20px;
    border-top: 1px solid #dfe1e5;
    background: #ffffff;
  }
  .chat-main__empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #5f6368;
  }
  .outbox-form { display: flex; gap: 12px; }
  .outbox-form input { flex: 1; padding: 14px 20px; border-radius: 24px; border: 1px solid #dfe1e5; background: #f8f9fa; color: #202124; font-size: 15px; outline: none; }
  .outbox-form input:focus { border-color: #bdc1c6; }
  .btn-send { padding: 0 24px; border-radius: 24px; background: #202124; color: white; font-weight: 600; font-size: 15px; border: none; cursor: pointer; }
  .btn-send:hover { background: #3c4043; }
  .outbox-status { font-size: 13px; color: #5f6368; margin-top: 8px; text-align: center; }
</style>
