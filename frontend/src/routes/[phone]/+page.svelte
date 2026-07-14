<script lang="ts">
  import { page } from '$app/state';
  import { onMount } from 'svelte';
  import type { PageData } from './$types';
  import RemoteControl from '$lib/RemoteControl.svelte';
  import AdbTerminal from '$lib/AdbTerminal.svelte';
  import Settings from '$lib/Settings.svelte';
  import Logs from '$lib/Logs.svelte';
  import SuparnaPanel from '$lib/suparna/SuparnaPanel.svelte';
  import BrowserView from '$lib/BrowserView.svelte';
  import { detectSmsCodes } from '$lib/smsCodes';

  let { data }: { data: PageData } = $props();

  // The phone number from the URL
  let phone = $derived(page.params.phone);

  // Connection state
  let appState: 'setup' | 'waiting' | 'challenge' | 'connected' | 'error' = $state('setup');
  // Challenge variables
  let inputTime = $state(''); // User inputs "18:36" or "1836"
  let challengeTimeStr = $state(''); // Normalized to "1836"
  let encryptedSafe = $state('');
  let safeSalt = $state('');
  let safeIv = $state('');
  let timeRemaining = $state(0);
  let challengeRemaining = $state(30);
  let challengeClicks = $state(0);

  let sessionToken: string = $state((data as any).sessionToken || '');
  let vpcUrl: string = $state((data as any).savedVpcUrl || '');
  let certFingerprint: string = $state((data as any).certFingerprint || '');
  let smsList: any[] = $state([]);
  let contacts: Record<string, string> = $state({});
  let sidebarTab: 'chats' | 'contacts' | 'settings' | 'logs' | 'suparna' | 'browser' = $state('chats');
  let settingsSection: 'node' | 'recovery' | 'contacts' = $state('node');
  let suparnaSection: 'vpc' | 'models' | 'rules' | 'phone' = $state('vpc');
  let contactSearchQuery: string = $state('');
  let chatSearchQuery: string = $state('');
  let syncContacts: boolean = $state(true);
  let selectedSender: string | null = $state(null);
  let copiedPhone: string | null = $state(null);
  let copiedCode: string | null = $state(null);
  let copyResetTimer: ReturnType<typeof setTimeout> | null = null;
  let copyCodeTimer: ReturnType<typeof setTimeout> | null = null;

  // Logs archive (days live in left sidebar)
  let logDays: Array<{ day: string; bytes: number; lines: number; updated_at: string }> = $state([]);
  let selectedLogDay: string | null = $state(null);
  let logTotalBytes = $state(0);
  let logQuotaBytes = $state(1 << 30);
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
    // Prefer D1 directory token (APK re-pair) over stale browser cookie.
    const serverVpc = (data as any).savedVpcUrl;
    const serverToken = (data as any).sessionToken;
    if (serverVpc && serverToken) {
      vpcUrl = serverVpc;
      sessionToken = serverToken;
      try {
        const authData = JSON.stringify({ vpcUrl, sessionToken, certFingerprint });
        document.cookie = `gafam_auth_${phone}=${encodeURIComponent(authData)}; domain=${getRootDomain()}; path=/; max-age=31536000`;
      } catch {}
    } else {
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
    }

    if (vpcUrl && sessionToken) {
      appState = 'connected' | 'error';
      loadSms();
      pollInterval = setInterval(loadSms, 5000);
    } else {
      appState = 'setup';
      // Deep link from recovery SMS: https://{phone}.gafam.cloud/?t=1834
      const tParam = page.url.searchParams.get('t') || page.url.searchParams.get('time');
      if (tParam) {
        const digits = tParam.replace(/[^0-9]/g, '');
        if (digits.length === 4) {
          inputTime = `${digits.slice(0, 2)}:${digits.slice(2, 4)}`;
          challengeTimeStr = digits;
          startWaitingForTime();
        }
      }
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
    appState = 'waiting';
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
    appState = 'challenge';
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
    if (appState === 'challenge') {
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

  async function copyPhone(phoneNumber: string, event?: MouseEvent) {
    event?.stopPropagation();
    event?.preventDefault();
    try {
      await navigator.clipboard.writeText(phoneNumber);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = phoneNumber;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    copiedPhone = phoneNumber;
    if (copyResetTimer) clearTimeout(copyResetTimer);
    copyResetTimer = setTimeout(() => {
      if (copiedPhone === phoneNumber) copiedPhone = null;
    }, 1600);
  }

  function smsCodes(sms: { body?: string; codes?: string[] }): string[] {
    if (sms.codes?.length) return sms.codes;
    if (sms.body) return detectSmsCodes(sms.body);
    return [];
  }

  async function copyCode(code: string) {
    try {
      await navigator.clipboard.writeText(code);
    } catch {
      const ta = document.createElement('textarea');
      ta.value = code;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.select();
      document.execCommand('copy');
      document.body.removeChild(ta);
    }
    copiedCode = code;
    if (copyCodeTimer) clearTimeout(copyCodeTimer);
    copyCodeTimer = setTimeout(() => {
      if (copiedCode === code) copiedCode = null;
    }, 1600);
  }

  async function processChallenge() {
    statusMsg = 'Retrieving & Deciphering safe... (this will take a moment)';
    
    // We wait a tiny bit so the UI updates
    setTimeout(async () => {
      let lockedDownCorrectTime: boolean | undefined = undefined;
      try {
        const res = await fetch(`/api/directory?phone=${phone}&time=${challengeTimeStr}`);
        
        if (res.status === 429) {
           appState = 'error';
           try {
             const errData: any = await res.json();
             statusMsg = errData.error || 'Rate limit exceeded. Try again later.';
           } catch(e) {
             statusMsg = 'Rate limit exceeded. Try again later.';
           }
           return;
        }

        const result: any = await res.json();
        if (!result.success) {
           appState = 'error';
           statusMsg = 'Decryption failed (bad time or clicks).';
           return;
        }

        encryptedSafe = result.encrypted_safe;
        safeSalt = result.salt;
        safeIv = result.iv;
        lockedDownCorrectTime = result.locked_down_correct_time;

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
          
          appState = 'connected' | 'error';
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
        appState = 'setup';
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
      const peer = sms.sender;
      if (!groups[peer]) groups[peer] = [];
      const direction =
        sms.direction ||
        (sms.status === 'outbound' || sms.status === 'sent' ? 'outbound' : 'inbound');
      groups[peer].push({ ...sms, direction });
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

  let chatSenders = $derived.by(() => {
    const groups = conversations();
    const q = chatSearchQuery.trim().toLowerCase();
    let keys = Object.keys(groups);
    keys.sort((a, b) => {
      const ta = groups[a]?.[groups[a].length - 1]?.timestamp || 0;
      const tb = groups[b]?.[groups[b].length - 1]?.timestamp || 0;
      return tb - ta;
    });
    if (!q) return keys;
    return keys.filter((sender) => {
      const name = getContactName(sender).toLowerCase();
      const preview = (groups[sender]?.[groups[sender].length - 1]?.body || '').toLowerCase();
      return name.includes(q) || sender.toLowerCase().includes(q) || preview.includes(q);
    });
  });

  let filteredContacts = $derived(() => {
    const entries = Object.entries(contacts);
    if (!contactSearchQuery) return entries;
    const q = contactSearchQuery.toLowerCase();
    return entries.filter(([cPhone, cName]: [string, string]) => 
      cName.toLowerCase().includes(q) || cPhone.includes(q)
    );
  });

  async function loadContacts() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy/contacts?${proxyParams.toString()}`);
      if (res.ok) {
        const payload: any = await res.json();
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
        const payload: any = await res.json();
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
        appState = 'setup';
        sessionToken = '';
        vpcUrl = '';
        statusMsg = 'Session expired. Please reauthorize from your phone.';
      } else {
        const errorData: any = await res.json().catch(() => ({}));
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
        const payload: any = await res.json();
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

<div class="dashboard-wrapper">
  
    {#if appState === 'setup'}
      <div class="login-card">
        <h2 class="login-card__title">Authorization Required</h2>
        <p class="login-card__desc">Press <strong>"Authorize Web Login"</strong> on your phone and enter the challenge time below.</p>
        <form class="login-card__field" onsubmit={startChallengeFlow}>
          <label>Challenge Time</label>
          <input type="text" placeholder="e.g. 18:36" bind:value={inputTime} required />
          <button type="submit" class="login-card__btn">Next</button>
        </form>
        {#if statusMsg}<p class="login-card__status">{statusMsg}</p>{/if}
        

      </div>

    {:else if appState === 'waiting'}
      <div class="login-card">
        <h2 class="login-card__title">Safe Retrieved</h2>
        <p class="login-card__desc">Waiting for {challengeTimeStr.substring(0,2)}:{challengeTimeStr.substring(2,4)} to open the safe.</p>
        <div class="countdown-display">{timeRemaining}s</div>
        <div class="waiting-dots"><span></span><span></span><span></span></div>
      </div>

    {:else if appState === 'challenge'}
      <div class="login-card challenge-card">
        <h2 class="login-card__title">Challenge Active</h2>
        <p class="login-card__desc">Click the button below the exact number of times shown on your phone.</p>
        <div class="challenge-timer">Time left: {challengeRemaining}s</div>
        <button class="challenge-btn" onclick={registerClick}>IMPULSE</button>
        <div class="challenge-counter">Registered impulses: {challengeClicks}</div>
      </div>

    {:else}
      <div class="messenger-container is-connected {sidebarTab === 'logs' || (sidebarTab === 'suparna' && suparnaSection === 'vpc') ? 'is-logs' : ''}">
        <div class="messenger-ui">
          <aside class="sidebar">
          <div class="sidebar__header">
            <div class="sidebar__tabs">
              <div class="sidebar__tabs-main">
                <button class="tab {sidebarTab === 'chats' ? 'active' : ''}" onclick={() => sidebarTab = 'chats'}>Chats</button>
                <button class="tab {sidebarTab === 'contacts' ? 'active' : ''}" onclick={() => sidebarTab = 'contacts'}>Contacts</button>
                <button class="tab {sidebarTab === 'logs' ? 'active' : ''}" onclick={() => sidebarTab = 'logs'}>Logs</button>
              </div>
              <div class="sidebar__tabs-end">
                <button class="tab {sidebarTab === 'suparna' ? 'active' : ''}" onclick={() => sidebarTab = 'suparna'}>Suparna</button>
                <button class="tab {sidebarTab === 'browser' ? 'active' : ''}" onclick={() => sidebarTab = 'browser'}>Browser</button>
                <button
                  type="button"
                  class="tab tab--icon {sidebarTab === 'settings' ? 'active' : ''}"
                  title="Settings"
                  aria-label="Settings"
                  onclick={() => sidebarTab = 'settings'}
                >
                  <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
                    <path d="M12.22 2h-.44a2 2 0 0 0-2 2v.18a2 2 0 0 1-1 1.73l-.43.25a2 2 0 0 1-2 0l-.15-.08a2 2 0 0 0-2.73.73l-.22.38a2 2 0 0 0 .73 2.73l.15.1a2 2 0 0 1 1 1.72v.51a2 2 0 0 1-1 1.74l-.15.09a2 2 0 0 0-.73 2.73l.22.38a2 2 0 0 0 2.73.73l.15-.08a2 2 0 0 1 2 0l.43.25a2 2 0 0 1 1 1.73V20a2 2 0 0 0 2 2h.44a2 2 0 0 0 2-2v-.18a2 2 0 0 1 1-1.73l.43-.25a2 2 0 0 1 2 0l.15.08a2 2 0 0 0 2.73-.73l.22-.39a2 2 0 0 0-.73-2.73l-.15-.08a2 2 0 0 1-1-1.74v-.5a2 2 0 0 1 1-1.74l.15-.09a2 2 0 0 0 .73-2.73l-.22-.38a2 2 0 0 0-2.73-.73l-.15.08a2 2 0 0 1-2 0l-.43-.25a2 2 0 0 1-1-1.73V4a2 2 0 0 0-2-2z" />
                    <circle cx="12" cy="12" r="3" />
                  </svg>
                </button>
              </div>
            </div>
            <div class="sidebar__actions">
              {#if sidebarTab === 'chats'}
                <div class="contact-search">
                  <input type="search" placeholder="Search chats..." bind:value={chatSearchQuery} />
                </div>
              {/if}
              {#if sidebarTab === 'contacts'}
                <div class="contact-search">
                  <input type="search" placeholder="Search contacts..." bind:value={contactSearchQuery} />
                </div>
              {/if}
              {#if sidebarTab !== 'logs' && sidebarTab !== 'settings' && sidebarTab !== 'suparna'}
              <label class="toggle-sync" title="Sync Contacts with Android">
                <input type="checkbox" bind:checked={syncContacts} onchange={toggleContactSync} />
                <span>Sync Contacts</span>
              </label>
              {/if}
              {#if sidebarTab === 'logs' || (sidebarTab === 'suparna' && suparnaSection === 'vpc')}
                <div class="logs-quota-mini">
                  <span>{(logTotalBytes / 1024).toFixed(0)} KB / {(logQuotaBytes / (1024 * 1024)).toFixed(0)} MB</span>
                  <div class="quota-bar"><div class="quota-fill" style="width: {Math.min(100, (logTotalBytes / logQuotaBytes) * 100)}%"></div></div>
                </div>
              {/if}
            </div>
          </div>
          <div class="sidebar__list">
            {#if sidebarTab === 'logs' || (sidebarTab === 'suparna' && suparnaSection === 'vpc')}
              <div class="logs-archive-label">Phone activity archive</div>
              {#each logDays as d}
                <button
                  class="chat-item {selectedLogDay === d.day ? 'active' : ''}"
                  onclick={() => selectedLogDay = d.day}
                >
                  <div class="chat-item__avatar logs-day-avatar">{d.day.slice(8, 10)}</div>
                  <div class="chat-item__info">
                    <div class="chat-item__name">{new Date(d.day + 'T12:00:00Z').toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })}</div>
                    <div class="chat-item__preview">{d.lines} lines · {(d.bytes / 1024).toFixed(1)} KB</div>
                  </div>
                </button>
              {/each}
              {#if logDays.length === 0}
                <div class="logs-sidebar-hint">
                  <p>No days yet</p>
                  <p class="hint-sub">APK ships logs every few seconds after pairing.</p>
                </div>
              {/if}
            {:else if sidebarTab === 'chats'}
              {#each chatSenders as sender}
                <button class="chat-item {selectedSender === sender ? 'active' : ''}" onclick={() => selectedSender = sender}>
                  <div class="chat-item__avatar">{ getContactName(sender).charAt(0).toUpperCase() }</div>
                  <div class="chat-item__info">
                    <div class="chat-item__name">{getContactName(sender)}</div>
                    <div class="chat-item__preview">
                      {#if conversations()[sender]?.length > 0}
                        {conversations()[sender][conversations()[sender].length - 1].body.substring(0, 30)}...
                      {:else}
                        New conversation
                      {/if}
                    </div>
                  </div>
                </button>
              {/each}
              {#if chatSenders.length === 0}
                <div class="logs-sidebar-hint">
                  <p>{chatSearchQuery.trim() ? 'No matching chats' : 'No conversations yet'}</p>
                </div>
              {/if}
            {:else if sidebarTab === 'contacts'}
              {#each filteredContacts() as [cPhone, cName]}
                <div class="chat-item chat-item--contact {selectedSender === cPhone ? 'active' : ''}">
                  <button
                    type="button"
                    class="chat-item__open"
                    onclick={() => { selectedSender = cPhone; sidebarTab = 'chats'; }}
                  >
                    <div class="chat-item__avatar">{ cName.charAt(0).toUpperCase() }</div>
                    <div class="chat-item__info">
                      <div class="chat-item__name">{cName}</div>
                      <div class="chat-item__preview contact-phone">{cPhone}</div>
                    </div>
                  </button>
                  <button
                    type="button"
                    class="btn-copy {copiedPhone === cPhone ? 'is-copied' : ''}"
                    title="Copy number"
                    aria-label="Copy {cPhone}"
                    onclick={(e) => copyPhone(cPhone, e)}
                  >
                    {copiedPhone === cPhone ? 'Copied' : 'Copy'}
                  </button>
                </div>
              {/each}
            {:else if sidebarTab === 'settings'}
              <button
                type="button"
                class="chat-item settings-nav {settingsSection === 'node' ? 'active' : ''}"
                onclick={() => { settingsSection = 'node'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">V</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">VPS Node</div>
                  <div class="chat-item__preview">Status & updates</div>
                </div>
              </button>
              <button
                type="button"
                class="chat-item settings-nav {settingsSection === 'recovery' ? 'active' : ''}"
                onclick={() => { settingsSection = 'recovery'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">R</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">Recovery</div>
                  <div class="chat-item__preview">Emergency guardians</div>
                </div>
              </button>
              <button
                type="button"
                class="chat-item settings-nav {settingsSection === 'contacts' ? 'active' : ''}"
                onclick={() => { settingsSection = 'contacts'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">C</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">Contacts</div>
                  <div class="chat-item__preview">Sync from Android</div>
                </div>
              </button>
            {:else if sidebarTab === 'suparna'}
              <button
                type="button"
                class="chat-item settings-nav {suparnaSection === 'vpc' ? 'active' : ''}"
                onclick={() => { suparnaSection = 'vpc'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">V</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">VPC 1 RAM</div>
                  <div class="chat-item__preview">Wake & analyze logs</div>
                </div>
              </button>
              <button
                type="button"
                class="chat-item settings-nav {suparnaSection === 'models' ? 'active' : ''}"
                onclick={() => { suparnaSection = 'models'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">M</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">Models</div>
                  <div class="chat-item__preview">GGUF catalog</div>
                </div>
              </button>
              <button
                type="button"
                class="chat-item settings-nav {suparnaSection === 'rules' ? 'active' : ''}"
                onclick={() => { suparnaSection = 'rules'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">R</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">Rules</div>
                  <div class="chat-item__preview">Local preferences</div>
                </div>
              </button>
              <button
                type="button"
                class="chat-item settings-nav {suparnaSection === 'phone' ? 'active' : ''}"
                onclick={() => { suparnaSection = 'phone'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">P</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">Phone</div>
                  <div class="chat-item__preview">One-shot edge tester</div>
                </div>
              </button>
            {/if}
          </div>
        </aside>

        <main class="chat-main {sidebarTab === 'logs' || (sidebarTab === 'suparna' && suparnaSection === 'vpc') ? 'chat-main--logs' : ''}">
          {#if sidebarTab === 'settings'}
            <Settings
              {sessionToken}
              {vpcUrl}
              bind:section={settingsSection}
              bind:syncContacts
              onContactSyncChange={toggleContactSync}
            />
          {:else if sidebarTab === 'suparna'}
            <SuparnaPanel
              {sessionToken}
              {vpcUrl}
              bind:selectedDay={selectedLogDay}
              bind:days={logDays}
              bind:totalBytes={logTotalBytes}
              bind:quotaBytes={logQuotaBytes}
              bind:section={suparnaSection}
            />
          {:else if sidebarTab === 'logs'}
            <Logs
              {sessionToken}
              {vpcUrl}
              bind:selectedDay={selectedLogDay}
              bind:days={logDays}
              bind:totalBytes={logTotalBytes}
              bind:quotaBytes={logQuotaBytes}
            />
          {:else if sidebarTab === 'browser'}
            <BrowserView
              {sessionToken}
              {vpcUrl}
            />
          {:else if selectedSender}
            <div class="chat-main__header">
              <div class="chat-main__identity">
                <h3>{getContactName(selectedSender)}</h3>
                <div class="chat-main__phone">
                  <span class="chat-main__number" title={selectedSender}>{selectedSender}</span>
                  <button
                    type="button"
                    class="btn-copy {copiedPhone === selectedSender ? 'is-copied' : ''}"
                    title="Copy number"
                    aria-label="Copy {selectedSender}"
                    onclick={(e) => copyPhone(selectedSender!, e)}
                  >
                    {copiedPhone === selectedSender ? 'Copied' : 'Copy'}
                  </button>
                </div>
              </div>
            </div>
            <div class="chat-main__messages">
              {#each (conversations()[selectedSender] || []) as sms}
                {@const codes = sms.direction !== 'outbound' ? smsCodes(sms) : []}
                <div class="msg {sms.direction === 'outbound' ? 'msg--out' : 'msg--in'} {sms.status === 'sending' ? 'msg--sending' : ''}">
                  <div class="msg__bubble">{sms.body}</div>
                  {#if codes.length > 0}
                    <div class="msg__codes">
                      <span class="msg__codes-label">Code</span>
                      {#each codes as code}
                        <button
                          type="button"
                          class="msg__code-btn {copiedCode === code ? 'is-copied' : ''}"
                          onclick={() => copyCode(code)}
                        >
                          {copiedCode === code ? 'Copié' : code}
                        </button>
                      {/each}
                    </div>
                  {/if}
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
              {#if outboxStatus}<div class="outbox-status">{outboxStatus}</div>{/if}
            </div>
          {:else}
            <div class="chat-main__empty">
              <p>Select a chat to start messaging</p>
            </div>
          {/if}
        </main>
      </div>
      </div>
    {/if}

  {#if vpcUrl && sessionToken}
    <aside class="remote-panel">
      <div class="remote-phone-container">
        <RemoteControl {sessionToken} {vpcUrl} certFingerprint={certFingerprint} />
      </div>
      <div class="remote-adb-container">
        <AdbTerminal {sessionToken} {vpcUrl} certFingerprint={certFingerprint} />
      </div>
    </aside>
  {/if}

</div>

<style>
  :global(html, body) {
    height: 100%;
    overflow: hidden;
  }
  :global(body) {
    background: #f8f9fa;
    color: #202124;
    margin: 0;
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
  }
  .dashboard-wrapper {
    display: flex;
    justify-content: center;
    align-items: stretch;
    gap: 16px;
    width: 100%;
    max-width: 1600px;
    height: 100%;
    max-height: 100%;
    margin: 0 auto;
    padding: 12px 16px;
    box-sizing: border-box;
    min-height: 0;
    overflow: hidden;
  }

  .messenger-container {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: #ffffff;
    border: 1px solid #dfe1e5;
    border-radius: 12px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.08);
  }

  .messenger-container.is-connected {
    max-width: none;
  }
  .messenger-container.is-connected.is-logs {
    max-width: none;
  }
  .messenger-ui {
    display: grid;
    grid-template-columns: minmax(240px, 320px) minmax(0, 1fr);
    flex: 1;
    height: 100%;
    width: 100%;
    min-height: 0;
    overflow: hidden;
  }
  .logs-archive-label {
    padding: 10px 16px 6px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #80868b;
    font-weight: 600;
  }
  .logs-day-avatar {
    background: #202124 !important;
    color: #fff !important;
    font-size: 12px !important;
  }
  .logs-quota-mini {
    width: 100%;
    font-size: 11px;
    color: #5f6368;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .quota-bar {
    height: 3px;
    background: #e8eaed;
    border-radius: 2px;
    overflow: hidden;
  }
  .quota-fill {
    height: 100%;
    background: #202124;
  }
  .chat-main--logs {
    background: #ffffff !important;
  }
  .top-bar {
    width: 100%;
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 40px;
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
    margin: auto;
  }
  .login-card__title { margin: 0 0 10px; font-size: 20px; color: #202124; }
  .login-card__desc { color: #5f6368; font-size: 14px; margin-bottom: 24px; }
  .login-card__field label { display: block; text-align: left; margin-bottom: 8px; color: #5f6368; font-size: 12px; text-transform: uppercase; }
  .login-card__field input { width: 100%; box-sizing: border-box; padding: 12px; background: #f1f3f4; border: 1px solid #dfe1e5; color: #202124; border-radius: 6px; margin-bottom: 16px; }
  .login-card__btn { width: 100%; padding: 12px; background: #202124; color: white; font-weight: bold; border: none; border-radius: 6px; cursor: pointer; }
  .login-card__status { color: #d93025; margin-top: 16px; font-size: 13px; }
  .login-card__recovery { margin-top: 32px; padding-top: 24px; border-top: 1px solid #dfe1e5; text-align: left; }
  .login-card__recovery h4 { margin: 0 0 8px; font-size: 14px; color: #202124; }
  .login-card__recovery p { margin: 0; font-size: 13px; color: #5f6368; line-height: 1.4; }
  
  .challenge-btn {
    width: 150px; height: 150px; border-radius: 50%; background: #f1f3f4; color: #202124; font-size: 18px; border: 1px solid #dfe1e5; cursor: pointer; margin: 20px auto; display: block; box-shadow: 0 4px 10px rgba(0,0,0,0.05);
  }
  .challenge-btn:active { background: #e8eaed; transform: scale(0.95); }
  .challenge-counter { font-size: 18px; font-weight: 500; margin-top: 20px; }
  
  .sidebar {
    min-width: 0;
    min-height: 0;
    height: 100%;
    background: #ffffff;
    border-right: 1px solid #dfe1e5;
    display: flex;
    flex-direction: column;
    overflow: hidden;
  }
  .sidebar__header {
    padding: 12px 14px;
    border-bottom: 1px solid #dfe1e5;
    display: flex;
    flex-direction: column;
    gap: 10px;
    flex-shrink: 0;
  }
  .sidebar__tabs {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
  }
  .sidebar__tabs-main,
  .sidebar__tabs-end {
    display: flex;
    gap: 8px;
  }
  .sidebar__tabs-main .tab {
    flex: 1;
  }
  .sidebar__tabs-end .tab {
    flex: 0 0 auto;
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
  .tab--icon {
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 8px 10px;
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
  
  .remote-panel {
    background: #ffffff;
    border: 1px solid #dfe1e5;
    border-radius: 12px;
    box-shadow: 0 8px 24px rgba(0,0,0,0.08);
    display: flex;
    flex-direction: column;
    width: min(380px, 34vw);
    min-width: 280px;
    max-width: 420px;
    min-height: 0;
    height: 100%;
    overflow: hidden;
    flex-shrink: 0;
  }
  
  .remote-phone-container {
    flex: 1;
    min-height: 0;
    border-bottom: 1px solid #dfe1e5;
    overflow: hidden;
    position: relative;
    background: #f5f5f5;
  }

  .remote-adb-container {
    height: min(180px, 28%);
    flex: none;
    overflow: hidden;
    background: #ffffff;
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
    flex: 1 1 auto;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    overscroll-behavior: contain;
    scrollbar-width: thin;
    scrollbar-color: #bdc1c6 transparent;
  }
  .sidebar__list::-webkit-scrollbar { width: 6px; }
  .sidebar__list::-webkit-scrollbar-track { background: transparent; }
  .sidebar__list::-webkit-scrollbar-thumb { background-color: #bdc1c6; border-radius: 10px; }
  .sidebar__list::-webkit-scrollbar-thumb:hover { background-color: #80868b; }
  
  .chat-item {
    display: flex;
    padding: 0;
    border: none;
    background: transparent;
    width: 100%;
    text-align: left;
    cursor: pointer;
    border-bottom: 1px solid #f1f3f4;
    gap: 8px;
    align-items: center;
    color: #202124;
  }
  .chat-item:hover, .chat-item.active { background: #e8eaed; }
  .chat-item__open {
    display: flex;
    flex: 1;
    min-width: 0;
    align-items: center;
    gap: 12px;
    padding: 15px 8px 15px 20px;
    border: none;
    background: transparent;
    text-align: left;
    cursor: pointer;
    color: inherit;
  }
  .chat-item--contact .btn-copy {
    flex-shrink: 0;
    margin-right: 12px;
  }
  button.chat-item {
    padding: 15px 20px;
  }
  .chat-item__avatar {
    width: 40px; height: 40px; border-radius: 50%; background: #dfe1e5; display: flex; align-items: center; justify-content: center; font-weight: bold; color: #202124; flex-shrink: 0;
  }
  .chat-item__info { flex: 1; overflow: hidden; }
  .chat-item__name { font-weight: 600; font-size: 15px; margin-bottom: 4px; }
  .chat-item__preview { font-size: 13px; color: #5f6368; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }

  .settings-nav__icon {
    font-size: 13px !important;
    font-weight: 700;
    background: #202124 !important;
    color: #ffffff !important;
  }
  
  .chat-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    background: #ffffff;
    min-width: 0;
    min-height: 0;
    height: 100%;
    overflow: hidden;
  }
  .logs-sidebar-hint {
    padding: 24px 16px;
    color: #5f6368;
    font-size: 13px;
  }
  .logs-sidebar-hint p { margin: 0; }
  .logs-sidebar-hint .hint-sub {
    color: #80868b;
    font-size: 12px;
    margin-top: 6px;
  }
  .chat-main__header {
    padding: 12px 16px;
    border-bottom: 1px solid #dfe1e5;
    background: #ffffff;
    flex-shrink: 0;
  }
  .chat-main__identity {
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
    flex-wrap: wrap;
  }
  .chat-main__header h3 {
    margin: 0;
    font-size: 18px;
    color: #202124;
    flex-shrink: 0;
  }
  .chat-main__phone {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    margin-left: auto;
  }
  .chat-main__number {
    font-size: 13px;
    font-weight: 500;
    color: #5f6368;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .btn-copy {
    flex-shrink: 0;
    padding: 5px 10px;
    border-radius: 6px;
    border: 1px solid #dfe1e5;
    background: #ffffff;
    color: #202124;
    font-size: 12px;
    font-weight: 600;
    cursor: pointer;
    line-height: 1.2;
  }
  .btn-copy:hover {
    background: #f1f3f4;
    border-color: #bdc1c6;
  }
  .btn-copy.is-copied {
    background: #202124;
    border-color: #202124;
    color: #ffffff;
  }
  .chat-main__messages {
    flex: 1 1 auto;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    overscroll-behavior: contain;
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
  .msg { max-width: 70%; display: flex; flex-direction: column; }
  .msg--in { align-self: flex-start; }
  .msg--in .msg__bubble { background: #f1f3f4; color: #202124; padding: 12px 16px; border-radius: 16px; border-bottom-left-radius: 4px; font-size: 15px; line-height: 1.4; }
  .msg--in .msg__time { font-size: 11px; color: #80868b; margin-top: 4px; margin-left: 4px; }
  .msg--out { align-self: flex-end; }
  .msg--out .msg__bubble { background: #202124; color: #ffffff; padding: 12px 16px; border-radius: 16px; border-bottom-right-radius: 4px; font-size: 15px; line-height: 1.4; }
  .msg--out .msg__time { font-size: 11px; color: #80868b; margin-top: 4px; margin-right: 4px; text-align: right; }
  .msg--sending { opacity: 0.6; }
  .msg__codes {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    margin-top: 6px;
  }
  .msg--out .msg__codes {
    justify-content: flex-end;
  }
  .msg__codes-label {
    font-size: 10px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: #80868b;
  }
  .msg__code-btn {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 13px;
    font-weight: 600;
    padding: 6px 12px;
    border-radius: 8px;
    border: 1px solid #202124;
    background: #202124;
    color: #ffffff;
    cursor: pointer;
  }
  .msg__code-btn.is-copied {
    background: #ffffff;
    color: #202124;
  }
  .msg__code-btn:hover {
    background: #3c4043;
    border-color: #3c4043;
  }
  .msg__code-btn.is-copied:hover {
    background: #f1f3f4;
  }
  
  .chat-main__input {
    padding: 12px 16px;
    border-top: 1px solid #dfe1e5;
    background: #ffffff;
    flex: 0 0 auto;
    position: relative;
    z-index: 2;
  }
  .chat-main__empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #5f6368;
  }
  .outbox-form { display: flex; gap: 10px; align-items: stretch; }
  .outbox-form input { flex: 1; min-width: 0; padding: 12px 16px; border-radius: 24px; border: 1px solid #dfe1e5; background: #f8f9fa; color: #202124; font-size: 15px; outline: none; }
  .outbox-form input:focus { border-color: #bdc1c6; }
  .btn-send { padding: 0 20px; border-radius: 24px; background: #202124; color: white; font-weight: 600; font-size: 15px; border: none; cursor: pointer; white-space: nowrap; }
  .btn-send:hover { background: #3c4043; }
  .outbox-status { font-size: 13px; color: #5f6368; margin-top: 8px; text-align: center; }

  @media (max-width: 1100px) {
    .dashboard-wrapper {
      gap: 10px;
      padding: 8px 10px;
    }
    .messenger-ui {
      grid-template-columns: minmax(200px, 260px) minmax(0, 1fr);
    }
    .remote-panel {
      width: min(300px, 32vw);
      min-width: 240px;
    }
  }

  @media (max-width: 860px) {
    .dashboard-wrapper {
      flex-direction: column;
      overflow-y: auto;
      overflow-x: hidden;
      height: 100%;
    }
    .messenger-container {
      min-height: min(70dvh, 640px);
      height: min(70dvh, 640px);
      flex: none;
    }
    .remote-panel {
      width: 100%;
      max-width: none;
      min-width: 0;
      height: 360px;
      flex: none;
    }
    .messenger-ui {
      grid-template-columns: minmax(160px, 42%) minmax(0, 1fr);
    }
    .chat-main__input {
      padding: 10px 12px;
    }
    .outbox-form input {
      padding: 10px 14px;
      font-size: 14px;
    }
  }

  @media (max-width: 560px) {
    .messenger-ui {
      grid-template-columns: 1fr;
      grid-template-rows: minmax(180px, 38%) minmax(0, 1fr);
    }
    .sidebar {
      border-right: none;
      border-bottom: 1px solid #dfe1e5;
    }
    .sidebar__tabs {
      flex-wrap: wrap;
    }
    .tab {
      font-size: 12px;
      padding: 6px;
    }
    .chat-main__identity {
      gap: 8px;
    }
    .chat-main__phone {
      margin-left: 0;
      width: 100%;
    }
  }
</style>
