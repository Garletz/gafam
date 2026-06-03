<script lang="ts">
  import { onMount } from 'svelte';
  import DeviceCard from './components/DeviceCard.svelte';
  import NotebookCard from './components/NotebookCard.svelte';
  import PhoneCard from './components/PhoneCard.svelte';

  // Platform sizing and perspective values (Defaulting to the highly legible, flat table view)
  let basePerspective = 720;
  let baseRotateX = 21;
  let currentRotateX = 21;
  let targetRotateX = 21;
  let parallaxStrength = 0.08;

  // Interactive Camera Tuning parameters (Defaulting to maximum zoom and high readability offsets)
  let cameraZoom = 1.30;
  let cameraY = 250; // Shifting the zoomed-in deck downward to remain perfectly centered and readable
  let isTuningPanelOpen = false;

  // Save view configuration to LocalStorage
  function saveSettings() {
    try {
      localStorage.setItem('gafam_camera_zoom', cameraZoom.toString());
      localStorage.setItem('gafam_camera_y', cameraY.toString());
      localStorage.setItem('gafam_base_perspective', basePerspective.toString());
      localStorage.setItem('gafam_base_rotate_x', baseRotateX.toString());
    } catch (e) {
      console.warn("localStorage not available for saving settings.");
    }
  }

  // Load view configuration from LocalStorage
  function loadSettings() {
    try {
      const zoom = localStorage.getItem('gafam_camera_zoom');
      const y = localStorage.getItem('gafam_camera_y');
      const persp = localStorage.getItem('gafam_base_perspective');
      const tilt = localStorage.getItem('gafam_base_rotate_x');
      
      if (zoom) cameraZoom = parseFloat(zoom);
      if (y) cameraY = parseInt(y, 10);
      if (persp) basePerspective = parseInt(persp, 10);
      if (tilt) {
        baseRotateX = parseInt(tilt, 10);
        targetRotateX = baseRotateX;
      }
    } catch (e) {
      console.warn("localStorage not available for loading settings.");
    }
  }

  function resetCamera() {
    cameraZoom = 1.30;
    cameraY = 250;
    basePerspective = 720;
    baseRotateX = 21;
    targetRotateX = 21;
    try {
      localStorage.setItem('gafam_camera_zoom', '1.30');
      localStorage.setItem('gafam_camera_y', '250');
      localStorage.setItem('gafam_base_perspective', '720');
      localStorage.setItem('gafam_base_rotate_x', '21');
    } catch (e) {}
  }

  // Generalised multiple card position state (% coordinates of #platform-top)
  let cardX = 3.0;      // Left card: Device Node Node [00_LOC]
  let cardY = 10.0;
  let noteX = 36.0;     // Middle card: Lined Notepad
  let noteY = 10.0;
  let phoneX = 69.0;    // Right card: Android eSIM Emulator Node
  let phoneY = 10.0;

  // Active dragging tracking
  let activeDragType: 'card' | 'note' | 'phone' | null = null;
  
  // Drag orchestration variables
  let dragStartX = 0;
  let dragStartY = 0;
  let elementStartX = 0;
  let elementStartY = 0;

  // Lined notepad content and auto-save state
  let noteContent = "";
  let saveStatus = "Saved"; // "Saved", "Saving...", "Error"
  let saveTimeout: number;

  async function fetchNote() {
    try {
      const response = await fetch("http://localhost:5150/api/gafam/notes");
      if (response.ok) {
        const data = await response.json();
        noteContent = data.content || "";
      }
    } catch (e) {
      console.warn("Loco backend offline, using local storage fallback for notes.");
      noteContent = localStorage.getItem('gafam_local_notes') || "";
    }
  }

  async function saveNoteRemote(content: string) {
    saveStatus = "Saving...";
    try {
      const response = await fetch("http://localhost:5150/api/gafam/notes", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ content })
      });
      if (response.ok) {
        saveStatus = "Saved";
      } else {
        saveStatus = "Error saving";
      }
    } catch (e) {
      console.warn("Loco backend offline, auto-saving notes locally.");
      localStorage.setItem('gafam_local_notes', content);
      saveStatus = "Saved (local)";
    }
  }

  function handleNoteInput() {
    saveStatus = "Saving...";
    clearTimeout(saveTimeout);
    saveTimeout = window.setTimeout(() => {
      saveNoteRemote(noteContent);
    }, 800);
  }

  // Android Emulator Card - State Variables
  interface Contact {
    id: number;
    name: string;
    phone_number: string;
    avatar_color: string;
    created_at: string;
  }

  interface SmsMessage {
    id: number;
    sender_number: string;
    message_body: string;
    received_at: string;
    is_read: boolean;
  }

  let contacts: Contact[] = [];
  let newContactName = "";
  let newContactPhone = "";
  let isSubmittingContact = false;

  let smsList: SmsMessage[] = [];
  let mockSmsSender = "+33 6 12 34 56 78";
  let mockSmsBody = "MFA code: GAFAM-9874B2 (Valid 5m)";
  let isSendingSms = false;

  let activePhoneTab: 'contacts' | 'sms' | 'esim' = 'contacts';
  let esimMode: 'webhook' | 'lpa' | 'cloud_api' = 'webhook';

  // Fetch SQLite Contacts
  async function fetchContacts() {
    try {
      const response = await fetch("http://localhost:5150/api/gafam/contacts");
      if (response.ok) {
        contacts = await response.json();
      }
    } catch (e) {
      console.warn("Loco backend offline, falling back to localStorage contacts.");
      contacts = JSON.parse(localStorage.getItem('gafam_local_contacts') || "[]");
    }
  }

  // Save Contact to SQLite
  async function addContact() {
    if (!newContactName || !newContactPhone) return;
    isSubmittingContact = true;
    
    const colors = ["#7c3aed", "#0284c7", "#059669", "#ea580c", "#db2777", "#475569"];
    const avatar_color = colors[Math.floor(Math.random() * colors.length)];
    
    const contactData = {
      name: newContactName,
      phone_number: newContactPhone,
      avatar_color
    };

    try {
      const response = await fetch("http://localhost:5150/api/gafam/contacts", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(contactData)
      });
      if (response.ok) {
        newContactName = "";
        newContactPhone = "";
        await fetchContacts();
      }
    } catch (e) {
      console.warn("Loco backend offline, saving contact locally.");
      const localContacts = JSON.parse(localStorage.getItem('gafam_local_contacts') || "[]");
      localContacts.push({
        id: Date.now(),
        ...contactData,
        created_at: new Date().toISOString()
      });
      localStorage.setItem('gafam_local_contacts', JSON.stringify(localContacts));
      contacts = localContacts;
      newContactName = "";
      newContactPhone = "";
    } finally {
      isSubmittingContact = false;
    }
  }

  // Fetch SQLite SMS logs
  async function fetchSms() {
    try {
      const response = await fetch("http://localhost:5150/api/gafam/sms");
      if (response.ok) {
        smsList = await response.json();
      }
    } catch (e) {
      console.warn("Loco backend offline, falling back to localStorage SMS.");
      smsList = JSON.parse(localStorage.getItem('gafam_local_sms') || "[]");
    }
  }

  // Trigger incoming SMS mock (webhook forwarder simulation)
  async function triggerMockSms() {
    if (!mockSmsSender || !mockSmsBody) return;
    isSendingSms = true;
    
    const smsData = {
      sender_number: mockSmsSender,
      message_body: mockSmsBody
    };

    try {
      const response = await fetch("http://localhost:5150/api/gafam/sms/mock-receive", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(smsData)
      });
      if (response.ok) {
        // Automatically switch tab to SMS inbox to visually prompt users
        activePhoneTab = 'sms';
        mockSmsBody = "MFA code: GAFAM-" + Math.floor(100000 + Math.random() * 900000);
        await fetchSms();
      }
    } catch (e) {
      console.warn("Loco backend offline, mock-receiving SMS locally.");
      const localSms = JSON.parse(localStorage.getItem('gafam_local_sms') || "[]");
      localSms.unshift({
        id: Date.now(),
        ...smsData,
        received_at: new Date().toISOString(),
        is_read: false
      });
      localStorage.setItem('gafam_local_sms', JSON.stringify(localSms));
      smsList = localSms;
      activePhoneTab = 'sms';
      mockSmsBody = "MFA code: GAFAM-" + Math.floor(100000 + Math.random() * 900000);
    } finally {
      isSendingSms = false;
    }
  }

  // GAFAM Trusted Device Metadata and mock Cryptographic trust tracing
  let machineKey = "0x8fC829A...4B2c";
  let trustedFingerprint = "SHA-256: 4f89d...0c8a";
  let activeIP = "127.0.0.1 (Local DevNode)";
  let isTrusted = true;

  // Peer-to-Peer Circle of Trust delegation variables
  let partnerPhone = "+33 6 98 76 54 32";
  let delegationStatus = "Idle";
  let delegationTokenReceived = "";

  // Parallax calculations (disabled during drag to optimize rendering performance)
  function handleMouseMove(e: MouseEvent) {
    if (activeDragType) return;
    const ny = (e.clientY / window.innerHeight - 0.5) * 2;
    targetRotateX = baseRotateX - ny * parallaxStrength * 7;
  }

  // Request P2P Backup login authorization via partner phone
  async function requestP2PDelegation() {
    delegationStatus = "Sending challenge...";
    try {
      const response = await fetch("http://localhost:5150/api/gafam/request-delegation-token", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          requester_phone: "+33 6 12 34 56 78",
          partner_phone: partnerPhone
        })
      });
      if (response.ok) {
        const data = await response.json();
        delegationStatus = "Dispatched!";
        delegationTokenReceived = data.delegation_token;
        console.log("Loco backend dispatched delegated token to partner successfully:", data);
      } else {
        delegationStatus = "Failed";
      }
    } catch (e) {
      console.warn("Loco backend offline, using client-side mockup P2P dispatch.");
      setTimeout(() => {
        delegationStatus = "Dispatched!";
        delegationTokenReceived = "P2P_LOCAL_74B9D0E2";
      }, 800);
    }
  }

  // API connection to Loco Rust backend
  async function syncTrustedSession() {
    try {
      const response = await fetch("http://localhost:5150/api/gafam/trust-device", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          client_signature: machineKey,
          fingerprint: trustedFingerprint,
          ip_address: activeIP
        })
      });
      if (response.ok) {
        const data = await response.json();
        console.log("Loco backend authenticated trusted session successfully:", data);
      }
    } catch (e) {
      console.warn("Loco backend offline, using client-side fallback authentication.");
    }
  }

  // Animation Loop for fluid, dampened tilt movement
  onMount(() => {
    let rafId: number;
    function tick() {
      currentRotateX += (targetRotateX - currentRotateX) * 0.07;
      rafId = requestAnimationFrame(tick);
    }
    tick();

    // Adjust perspective and base rotation limits to stay perfectly legible across viewport scales
    function handleResize() {
      // If the user has manually tuned the camera settings, do not overwrite them on resize
      if (cameraZoom !== 1.30 || cameraY !== 250) {
        return;
      }
      const vw = window.innerWidth;
      if (vw < 480) {
        basePerspective = 390;
        baseRotateX = 22;
      } else if (vw < 768) {
        basePerspective = 460;
        baseRotateX = 24;
      } else if (vw < 1200) {
        basePerspective = 520;
        baseRotateX = 26;
      } else {
        basePerspective = 720;
        baseRotateX = 21;
      }
      if (!activeDragType) {
        targetRotateX = baseRotateX;
      }
    }

    loadSettings();
    fetchNote();
    fetchContacts();
    fetchSms();
    handleResize();
    window.addEventListener('resize', handleResize);
    
    // Initial backend synchronization
    syncTrustedSession();

    return () => {
      cancelAnimationFrame(rafId);
      window.removeEventListener('resize', handleResize);
    };
  });

  // Drag initialization
  function startDrag(e: MouseEvent, type: 'card' | 'note' | 'phone') {
    const target = e.target as HTMLElement;
    // Prevent dragging when typing in textarea or clicking buttons
    if (target.closest('textarea') || target.closest('button') || target.closest('input')) {
      return;
    }
    // Allow dragging only via the header or explicit grab handle
    if (type === 'card' && !target.closest('.drag-card__header') && !target.closest('.card-move-btn')) {
      return;
    }
    if (type === 'note' && !target.closest('.notebook-header') && !target.closest('.note-move-btn')) {
      return;
    }
    if (type === 'phone' && !target.closest('.phone-header') && !target.closest('.phone-move-btn')) {
      return;
    }
    e.preventDefault();
    activeDragType = type;
    dragStartX = e.clientX;
    dragStartY = e.clientY;

    const platformTop = document.getElementById('platform-top');
    if (platformTop) {
      if (type === 'card') {
        elementStartX = (cardX / 100) * platformTop.clientWidth;
        elementStartY = (cardY / 100) * platformTop.clientHeight;
      } else if (type === 'note') {
        elementStartX = (noteX / 100) * platformTop.clientWidth;
        elementStartY = (noteY / 100) * platformTop.clientHeight;
      } else {
        elementStartX = (phoneX / 100) * platformTop.clientWidth;
        elementStartY = (phoneY / 100) * platformTop.clientHeight;
      }
    }
  }

  // Reactive drag coordinates mapping
  function doDrag(e: MouseEvent) {
    if (!activeDragType) return;
    const dx = e.clientX - dragStartX;
    // Compensate for vertical perspective compression
    const dy = (e.clientY - dragStartY) * 1.45;

    const platformTop = document.getElementById('platform-top');
    if (platformTop) {
      let newLeftPx = elementStartX + dx;
      let newTopPx = elementStartY + dy;

      const elementId = activeDragType === 'card' ? 'test-card' : (activeDragType === 'note' ? 'notebook-card' : 'phone-card');
      const el = document.getElementById(elementId);
      const elW = el ? el.offsetWidth : 320;
      const elH = el ? el.offsetHeight : 210;

      const maxLeftPx = platformTop.clientWidth - elW;
      const maxTopPx = platformTop.clientHeight - elH;

      // Bound element inside the platform
      newLeftPx = Math.max(0, Math.min(newLeftPx, maxLeftPx));
      newTopPx = Math.max(0, Math.min(newTopPx, maxTopPx));

      if (activeDragType === 'card') {
        cardX = (newLeftPx / platformTop.clientWidth) * 100;
        cardY = (newTopPx / platformTop.clientHeight) * 100;
      } else if (activeDragType === 'note') {
        noteX = (newLeftPx / platformTop.clientWidth) * 100;
        noteY = (newTopPx / platformTop.clientHeight) * 100;
      } else {
        phoneX = (newLeftPx / platformTop.clientWidth) * 100;
        phoneY = (newTopPx / platformTop.clientHeight) * 100;
      }
    }
  }

  function stopDrag() {
    activeDragType = null;
  }

  // Mock cryptographic key signature generation
  async function cycleTrustSignature() {
    const hex = "0123456789ABCDEF";
    let newKey = "0x";
    let newFingerprint = "SHA-256: ";
    for (let i = 0; i < 8; i++) newKey += hex[Math.floor(Math.random() * 16)];
    newKey += "...";
    for (let i = 0; i < 12; i++) newFingerprint += hex[Math.floor(Math.random() * 16)].toLowerCase();
    newFingerprint += "...";
    
    machineKey = newKey + " (" + hex[Math.floor(Math.random() * 16)] + hex[Math.floor(Math.random() * 16)] + " Node)";
    trustedFingerprint = newFingerprint;

    await syncTrustedSession();
  }
