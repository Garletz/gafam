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
  import SandboxView from '$lib/SandboxView.svelte';
  import QuestBoard from '$lib/QuestBoard.svelte';
  import VaultView from '$lib/VaultView.svelte';
  import FederationView from '$lib/FederationView.svelte';
  import SmsComposeAids from '$lib/SmsComposeAids.svelte';
  import SmsManagerView from '$lib/SmsManagerView.svelte';
  import ContactProfileModal, { type ContactFull } from '$lib/ContactProfileModal.svelte';
  import { detectSmsCodes } from '$lib/smsCodes';
  import { blurContacts } from '$lib/privacy';

  let { data }: { data: PageData } = $props();

  // The phone number from the URL
  let phone = $derived(page.params.phone);

  let contactsFullList: ContactFull[] = $state([]);
  let selectedContactForProfile: ContactFull | null = $state(null);

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
  let mmsList: any[] = $state([]);
  let contacts: Record<string, string> = $state({});
  let sidebarTab: 'chats' | 'settings' | 'logs' | 'suparna' | 'browser' | 'sandbox' | 'quests' = $state('chats');
  let settingsSection: 'node' | 'recovery' | 'contacts' | 'sms_manager' = $state('node');
  let suparnaSection: 'vpc' | 'models' | 'rules' | 'phone' | 'providers' = $state('vpc');
  let contactSearchQuery: string = $state('');
  let chatSearchQuery: string = $state('');
  let syncContacts: boolean = $state(true);
  let syncContactsLoading: boolean = $state(true);
  let selectedSender: string | null = $state(null);
  let chatSearch = $state('');
  let messagesEl: HTMLDivElement | undefined = $state();

  let scrollTick = $state(0);

  $effect(() => {
    // Auto-scroll to bottom when new messages arrive or switching conversations
    if (selectedSender && messagesEl) {
      const msgs = conversations()[selectedSender];
      if (msgs) scrollTick; // reactive dependency: re-triggers on any conversation change
      requestAnimationFrame(() => {
        if (messagesEl) {
          messagesEl.scrollTop = messagesEl.scrollHeight;
        }
      });
    }
  });
  let copiedPhone: string | null = $state(null);
  let copiedCode: string | null = $state(null);
  let copyResetTimer: ReturnType<typeof setTimeout> | null = null;
  let copyCodeTimer: ReturnType<typeof setTimeout> | null = null;

  /** iOS-style long-press delete on a chat row */
  let chatActionPeer: string | null = $state(null);
  let chatDeleting = $state(false);
  let longPressTimer: ReturnType<typeof setTimeout> | null = null;
  let longPressFired = false;
  /** Peers hidden after delete (covers VPC lag / outdated image until real DELETE lands) */
  let hiddenChatPeers = $state<Record<string, true>>({});

  // Logs archive (days live in left sidebar)
  let logDays: Array<{ day: string; bytes: number; lines: number; updated_at: string }> = $state([]);
  let selectedLogDay: string | null = $state(null);
  let logTotalBytes = $state(0);
  let logQuotaBytes = $state(1 << 30);
  let pollInterval: ReturnType<typeof setInterval>;
  let countdownInterval: ReturnType<typeof setInterval>;
  let draftPollInterval: ReturnType<typeof setInterval>;
  let statusMsg = $state('');
  
  let outboxRecipient = $state('');
  let outboxBody = $state('');
  let outboxStatus = $state('');
  let outboxTextarea: HTMLTextAreaElement | null = $state(null);
  let draftTimer: ReturnType<typeof setTimeout> | null = null;
  let lastDraftPeer: string | null = $state(null);
  let lastDraftSync: number = $state(0);
  let lastDraftSaved: number = $state(0);
  let lastDraftVersion = $state('');
  let draftCache: Record<string, string> = {};
  
  // Profile menu state
  let isProfileMenuOpen = $state(false);

  // Navigation: Chat (left), Organic Tools (center), Settings (right)
  type SidebarTab = 'chats' | 'settings' | 'logs' | 'suparna' | 'browser' | 'sandbox' | 'quests' | 'vault' | 'federation';
  let showContactsInChat = $state(false);
  let toolsWiggle = $state(false);
  let appsMenuOpen = $state(false);

  const toolsMenuItems: Array<{ id: SidebarTab; label: string; hint: string; icon: string }> = [
    { id: 'quests', label: 'Quests', hint: 'Mission board', icon: 'quests' },
    { id: 'suparna', label: 'Suparna', hint: 'Edge & VPC AI', icon: 'suparna' },
    { id: 'browser', label: 'Browser', hint: 'Vātāyana', icon: 'browser' },
    { id: 'sandbox', label: 'Sandbox', hint: 'Yantraśālā', icon: 'sandbox' },
    { id: 'vault', label: 'Vault', hint: 'Research memory', icon: 'vault' },
    { id: 'federation', label: 'Federation', hint: 'VPC↔VPC links & inbox', icon: 'links' },
    { id: 'logs', label: 'Logs', hint: 'Phone activity', icon: 'logs' },
  ];
  const isToolTab = $derived(['quests', 'suparna', 'browser', 'sandbox', 'vault', 'federation', 'logs'].includes(sidebarTab));
  const toolsMenuActive = $derived(isToolTab);

  function selectSidebarTab(tab: SidebarTab) {
    sidebarTab = tab;
    appsMenuOpen = false;
    if (tab !== 'chats') showContactsInChat = false;
  }

  function toggleContactsInChat() {
    showContactsInChat = !showContactsInChat;
  }

  function clickTools() {
    toolsWiggle = true;
    setTimeout(() => { toolsWiggle = false; }, 300);
    sidebarTab = 'quests';
  }

  $effect(() => {
    if (!appsMenuOpen) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') appsMenuOpen = false;
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  });

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
      // Poll drafts every 1.5s (only if user isn't currently editing)
      const draftPoll = setInterval(() => {
        if (selectedSender && !draftTimer) loadDraft(selectedSender);
      }, 1500);
      draftPollInterval = draftPoll;
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
      if (draftPollInterval) clearInterval(draftPollInterval);
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
      const list = groups[peer];
      // Drop near-duplicates already in DB (same body+direction within 3 min)
      const isDup = list.some(
        (m) =>
          m.body === sms.body &&
          m.direction === direction &&
          Math.abs((m.timestamp || 0) - (sms.timestamp || 0)) < 180000
      );
      if (!isDup) list.push({ ...sms, direction });
    }

    // Merge carrier MMS into the same conversation threads
    for (const mms of mmsList) {
      const peer = mms.sender;
      if (!groups[peer]) groups[peer] = [];
      const direction = mms.direction || 'inbound';
      const textParts = (mms.parts || []).filter((p: any) => p.text).map((p: any) => p.text);
      const hasMedia = (mms.parts || []).some((p: any) => p.has_media);
      const list = groups[peer];
      const isDup = list.some(
        (m) => m.is_mms && m.id === mms.id
      );
      if (!isDup) {
        list.push({
          ...mms,
          direction,
          is_mms: true,
          body: textParts.join('\n') || (hasMedia ? '📷 Photo' : '(MMS)'),
        });
      }
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
    let keys = Object.keys(groups).filter((s) => !hiddenChatPeers[s]);
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
            const list: ContactFull[] = JSON.parse(plaintext);
            contactsFullList = list;
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
          contactsFullList = payload;
          const map: Record<string, string> = {};
          for (const c of payload) {
            map[c.phone_number] = c.display_name;
          }
          contacts = map;
        }
      }
    } catch(e) {}
  }

  function openContactProfileByPhone(targetPhone: string) {
    const found = contactsFullList.find(c => c.phone_number === targetPhone);
    if (found) {
      selectedContactForProfile = { ...found };
    } else {
      selectedContactForProfile = {
        phone_number: targetPhone,
        display_name: contacts[targetPhone] || targetPhone,
        skills: [],
        languages: ['fr']
      };
    }
  }

  function handleContactSaved(updated: ContactFull) {
    const idx = contactsFullList.findIndex(c => c.phone_number === updated.phone_number);
    if (idx >= 0) {
      contactsFullList[idx] = updated;
    } else {
      contactsFullList.push(updated);
    }
    contacts[updated.phone_number] = updated.display_name;
    contacts = { ...contacts };
    selectedContactForProfile = updated;
  }

  function exportContactsCSV() {
    const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken });
    window.open(`/api/proxy/contacts/csv?${proxyParams.toString()}`, '_blank');
  }

  async function handleCSVUpload(e: Event) {
    const target = e.target as HTMLInputElement;
    if (!target.files || target.files.length === 0) return;
    const file = target.files[0];
    const text = await file.text();
    const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken });
    const res = await fetch(`/api/proxy/contacts/csv?${proxyParams.toString()}`, {
      method: 'POST',
      body: text
    });
    if (res.ok) {
      await loadContacts();
    }
    target.value = '';
  }

  function openChatActions(peer: string) {
    chatActionPeer = peer;
    try {
      navigator.vibrate?.(12);
    } catch {
      /* ignore */
    }
  }

  function closeChatActions() {
    chatActionPeer = null;
  }

  function onChatPointerDown(peer: string) {
    longPressFired = false;
    if (longPressTimer) clearTimeout(longPressTimer);
    longPressTimer = setTimeout(() => {
      longPressFired = true;
      openChatActions(peer);
    }, 480);
  }

  function onChatPointerEnd() {
    if (longPressTimer) {
      clearTimeout(longPressTimer);
      longPressTimer = null;
    }
  }

  function selectChat(peer: string) {
    if (longPressFired) {
      longPressFired = false;
      return;
    }
    selectedSender = peer;
  }

  async function deleteConversation(peer: string) {
    if (!vpcUrl || !sessionToken || chatDeleting) return;
    chatDeleting = true;
    // Armé sur la ligne = confirmation — pas de modale
    hiddenChatPeers = { ...hiddenChatPeers, [peer]: true };
    smsList = smsList.filter((s) => s.sender !== peer);
    optimisticSms = optimisticSms.filter((s) => s.sender !== peer);
    if (selectedSender === peer) selectedSender = null;
    closeChatActions();
    try {
      const params = new URLSearchParams({
        vpcUrl,
        token: sessionToken,
        peer
      });
      const res = await fetch(`/api/proxy/sms?${params}`, { method: 'DELETE' });
      const data = await res.json().catch(() => ({}));
      if (!res.ok && res.status !== 404) {
        statusMsg = data.error || `Delete failed (${res.status})`;
      } else if (res.status === 404) {
        statusMsg = 'Hidden locally — Update VPC for permanent delete.';
      } else {
        statusMsg = '';
      }
    } catch (e: any) {
      statusMsg = e?.message || 'Delete failed';
    } finally {
      chatDeleting = false;
    }
  }

  async function loadSms() {
    loadContacts();
    loadMms();
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy?${proxyParams.toString()}`);
      if (res.ok) {
        const payload: any = await res.json();
        if (payload.error) {
           statusMsg = 'VPC returned an error: ' + payload.error;
        } else         if (payload.encrypted_data && payload.iv) {
           try {
             const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
              smsList = JSON.parse(plaintext);
              scrollTick++;
              if (appState === 'connected') statusMsg = '';
           } catch (decErr: any) {
             statusMsg = 'Decryption failed: ' + decErr.message;
           }
        } else {
            smsList = payload;
            scrollTick++;
            if (appState === 'connected') statusMsg = '';
        }
      } else if (res.status === 403) {
        if (pollInterval) clearInterval(pollInterval);
        if (draftPollInterval) clearInterval(draftPollInterval);
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

  async function loadMms() {
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy/mms?${proxyParams.toString()}`);
      if (res.ok) {
        const payload: any = await res.json();
        if (payload.encrypted_data && payload.iv) {
          try {
            const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
            mmsList = JSON.parse(plaintext);
          } catch (e) {
            console.error("Failed to decrypt MMS", e);
          }
        } else if (Array.isArray(payload)) {
          mmsList = payload;
        }
      }
    } catch (e) {}
  }

  /** Cache of decrypted MMS media part URLs (blob:) keyed by part id. */
  let mmsMediaUrls: Record<number, string> = $state({});

  async function loadMmsMediaPart(partId: number) {
    if (mmsMediaUrls[partId] || !vpcUrl || !sessionToken) return;
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, id: String(partId) });
      const res = await fetch(`/api/proxy/mms-media?${proxyParams.toString()}`);
      if (!res.ok) return;
      const payload: any = await res.json();
      if (!payload.encrypted_data || !payload.iv) return;
      const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
      const part = JSON.parse(plaintext);
      if (!part.data_base64) return;
      const bin = atob(part.data_base64);
      const bytes = new Uint8Array(bin.length);
      for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
      const blob = new Blob([bytes], { type: part.content_type || 'application/octet-stream' });
      mmsMediaUrls = { ...mmsMediaUrls, [partId]: URL.createObjectURL(blob) };
    } catch (e) {}
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
        outboxBody = body;
        optimisticSms = optimisticSms.filter(m => m !== optMsg);
      }
    } catch (err: any) {
      outboxStatus = 'Error: ' + err.message;
      outboxBody = body;
      optimisticSms = optimisticSms.filter(m => m !== optMsg);
    }
  }

  // ── Draft sync (cross-device typing) ──

  async function saveDraft(peer: string, body: string) {
    if (!vpcUrl || !sessionToken || !peer) return;
    try {
      const plaintext = JSON.stringify({ peer, body });
      const encryptedPayload = await encryptAESGCM(plaintext, sessionToken);
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint });
      const res = await fetch(`/api/proxy/draft?${proxyParams.toString()}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(encryptedPayload)
      });
      if (res.ok) {
        const data = await res.json();
        console.log('[draft] saved OK:', data.updated_at);
        if (data.updated_at) lastDraftVersion = data.updated_at;
        lastDraftSaved = Date.now();
        draftCache[peer] = body;
      }
    } catch (_) {}
  }

  async function loadDraft(peer: string) {
    if (!vpcUrl || !sessionToken || !peer) return;
    try {
      const proxyParams = new URLSearchParams({ vpcUrl, token: sessionToken, certFingerprint, peer });
      const res = await fetch(`/api/proxy/draft?${proxyParams.toString()}`);
      if (res.ok) {
        const data = await res.json();
        console.log('[draft] loaded:', data.body?.substring(0,20), 'v:', data.updated_at, 'local:', lastDraftVersion);
        if (!data.updated_at || data.updated_at === lastDraftVersion) return;
        lastDraftVersion = data.updated_at;
        outboxBody = data.body ?? '';
        lastDraftSync = Date.now();
      }
    } catch (_) {}
  }

  function triggerDraftSave(body: string) {
    if (!vpcUrl || !sessionToken || !selectedSender) return;
    if (draftTimer) clearTimeout(draftTimer);
    draftTimer = setTimeout(async () => {
      await saveDraft(selectedSender!, body);
      draftTimer = null;
    }, 300);
  }

  $effect(() => {
    // When switching conversations, load draft for the new peer
    const peer = selectedSender;
    if (!peer) return;
    if (peer === lastDraftPeer) return;
    if (lastDraftPeer) {
      draftCache[lastDraftPeer] = outboxBody;
    }
    lastDraftPeer = peer;
    lastDraftVersion = '';
    outboxBody = '';
    loadDraft(peer);
  });

  $effect(() => {
    // Track outboxBody changes for draft save
    const body = outboxBody;
    const peer = selectedSender;
    if (!vpcUrl || !sessionToken || !peer) return;
    if (draftTimer) clearTimeout(draftTimer);
    draftTimer = setTimeout(async () => {
      await saveDraft(peer, body);
      draftTimer = null;
    }, 300);
  });

  async function loadSettings() {
    try {
      syncContactsLoading = true;
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
    } catch (e) {} finally {
      syncContactsLoading = false;
    }
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
      <div class="messenger-container is-connected {isToolTab ? 'is-logs' : ''}">
        <div class="messenger-ui">
          <aside class="sidebar">
          <div class="sidebar__header">
            <div class="sidebar__tabs">
              <div class="sidebar__tabs-main">
                <button
                  type="button"
                  class="tab {sidebarTab === 'chats' ? 'active' : ''}"
                  onclick={() => selectSidebarTab('chats')}
                >Chats</button>
              </div>
              <div class="sidebar__tabs-center">
                <button
                  type="button"
                  class="tools-trigger"
                  class:is-open={sidebarTab === 'tools'}
                  class:is-active={toolsMenuActive}
                  class:wiggle={toolsWiggle}
                  title="Organic Tools"
                  aria-label="Organic Tools"
                  onclick={clickTools}
                >
                  <span class="tools-grid" aria-hidden="true">
                    {#each Array(9) as _, i (i)}
                      <span class="tools-dot" class:alive={toolsMenuActive}></span>
                    {/each}
                  </span>
                </button>
              </div>
              <div class="sidebar__tabs-end">
                <button
                  type="button"
                  class="tab {sidebarTab === 'settings' ? 'active' : ''}"
                  onclick={() => selectSidebarTab('settings')}
                >Settings</button>
              </div>
            </div>
            <div class="sidebar__actions">
              {#if sidebarTab === 'chats' && !showContactsInChat}
                <div class="contact-search">
                  <input type="search" placeholder="Search chats..." bind:value={chatSearchQuery} />
                </div>
              {/if}
              {#if sidebarTab === 'chats' && showContactsInChat}
                <div class="contact-search">
                  <input type="search" placeholder="Search contacts..." bind:value={contactSearchQuery} />
                </div>
                <div class="contact-csv-actions">
                  <button type="button" class="btn-csv-tool" onclick={exportContactsCSV} title="Exporter les contacts en CSV">
                    ↓ CSV
                  </button>
                  <label class="btn-csv-tool" title="Importer des contacts depuis un CSV">
                    ↑ CSV
                    <input type="file" accept=".csv" class="hidden-file-input" onchange={handleCSVUpload} />
                  </label>
                </div>
              {/if}
              {#if sidebarTab === 'chats'}
              <div class="chat-sub-actions">
                <label class="toggle-sync" title="Sync Contacts with Android">
                  <input type="checkbox" bind:checked={syncContacts} onchange={toggleContactSync} disabled={syncContactsLoading} />
                  <span>Sync{#if syncContactsLoading}…{/if}</span>
                </label>
                <button
                  type="button"
                  class="contacts-toggle"
                  class:active={showContactsInChat}
                  onclick={toggleContactsInChat}
                  title="Show contacts"
                >
                  {showContactsInChat ? '← Back to chats' : 'Contacts'}
                </button>
              </div>
              {/if}
              {#if sidebarTab === 'logs'}
                <div class="logs-quota-mini">
                  <span>{(logTotalBytes / 1024).toFixed(0)} KB / {(logQuotaBytes / (1024 * 1024)).toFixed(0)} MB</span>
                  <div class="quota-bar"><div class="quota-fill" style="width: {Math.min(100, (logTotalBytes / logQuotaBytes) * 100)}%"></div></div>
                </div>
              {/if}
            </div>
          </div>
          <div class="sidebar__list">
            {#if sidebarTab === 'chats' && !showContactsInChat}
              {#each chatSenders as sender}
                {#if chatActionPeer === sender}
                  <div class="chat-item chat-item--armed" role="group" aria-label="Delete conversation">
                    <div class="chat-item__arm-meta">
                      <span class="chat-item__arm-name" class:blurred={$blurContacts}>{getContactName(sender)}</span>
                      <span class="chat-item__arm-hint">Delete this chat?</span>
                    </div>
                    <button
                      type="button"
                      class="chat-item__arm-del"
                      disabled={chatDeleting}
                      onclick={() => deleteConversation(sender)}
                    >
                      {chatDeleting ? '…' : 'Delete'}
                    </button>
                    <button
                      type="button"
                      class="chat-item__arm-cancel"
                      onclick={closeChatActions}
                    >
                      Cancel
                    </button>
                  </div>
                {:else}
                  <button
                    type="button"
                    class="chat-item {selectedSender === sender ? 'active' : ''}"
                    onclick={() => selectChat(sender)}
                    onpointerdown={() => onChatPointerDown(sender)}
                    onpointerup={onChatPointerEnd}
                    onpointerleave={onChatPointerEnd}
                    onpointercancel={onChatPointerEnd}
                    oncontextmenu={(e) => {
                      e.preventDefault();
                      openChatActions(sender);
                    }}
                  >
                    <div class="chat-item__avatar" class:blurred={$blurContacts}>{ getContactName(sender).charAt(0).toUpperCase() }</div>
                    <div class="chat-item__info">
                      <div class="chat-item__name" class:blurred={$blurContacts}>{getContactName(sender)}</div>
                      <div class="chat-item__preview">
                        {#if conversations()[sender]?.length > 0}
                          {conversations()[sender][conversations()[sender].length - 1].body.substring(0, 30)}...
                        {:else}
                          New conversation
                        {/if}
                      </div>
                    </div>
                  </button>
                {/if}
              {/each}
              {#if chatSenders.length === 0}
                <div class="logs-sidebar-hint">
                  <p>{chatSearchQuery.trim() ? 'No matching chats' : 'No conversations yet'}</p>
                </div>
              {/if}
            {:else if sidebarTab === 'chats' && showContactsInChat}
              {#each filteredContacts() as [cPhone, cName]}
                <div class="chat-item chat-item--contact {selectedSender === cPhone ? 'active' : ''}">
                  <button
                    type="button"
                    class="chat-item__open"
                    onclick={() => openContactProfileByPhone(cPhone)}
                    title="Voir et éditer le profil"
                  >
                    <div class="chat-item__avatar" class:blurred={$blurContacts}>{ cName.charAt(0).toUpperCase() }</div>
                    <div class="chat-item__info">
                      <div class="chat-item__name" class:blurred={$blurContacts}>{cName}</div>
                      <div class="chat-item__preview contact-phone">{cPhone}</div>
                    </div>
                  </button>
                  <div class="chat-item__actions-group">
                    <button
                      type="button"
                      class="btn-contact-chat"
                      title="Ouvrir la conversation"
                      aria-label="Discuter avec {cName}"
                      onclick={() => { selectedSender = cPhone; showContactsInChat = false; }}
                    >
                      <svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2">
                        <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z"/>
                      </svg>
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
                </div>
              {/each}
            {:else if isToolTab}
              <div class="logs-archive-label">Organic Tools</div>
              <div class="logs-archive-sub">Karaka · Organs</div>
              {#each toolsMenuItems as item}
                <button class="chat-item settings-nav {sidebarTab === item.id ? 'active' : ''}" onclick={() => selectSidebarTab(item.id)}>
                  <div class="chat-item__avatar settings-nav__icon">
                    {#if item.icon === 'quests'}
                      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><path d="M4 6h16M4 12h10M4 18h14"/><circle cx="18" cy="12" r="2"/></svg>
                    {:else if item.icon === 'browser'}
                      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>
                    {:else if item.icon === 'sandbox'}
                      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M9 3v18M15 3v18M3 9h18M3 15h18"/></svg>
                    {:else if item.icon === 'suparna'}
                      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><path d="M12 3v3M12 18v3M3 12h3M18 12h3"/><circle cx="12" cy="12" r="4.5"/><path d="M7.5 7.5l2 2M14.5 14.5l2 2M16.5 7.5l-2 2M9.5 14.5l-2 2"/></svg>
                    {:else}
                      <svg viewBox="0 0 24 24" width="15" height="15" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h8M8 9h2"/></svg>
                    {/if}
                  </div>
                  <div class="chat-item__info">
                    <div class="chat-item__name">{item.label}</div>
                    <div class="chat-item__preview">{item.hint}</div>
                  </div>
                </button>
              {/each}
              {#if sidebarTab === 'logs'}
                <div class="logs-quota-mini" style="margin: 4px 16px;">
                  <span>{(logTotalBytes / 1024).toFixed(0)} KB / {(logQuotaBytes / (1024 * 1024)).toFixed(0)} MB</span>
                  <div class="quota-bar"><div class="quota-fill" style="width: {Math.min(100, (logTotalBytes / logQuotaBytes) * 100)}%"></div></div>
                </div>
                {#each logDays as d}
                  <button class="chat-item {selectedLogDay === d.day ? 'active' : ''}" onclick={() => selectedLogDay = d.day}>
                    <div class="chat-item__avatar logs-day-avatar">{d.day.slice(8, 10)}</div>
                    <div class="chat-item__info">
                      <div class="chat-item__name">{new Date(d.day + 'T12:00:00Z').toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' })}</div>
                      <div class="chat-item__preview">{d.lines} lines · {(d.bytes / 1024).toFixed(1)} KB</div>
                    </div>
                  </button>
                {/each}
                {#if logDays.length === 0}
                  <div class="logs-sidebar-hint"><p>No days yet</p><p class="hint-sub">APK ships logs every few seconds after pairing.</p></div>
                {/if}
              {/if}
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
              <button
                type="button"
                class="chat-item settings-nav {settingsSection === 'sms_manager' ? 'active' : ''}"
                onclick={() => { settingsSection = 'sms_manager'; }}
              >
                <div class="chat-item__avatar settings-nav__icon">S</div>
                <div class="chat-item__info">
                  <div class="chat-item__name">SMS Manager</div>
                  <div class="chat-item__preview">Bulk ops, export, stats</div>
                </div>
              </button>
            {/if}
          </div>
        </aside>

        <main class="chat-main {isToolTab ? 'chat-main--logs' : ''}">
          {#if isToolTab}
            <header class="tools-head">
              <h2 class="tools-head__title">Organic Tools</h2>
              <p class="tools-head__sub">Karaka · Organs</p>
              <nav class="tools-head__tabs">
                {#each toolsMenuItems as item}
                  <button
                    class="tools-tab {sidebarTab === item.id ? 'active' : ''}"
                    onclick={() => selectSidebarTab(item.id)}
                  >{item.label}</button>
                {/each}
              </nav>
            </header>
          {/if}
          {#if sidebarTab === 'settings'}
            {#if settingsSection === 'sms_manager'}
              <SmsManagerView
                {sessionToken}
                {vpcUrl}
                {smsList}
                {mmsList}
                {contacts}
                onChanged={() => loadSms()}
              />
            {:else}
            <Settings
              {sessionToken}
              {vpcUrl}
              bind:section={settingsSection}
              bind:syncContacts
              onContactSyncChange={toggleContactSync}
            />
            {/if}
          {:else if sidebarTab === 'quests'}
            <QuestBoard {sessionToken} {vpcUrl} />
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
          {:else if sidebarTab === 'sandbox'}
            <SandboxView
              {sessionToken}
              {vpcUrl}
            />
          {:else if sidebarTab === 'vault'}
            <VaultView
              {sessionToken}
              {vpcUrl}
            />
          {:else if sidebarTab === 'federation'}
            <FederationView
              {sessionToken}
              {vpcUrl}
            />
          {:else if selectedSender}
            {@const msgs = conversations()[selectedSender] || []}
            {@const filteredMsgs = chatSearch ? msgs.filter(m => m.body.toLowerCase().includes(chatSearch.toLowerCase())) : msgs}
            <div class="chat-main__header">
              <div class="chat-main__identity">
                <h3>
                  {getContactName(selectedSender)}
                  <span
                    class="sync-dot"
                    class:synced={lastDraftSaved > 0}
                    class:live={Date.now() - lastDraftSync < 3000}
                    title={lastDraftSaved ? 'Live sync active' : 'Sync idle'}
                  ></span>
                </h3>
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
              <div class="chat-main__tools">
                <input
                  type="text"
                  class="chat-search"
                  placeholder="Search messages..."
                  bind:value={chatSearch}
                />
                {#if chatSearch}
                  <span class="chat-search-count">{filteredMsgs.length}/{msgs.length}</span>
                {/if}
              </div>
            </div>
            <div class="chat-main__messages" bind:this={messagesEl}>
              {#each filteredMsgs as sms}
                {@const codes = sms.direction !== 'outbound' && !sms.is_mms ? smsCodes(sms) : []}
                <div class="msg {sms.direction === 'outbound' ? 'msg--out' : 'msg--in'} {sms.status === 'sending' ? 'msg--sending' : ''}">
                  {#if sms.is_mms && sms.parts?.some((p: any) => p.has_media)}
                    <div class="msg__media">
                      {#each sms.parts.filter((p: any) => p.has_media) as part}
                        {#if mmsMediaUrls[part.id]}
                          {#if part.content_type?.startsWith('image/')}
                            <a href={mmsMediaUrls[part.id]} target="_blank" rel="noopener">
                              <img class="msg__media-img" src={mmsMediaUrls[part.id]} alt={part.name || 'MMS image'} />
                            </a>
                          {:else if part.content_type?.startsWith('video/')}
                            <video class="msg__media-img" src={mmsMediaUrls[part.id]} controls></video>
                          {:else if part.content_type?.startsWith('audio/')}
                            <audio src={mmsMediaUrls[part.id]} controls></audio>
                          {:else}
                            <a class="msg__media-file" href={mmsMediaUrls[part.id]} download={part.name || 'attachment'}>📎 {part.name || 'Attachment'}</a>
                          {/if}
                        {:else}
                          <button type="button" class="msg__media-load" onclick={() => loadMmsMediaPart(part.id)}>
                            📷 Load media ({Math.round((part.size || 0) / 1024)} KB)
                          </button>
                        {/if}
                      {/each}
                    </div>
                  {/if}
                  {#if sms.body && sms.body !== '📷 Photo'}
                    <div class="msg__bubble">{sms.body}</div>
                  {/if}
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
                     {#if sms.status === 'sending'}
                       <span class="msg__status">(Sending...)</span>
                     {:else if sms.direction === 'outbound'}
                       {#if sms.status === 'sent'}
                         <span class="msg__status msg__status--sent" title="Sent by phone">✓</span>
                       {:else if sms.status === 'failed'}
                         <span class="msg__status msg__status--failed" title="Send failed">✗</span>
                       {:else if sms.status === 'outbound'}
                         <span class="msg__status msg__status--queued" title="Queued on relay">⏳</span>
                       {/if}
                     {/if}
                  </div>
                </div>
              {/each}
            </div>
            <div class="chat-main__input">
              <SmsComposeAids
                {vpcUrl}
                {sessionToken}
                nodePhone={phone || ''}
                bind:body={outboxBody}
                bind:textareaEl={outboxTextarea}
              />
              <form class="outbox-form" onsubmit={sendSms}>
                <textarea
                  bind:this={outboxTextarea}
                  bind:value={outboxBody}
                  placeholder="Écrire un SMS… (heure / lieu via les chips)"
                  rows="2"
                  required
                  oninput={(e) => triggerDraftSave(e.currentTarget.value)}
                ></textarea>
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

{#if selectedContactForProfile}
  <ContactProfileModal
    contact={selectedContactForProfile}
    {vpcUrl}
    {sessionToken}
    onClose={() => (selectedContactForProfile = null)}
    onOpenChat={(p) => {
      selectedSender = p;
      showContactsInChat = false;
    }}
    onSaved={handleContactSaved}
  />
{/if}

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
    padding: 10px 16px 2px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #80868b;
    font-weight: 600;
  }
  .logs-archive-sub {
    padding: 0 16px 8px;
    font-size: 11px;
    color: #9aa0a6;
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
    align-items: center;
    gap: 4px;
    min-width: 0;
  }
  .sidebar__tabs-main {
    flex: 1;
    display: flex;
    min-width: 0;
    justify-content: center;
  }
  .sidebar__tabs-main .tab {
    flex: 0 0 auto;
    min-width: 0;
    padding: 8px 16px;
  }
  .sidebar__tabs-center {
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .sidebar__tabs-end {
    flex: 1;
    display: flex;
    justify-content: center;
  }
  .sidebar__tabs-end .tab {
    flex: 0 0 auto;
    padding: 8px 16px;
  }
  .tab {
    flex: 1;
    padding: 8px 6px;
    border: none;
    background: transparent;
    font-size: 13px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    white-space: nowrap;
  }
  .tab.active {
    color: #202124;
    border-bottom-color: #202124;
  }
  .sidebar__apps {
    position: relative;
    flex: 0 0 auto;
    margin-left: 2px;
  }
  .sidebar__tabs-center {
    position: relative;
    flex: 0 0 auto;
    display: flex;
    align-items: center;
    justify-content: center;
  }
  .tools-trigger {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    border: none;
    border-radius: 50%;
    background: transparent;
    color: #5f6368;
    cursor: pointer;
    transition: background 0.18s ease, color 0.18s ease, transform 0.25s ease;
  }
  .tools-trigger:hover {
    background: #f1f3f4;
    color: #202124;
  }
  .tools-trigger.is-open {
    background: #202124;
    color: #fff;
    transform: scale(0.95);
  }
  .tools-trigger.is-active {
    color: #202124;
  }
  .tools-trigger.wiggle {
    animation: tools-click 0.3s ease;
  }
  @keyframes tools-click {
    0%, 100% { transform: scale(1); }
    50% { transform: scale(0.85); }
  }
  .tools-grid {
    display: grid;
    grid-template-columns: repeat(3, 4px);
    gap: 3px;
  }
  .tools-dot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: currentColor;
    opacity: 0.5;
    animation: dot-blink 3s ease-in-out infinite;
  }
  .tools-dot:nth-child(1) { animation-delay: 0s; }
  .tools-dot:nth-child(2) { animation-delay: 0.4s; }
  .tools-dot:nth-child(3) { animation-delay: 0.8s; }
  .tools-dot:nth-child(4) { animation-delay: 1.2s; }
  .tools-dot:nth-child(5) { animation-delay: 1.6s; }
  .tools-dot:nth-child(6) { animation-delay: 2.0s; }
  .tools-dot:nth-child(7) { animation-delay: 0.2s; }
  .tools-dot:nth-child(8) { animation-delay: 0.6s; }
  .tools-dot:nth-child(9) { animation-delay: 1.0s; }
  @keyframes dot-blink {
    0%, 90%, 100% { opacity: 0.45; }
    95% { opacity: 1; }
  }
  .tools-dot.alive {
    animation: dot-blink-alive 1.2s ease-in-out infinite;
  }
  @keyframes dot-blink-alive {
    0%, 100% { opacity: 0.5; }
    50% { opacity: 1; }
  }
  .chat-sub-actions {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    width: 100%;
  }
  .contacts-toggle {
    padding: 4px 10px;
    border: 1px solid #dfe1e5;
    border-radius: 4px;
    background: #fff;
    font-size: 11px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
    white-space: nowrap;
  }
  .contacts-toggle:hover {
    background: #f1f3f4;
  }
  .contacts-toggle.active {
    background: #202124;
    color: #fff;
    border-color: #202124;
  }
  .tools-head {
    padding: 16px 20px 0;
    border-bottom: 1px solid #dfe1e5;
    flex-shrink: 0;
    background: #fff;
  }
  .tools-head__title {
    margin: 0;
    font-size: 18px;
    font-weight: 600;
    color: #202124;
  }
  .tools-head__sub {
    margin: 2px 0 12px;
    font-size: 12px;
    color: #80868b;
  }
  .tools-head__tabs {
    display: flex;
    gap: 4px;
    margin-bottom: -1px;
  }
  .tools-tab {
    padding: 10px 14px;
    border: none;
    background: transparent;
    font-size: 13px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
    border-bottom: 2px solid transparent;
  }
  .tools-tab:hover {
    color: #202124;
    background: #f8f9fa;
  }
  .tools-tab.active {
    color: #202124;
    border-bottom-color: #202124;
  }
  .apps-trigger {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 108px;
    padding: 6px 8px;
    border: none;
    border-radius: 999px;
    background: transparent;
    color: #5f6368;
    cursor: pointer;
    transition: background 0.18s ease, color 0.18s ease, transform 0.18s ease;
  }
  .apps-trigger:hover,
  .apps-trigger.is-open {
    background: #f1f3f4;
    color: #202124;
  }
  .apps-trigger.is-active {
    color: #202124;
  }
  .apps-trigger.is-open .apps-grid {
    transform: rotate(90deg) scale(1.05);
  }
  .apps-grid {
    display: grid;
    grid-template-columns: repeat(3, 4px);
    gap: 3px;
    transition: transform 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
  }
  .apps-dot {
    width: 4px;
    height: 4px;
    border-radius: 50%;
    background: currentColor;
  }
  .apps-trigger__label {
    font-size: 11px;
    font-weight: 700;
    letter-spacing: 0.01em;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .apps-backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: transparent;
  }
  .apps-panel {
    position: absolute;
    top: calc(100% + 8px);
    right: 0;
    z-index: 50;
    width: min(248px, calc(100vw - 48px));
    padding: 10px;
    border: 1px solid #dadce0;
    border-radius: 16px;
    background: #fff;
    box-shadow: 0 8px 28px rgba(32, 33, 36, 0.16);
    transform-origin: top right;
    animation: apps-panel-in 0.22s cubic-bezier(0.2, 0.8, 0.2, 1);
    overflow: hidden;
  }
  @keyframes apps-panel-in {
    from {
      opacity: 0;
      transform: scale(0.92) translateY(-6px);
    }
    to {
      opacity: 1;
      transform: scale(1) translateY(0);
    }
  }
  .apps-panel__grid {
    display: grid;
    grid-template-columns: 1fr;
    gap: 4px;
  }
  .apps-tile {
    display: grid;
    grid-template-columns: 40px 1fr;
    grid-template-rows: auto auto;
    column-gap: 10px;
    row-gap: 1px;
    align-items: center;
    width: 100%;
    padding: 10px;
    border: none;
    border-radius: 12px;
    background: transparent;
    color: #202124;
    text-align: left;
    cursor: pointer;
    opacity: 0;
    transform: translateY(6px);
    animation: apps-tile-in 0.24s cubic-bezier(0.2, 0.8, 0.2, 1) forwards;
    animation-delay: calc(var(--i, 0) * 45ms);
  }
  @keyframes apps-tile-in {
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }
  .apps-tile:hover,
  .apps-tile.active {
    background: #f1f3f4;
  }
  .apps-tile__icon {
    grid-row: 1 / span 2;
    width: 40px;
    height: 40px;
    border-radius: 12px;
    display: grid;
    place-items: center;
    background: #f1f3f4;
    color: #202124;
  }
  .apps-tile.active .apps-tile__icon {
    background: #202124;
    color: #ffffff;
  }
  .apps-tile__label {
    font-size: 13px;
    font-weight: 700;
  }
  .apps-tile__hint {
    font-size: 11px;
    color: #80868b;
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
    accent-color: #000000 !important;
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
  .chat-item {
    -webkit-user-select: none;
    user-select: none;
    -webkit-touch-callout: none;
    touch-action: manipulation;
  }
  /* Long-press arm: inline B&W on the row — no modal */
  .chat-item--armed {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 12px 12px 12px 16px;
    background: #202124;
    color: #ffffff;
    border-bottom: 1px solid #202124;
    cursor: default;
  }
  .chat-item__arm-meta {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .chat-item__arm-name {
    font-weight: 600;
    font-size: 15px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .chat-item__arm-hint {
    font-size: 12px;
    color: #bdc1c6;
  }
  .chat-item__arm-del {
    flex-shrink: 0;
    border: 1px solid #ffffff;
    background: #ffffff;
    color: #202124;
    font-size: 12px;
    font-weight: 700;
    letter-spacing: 0.02em;
    text-transform: uppercase;
    padding: 8px 12px;
    cursor: pointer;
    border-radius: 0;
  }
  .chat-item__arm-del:hover:not(:disabled) {
    background: #f1f3f4;
  }
  .chat-item__arm-del:disabled {
    opacity: 0.5;
    cursor: wait;
  }
  .chat-item__arm-cancel {
    flex-shrink: 0;
    border: 1px solid #ffffff;
    background: transparent;
    color: #ffffff;
    font-size: 12px;
    font-weight: 600;
    padding: 8px 10px;
    cursor: pointer;
    border-radius: 0;
  }
  .chat-item__arm-cancel:hover {
    background: #ffffff;
    color: #202124;
  }
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
  .chat-item__actions-group {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-right: 12px;
    flex-shrink: 0;
  }
  .btn-contact-chat {
    background: transparent;
    border: 1px solid #dfe1e5;
    color: #202124;
    border-radius: 4px;
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    transition: all 0.15s;
  }
  .btn-contact-chat:hover {
    background: #202124;
    color: #ffffff;
    border-color: #202124;
  }
  .contact-csv-actions {
    display: flex;
    gap: 6px;
    margin-top: 6px;
  }
  .btn-csv-tool {
    background: #ffffff;
    border: 1px solid #dfe1e5;
    color: #5f6368;
    font-size: 11px;
    font-weight: 600;
    padding: 3px 8px;
    border-radius: 4px;
    cursor: pointer;
    display: inline-flex;
    align-items: center;
    justify-content: center;
  }
  .btn-csv-tool:hover {
    background: #f1f3f4;
    color: #202124;
    border-color: #bdc1c6;
  }
  .hidden-file-input {
    display: none;
  }
  .chat-item--contact .btn-copy {
    flex-shrink: 0;
    margin-right: 0;
  }
  button.chat-item {
    padding: 15px 20px;
  }
  .chat-item__avatar {
    width: 40px; height: 40px; border-radius: 50%; background: #dfe1e5; display: flex; align-items: center; justify-content: center; font-weight: bold; color: #202124; flex-shrink: 0;
  }
  .chat-item__info { flex: 1; overflow: hidden; }
  .chat-item__name { font-weight: 600; font-size: 15px; margin-bottom: 4px; }
  .blurred { filter: blur(6px); user-select: none; }
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
    border-bottom: 1px solid #e8eaed;
    flex-shrink: 0;
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }

  .chat-main__tools {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-shrink: 0;
  }

  .chat-search {
    padding: 4px 8px;
    border: 1px solid #dadce0;
    border-radius: 14px;
    font-size: 12px;
    width: 160px;
    outline: none;
    background: #f1f3f4;
    color: #202124;
  }

  .chat-search:focus {
    background: #fff;
    border-color: #80868b;
  }

  .chat-search::placeholder {
    color: #80868b;
  }

  .chat-search-count {
    font-size: 11px;
    color: #80868b;
    white-space: nowrap;
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
  .sync-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #444;
    margin-left: 6px;
    vertical-align: middle;
    transition: background 0.3s;
  }
  .sync-dot.synced { background: #5f6368; }
  .sync-dot.live {
    background: #34a853;
    box-shadow: 0 0 6px #34a853;
    animation: sync-pulse 1.5s infinite;
  }
  @keyframes sync-pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.5; }
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
  .msg__status { margin-left: 6px; font-size: 11px; }
  .msg__status--sent { color: #137333; font-weight: 700; }
  .msg__status--failed { color: #d93025; font-weight: 700; }
  .msg__status--queued { color: #80868b; }
  .msg__media { display: flex; flex-direction: column; gap: 6px; margin-bottom: 4px; }
  .msg__media-img {
    max-width: 260px;
    max-height: 260px;
    border-radius: 12px;
    display: block;
    object-fit: cover;
  }
  .msg__media-file {
    display: inline-block;
    padding: 8px 12px;
    border-radius: 10px;
    background: #f1f3f4;
    color: #202124;
    font-size: 13px;
    text-decoration: none;
  }
  .msg__media-load {
    padding: 10px 14px;
    border: 1px dashed #bdc1c6;
    border-radius: 12px;
    background: #f8f9fa;
    color: #5f6368;
    font-size: 13px;
    cursor: pointer;
    text-align: left;
  }
  .msg__media-load:hover { background: #f1f3f4; color: #202124; }
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
  .outbox-form textarea {
    flex: 1;
    min-width: 0;
    min-height: 44px;
    max-height: 120px;
    padding: 12px 16px;
    border-radius: 16px;
    border: 1px solid #dfe1e5;
    background: #f8f9fa;
    color: #202124;
    font-size: 15px;
    outline: none;
    resize: vertical;
    font-family: inherit;
    line-height: 1.35;
  }
  .outbox-form textarea:focus { border-color: #bdc1c6; }
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
    .outbox-form textarea,
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
      gap: 2px;
    }
    .tab {
      font-size: 12px;
      padding: 6px 4px;
    }
    .apps-trigger__label {
      display: none;
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