</script>

<svelte:window on:mousemove={doDrag} on:mouseup={stopDrag} />

<main class="viewport" on:mousemove={handleMouseMove}>
  <!-- Background with very premium subtle high-tech ambient glow -->
  <div class="glow-bg"></div>

  <!-- Floating Camera Controller Sidebar (HUD Overlay) -->
  {#if isTuningPanelOpen}
    <div class="camera-panel-card">
      <div class="panel-header">
        <h3>DECK CONFIGURATOR</h3>
        <button type="button" class="panel-close-btn" on:click={() => isTuningPanelOpen = false}>✕</button>
      </div>
      
      <div class="panel-body">
        <div class="slider-group">
          <div class="slider-labels">
            <span class="slider-title">ZOOM SCALE</span>
            <span class="slider-value">{cameraZoom.toFixed(2)}x</span>
          </div>
          <input type="range" min="0.4" max="1.8" step="0.02" bind:value={cameraZoom} on:input={saveSettings} class="slider-input" />
        </div>

        <div class="slider-group">
          <div class="slider-labels">
            <span class="slider-title">CAMERA Y POSITION</span>
            <span class="slider-value">{cameraY}px</span>
          </div>
          <input type="range" min="-250" max="250" step="5" bind:value={cameraY} on:input={saveSettings} class="slider-input" />
        </div>

        <div class="slider-group">
          <div class="slider-labels">
            <span class="slider-title">DECK TILT</span>
            <span class="slider-value">{baseRotateX}°</span>
          </div>
          <input 
            type="range" 
            min="10" 
            max="45" 
            step="1" 
            bind:value={baseRotateX} 
            on:input={() => { targetRotateX = baseRotateX; saveSettings(); }}
            class="slider-input" 
          />
        </div>

        <div class="slider-group">
          <div class="slider-labels">
            <span class="slider-title">PERSPECTIVE DEPTH</span>
            <span class="slider-value">{basePerspective}px</span>
          </div>
          <input type="range" min="300" max="1400" step="20" bind:value={basePerspective} on:input={saveSettings} class="slider-input" />
        </div>

        <button type="button" class="reset-btn" on:click={resetCamera}>
          Reset View Parameters
        </button>
      </div>
    </div>
  {/if}

  <!-- The 3D Scene Wrapper -->
  <div id="scene" class="scene">
    <div id="platform-wrapper" class="platform-wrapper">
      <div 
        id="platform" 
        class="platform"
        style="transform: translateY({cameraY}px) scale({cameraZoom});"
      >
        
        <!-- 3D Platform Top Face (futuristic frosted silver block) -->
        <div 
          id="platform-top" 
          class="platform__top"
          style="transform: perspective({basePerspective}px) rotateX({currentRotateX}deg);"
        >
          
          <!-- Solid Estrade to demarcate central hardware/device node alignment -->
          <div class="estrade-indicator">
            <div class="estrade-glow"></div>
            <span class="estrade-label">GAFAM MINI-VPC LAYER</span>
          </div>

          <!-- Draggable Test Card: Device Node [00_LOC] -->
          <DeviceCard
            {cardX}
            {cardY}
            {activeDragType}
            {startDrag}
            {currentRotateX}
            {machineKey}
            {trustedFingerprint}
            {activeIP}
            {partnerPhone}
            {delegationStatus}
            {delegationTokenReceived}
            {requestP2PDelegation}
            {cycleTrustSignature}
          />

          <!-- Draggable Notebook Paper Sheet -->
          <NotebookCard
            {noteX}
            {noteY}
            {activeDragType}
            {startDrag}
            bind:noteContent
            {saveStatus}
            {handleNoteInput}
          />

          <!-- Draggable Android Minimalist Phone Card -->
          <PhoneCard
            {phoneX}
            {phoneY}
            {activeDragType}
            {startDrag}
            {contacts}
            bind:newContactName
            bind:newContactPhone
            {isSubmittingContact}
            {addContact}
            {smsList}
            bind:mockSmsSender
            bind:mockSmsBody
            {isSendingSms}
            {triggerMockSms}
            bind:activePhoneTab
            bind:esimMode
          />

        </div>

        <!-- 3D Platform Front Face (plunge/crease with subtle drop shadow) -->
        <div id="platform-front" class="platform__front">
          <button 
            type="button" 
            class="apron-config-btn" 
            on:click={() => isTuningPanelOpen = !isTuningPanelOpen}
            title="Open 3D View Configurator"
          >
            <svg class="gear-icon-svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="12" r="3" />
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
            </svg>
          </button>
        </div>

      </div>
    </div>
  </div>
</main>

<style>
  /* Base structural styles are kept inside the component for perfect encapsulation */
  .viewport {
    position: relative;
    width: 100vw;
    height: 100vh;
    overflow: hidden;
    background-color: #fafafa;
    display: flex;
    align-items: center;
    justify-content: center;
    font-family: 'Outfit', 'Inter', sans-serif;
  }

  .glow-bg {
    position: absolute;
    inset: 0;
    background: radial-gradient(circle 800px at 50% 50%, rgba(207, 216, 220, 0.25) 0%, rgba(255, 255, 255, 0) 100%);
    pointer-events: none;
    z-index: 1;
  }

  .scene {
    position: relative;
    z-index: 5;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
  }

  .platform-wrapper {
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
  }

  /* 3D Platform block */
  .platform {
    position: relative;
    z-index: 10;
    width: 86%;
    max-width: 1400px;
    height: 0;
  }

  .platform__top {
    position: absolute;
    bottom: 0;
    left: 0;
    width: 100%;
    aspect-ratio: 16 / 7.2;
    z-index: 2;
    background: linear-gradient(155deg, #ffffff 0%, #fcfdfe 40%, #f0f2f5 100%);
    transform-origin: center bottom;
    transition: transform 0.45s cubic-bezier(0.23, 1, 0.32, 1);
    box-shadow:
      inset 0 2px 0 rgba(255, 255, 255, 0.8),
      inset 0 -1px 0 rgba(0, 0, 0, 0.04);
    border-radius: 8px 8px 0 0;
  }

  /* Visual ambient lighting on the platform */
  .platform__top::before {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(135deg, rgba(255, 255, 255, 0.15) 0%, transparent 60%, rgba(0, 0, 0, 0.03) 100%);
    pointer-events: none;
    border-radius: 8px 8px 0 0;
  }

  .platform__front {
    position: absolute;
    top: -1px;
    left: 0;
    width: 100%;
    height: 2500px;
    z-index: 1;
    background: linear-gradient(to bottom, #e2e5e9 0%, #d5d9de 15%, #c8ccd1 40%, #b0b4b8 100%);
    transform: perspective(800px) rotateX(-19deg);
    transform-origin: center top;
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.5),
      inset 0 3px 6px rgba(0, 0, 0, 0.05);
  }

  /* Shading on the edges of front face for solid 3D depth */
  .platform__front::before {
    content: '';
    position: absolute;
    inset: 0;
    background: linear-gradient(to right, rgba(0, 0, 0, 0.08) 0%, transparent 15%, transparent 85%, rgba(0, 0, 0, 0.08) 100%);
    pointer-events: none;
  }

  /* Central VPC alignment lane */
  .estrade-indicator {
    position: absolute;
    left: 20%;
    top: 5%;
    width: 60%;
    height: 90%;
    border: 1px dashed rgba(176, 190, 197, 0.6);
    border-radius: 12px;
    pointer-events: none;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 0;
  }

  .estrade-glow {
    position: absolute;
    inset: 0;
    background: radial-gradient(circle 300px at 50% 50%, rgba(144, 202, 249, 0.06) 0%, transparent 100%);
  }

  .estrade-label {
    font-size: 11px;
    letter-spacing: 0.25em;
    color: #90a4ae;
    font-weight: 600;
    opacity: 0.8;
    text-shadow: 0 1px 0 rgba(255, 255, 255, 0.9);
  }

  /* ── Apron Calibration SVG Gear Trigger ── */
  .apron-config-btn {
    position: absolute;
    top: 8px;
    right: 20px;
    width: 32px;
    height: 32px;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    color: rgba(71, 85, 105, 0.4);
    background: rgba(255, 255, 255, 0.3);
    border: 1px solid rgba(226, 232, 240, 0.6);
    cursor: pointer;
    transition: all 0.3s cubic-bezier(0.4, 0, 0.2, 1);
    pointer-events: auto;
    border-radius: 50%;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.02);
  }

  .apron-config-btn:hover {
    color: #1e293b;
    background: #ffffff;
    border-color: #cbd5e1;
    transform: scale(1.1);
    box-shadow: 0 4px 10px rgba(15, 23, 42, 0.08);
  }

  .apron-config-btn:active {
    transform: scale(0.92);
  }

  .gear-icon-svg {
    width: 16px;
    height: 16px;
    transition: transform 0.8s cubic-bezier(0.4, 0, 0.2, 1);
  }

  .apron-config-btn:hover .gear-icon-svg {
    transform: rotate(180deg);
  }

  /* Floating HUD Modal Card */
  .camera-panel-card {
    position: fixed;
    top: 30px;
    right: 30px;
    width: 300px;
    background: rgba(255, 255, 255, 0.82);
    backdrop-filter: blur(24px);
    -webkit-backdrop-filter: blur(24px);
    border: 1px solid rgba(255, 255, 255, 0.6);
    border-radius: 16px;
    z-index: 2000;
    box-shadow:
      0 20px 40px rgba(15, 23, 42, 0.08),
      0 1px 3px rgba(0, 0, 0, 0.02),
      inset 0 0 0 1px rgba(255, 255, 255, 0.5);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    animation: modalSlideIn 0.35s cubic-bezier(0.16, 1, 0.3, 1);
    font-family: 'Outfit', 'Inter', sans-serif;
  }

  @keyframes modalSlideIn {
    from {
      opacity: 0;
      transform: translateY(12px) scale(0.98);
    }
    to {
      opacity: 1;
      transform: translateY(0) scale(1);
    }
  }

  .panel-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 14px 18px;
    border-bottom: 1px solid rgba(0, 0, 0, 0.05);
    background: rgba(248, 250, 252, 0.4);
  }

  .panel-header h3 {
    margin: 0;
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.1em;
    color: #475569;
    text-transform: uppercase;
  }

  .panel-close-btn {
    background: none;
    border: none;
    font-size: 14px;
    color: #94a3b8;
    cursor: pointer;
    padding: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    width: 24px;
    height: 24px;
    transition: all 0.2s;
  }

  .panel-close-btn:hover {
    background: rgba(0, 0, 0, 0.05);
    color: #334155;
  }

  .panel-body {
    padding: 18px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }

  /* Sliders and Group controls */
  .slider-group {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  .slider-labels {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  .slider-title {
    font-size: 8px;
    font-weight: 700;
    color: #64748b;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .slider-value {
    font-family: monospace;
    font-size: 9px;
    font-weight: 700;
    color: #0f172a;
    background: rgba(0, 0, 0, 0.04);
    padding: 2px 6px;
    border-radius: 4px;
  }

  .slider-input {
    -webkit-appearance: none;
    appearance: none;
    width: 100%;
    height: 4px;
    border-radius: 2px;
    background: #cbd5e1;
    outline: none;
    cursor: pointer;
    transition: background 0.15s ease;
  }

  .slider-input:hover {
    background: #94a3b8;
  }

  .slider-input::-webkit-slider-thumb {
    -webkit-appearance: none;
    appearance: none;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: #475569;
    cursor: pointer;
    box-shadow: 0 2px 5px rgba(0, 0, 0, 0.15);
    transition: transform 0.1s, background-color 0.1s;
    border: 2px solid #ffffff;
  }

  .slider-input::-webkit-slider-thumb:hover {
    transform: scale(1.2);
    background: #0f172a;
  }

  .slider-input::-moz-range-thumb {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: #475569;
    cursor: pointer;
    box-shadow: 0 2px 5px rgba(0, 0, 0, 0.15);
    transition: transform 0.1s, background-color 0.1s;
    border: 2px solid #ffffff;
  }

  .slider-input::-moz-range-thumb:hover {
    transform: scale(1.2);
    background: #0f172a;
  }

  .reset-btn {
    width: 100%;
    padding: 9px;
    font-size: 9px;
    font-weight: 700;
    color: #475569;
    background: #f1f5f9;
    border: 1px solid #e2e8f0;
    border-radius: 8px;
    cursor: pointer;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    transition: all 0.15s ease;
    margin-top: 4px;
  }

  .reset-btn:hover {
    background: #e2e8f0;
    color: #0f172a;
    border-color: #cbd5e1;
  }

  .reset-btn:active {
    transform: scale(0.97);
  }


</style>
