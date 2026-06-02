<script lang="ts">
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

  export let phoneX: number;
  export let phoneY: number;
  export let activeDragType: string | null;
  export let startDrag: (e: MouseEvent, type: 'card' | 'note' | 'phone') => void;
  export let contacts: Contact[];
  export let newContactName: string;
  export let newContactPhone: string;
  export let isSubmittingContact: boolean;
  export let addContact: () => Promise<void>;
  export let smsList: SmsMessage[];
  export let mockSmsSender: string;
  export let mockSmsBody: string;
  export let isSendingSms: boolean;
  export let triggerMockSms: () => Promise<void>;
  export let activePhoneTab: 'contacts' | 'sms' | 'esim';
  export let esimMode: 'webhook' | 'lpa' | 'cloud_api';
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div 
  id="phone-card" 
  class="phone-card {activeDragType === 'phone' ? 'dragging' : ''}"
  style="left: {phoneX}%; top: {phoneY}%; z-index: {activeDragType === 'phone' ? 999 : 10};"
  on:mousedown={(e) => startDrag(e, 'phone')}
>
  <!-- Phone Header Grab Area -->
  <div class="phone-header">
    <div class="phone-header-left">
      <span class="phone-signal-bar">📶</span>
      <span>GAFAM eSIM GATEWAY</span>
    </div>
    
    <button 
      type="button" 
      class="phone-move-btn" 
      aria-label="Grab Node"
      title="Hold and drag phone card across platform"
    >
      <svg viewBox="0 0 36 36" width="10" height="10" fill="currentColor">
        <rect x="13" y="4" width="10" height="9" rx="2" />
        <rect x="13" y="23" width="10" height="9" rx="2" />
        <rect x="4" y="13" width="9" height="10" rx="2" />
        <rect x="23" y="13" width="9" height="10" rx="2" />
      </svg>
    </button>
  </div>

  <!-- Tab Navigation Menu -->
  <div class="phone-tab-bar">
    <button type="button" class="tab-btn {activePhoneTab === 'contacts' ? 'active' : ''}" on:click={() => activePhoneTab = 'contacts'}>Contacts</button>
    <button type="button" class="tab-btn {activePhoneTab === 'sms' ? 'active' : ''}" on:click={() => activePhoneTab = 'sms'}>SMS Inbox</button>
    <button type="button" class="tab-btn {activePhoneTab === 'esim' ? 'active' : ''}" on:click={() => activePhoneTab = 'esim'}>eSIM Config</button>
  </div>

  <!-- Phone Body Content -->
  <div class="phone-body">
    {#if activePhoneTab === 'contacts'}
      <!-- Contacts manager view -->
      <div class="contacts-wrapper">
        <div class="contact-list scroll-container">
          {#if contacts.length === 0}
            <span class="empty-text">No contacts found in SQLite database.</span>
          {/if}
          {#each contacts as c}
            <div class="contact-item">
              <div class="contact-avatar" style="background-color: {c.avatar_color}">
                {c.name.charAt(0).toUpperCase()}
              </div>
              <div class="contact-info">
                <span class="c-name">{c.name}</span>
                <span class="c-phone">{c.phone_number}</span>
              </div>
            </div>
          {/each}
        </div>

        <!-- Quick Add contact form -->
        <div class="quick-add-form">
          <span class="form-title">Add SQLite Contact</span>
          <div class="input-row">
            <input type="text" placeholder="Name" bind:value={newContactName} class="phone-input" />
            <input type="text" placeholder="Phone" bind:value={newContactPhone} class="phone-input" />
          </div>
          <button type="button" class="phone-action-btn" on:click={addContact} disabled={isSubmittingContact}>
            {isSubmittingContact ? 'Adding...' : 'Save Contact'}
          </button>
        </div>
      </div>
    {:else if activePhoneTab === 'sms'}
      <!-- SMS Inbox logs -->
      <div class="sms-wrapper">
        <div class="sms-list scroll-container">
          {#if smsList.length === 0}
            <span class="empty-text">No incoming text messages found.</span>
          {/if}
          {#each smsList as s}
            <div class="sms-item">
              <div class="sms-meta">
                <span class="sms-sender">{s.sender_number}</span>
                <span class="sms-time">{new Date(s.received_at).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'})}</span>
              </div>
              <div class="sms-body-text">{s.message_body}</div>
            </div>
          {/each}
        </div>

        <!-- Mock SMS trigger box to simulate webhook bridge -->
        <div class="mock-sms-form">
          <span class="form-title">Simulate eSIM Webhook Forwarder</span>
          <div class="input-row">
            <input type="text" placeholder="Sender" bind:value={mockSmsSender} class="phone-input" />
            <input type="text" placeholder="SMS Body / MFA Code" bind:value={mockSmsBody} class="phone-input" />
          </div>
          <button type="button" class="phone-action-btn sms-btn" on:click={triggerMockSms} disabled={isSendingSms}>
            {isSendingSms ? 'Syncing...' : 'Broadcast Webhook SMS'}
          </button>
        </div>
      </div>
    {:else}
      <!-- eSIM config options -->
      <div class="esim-wrapper scroll-container">
        <span class="esim-section-title">eSIM Routing Mode</span>
        
        <!-- Option A -->
        <label class="esim-option-row {esimMode === 'webhook' ? 'active' : ''}">
          <input type="radio" name="esim" value="webhook" bind:group={esimMode} />
          <div class="esim-option-desc">
            <span class="esim-mode-name">Solution A: Webhook SIM Forwarder</span>
            <span class="esim-mode-details">Active. Feeds directly from personal hardware (white SIM disk plugged via Wi-Fi/RJ45).</span>
          </div>
        </label>

        <!-- Option B -->
        <label class="esim-option-row {esimMode === 'lpa' ? 'active' : ''}">
          <input type="radio" name="esim" value="lpa" bind:group={esimMode} />
          <div class="esim-option-desc">
            <span class="esim-mode-name">Solution B: Virtual eSIM (GSMA LPA)</span>
            <span class="esim-mode-details">Standby. Requires virtual eUICC and smart-card LPA bridge inside host Linux kernel.</span>
          </div>
        </label>

        <!-- Option C -->
        <label class="esim-option-row {esimMode === 'cloud_api' ? 'active' : ''}">
          <input type="radio" name="esim" value="cloud_api" bind:group={esimMode} />
          <div class="esim-option-desc">
            <span class="esim-mode-name">Solution C: Developer SIM SDK</span>
            <span class="esim-mode-details">Offline. Standby programmable eSIM profiles routed via Twilio / Telnyx global APIs.</span>
          </div>
        </label>

        <div class="esim-info-card">
          <strong>Hardware Concept:</strong> We will construct a minimal disk device in the future. Equipped with an RJ45/Wi-Fi connection, this physical box hosts the eSIM/SIM card, boots cleanly, and forwards all text verification codes over an encrypted HTTP stream directly to GAFAM.
        </div>
      </div>
    {/if}
  </div>
</div>

<style>
  /* DRAGGABLE PHONE CARD (GAFAM eSIM / SMS Directory) */
  .phone-card {
    position: absolute;
    width: 320px;
    background: linear-gradient(135deg, rgba(255, 255, 255, 0.98) 0%, rgba(248, 250, 252, 0.94) 100%);
    backdrop-filter: blur(16px);
    -webkit-backdrop-filter: blur(16px);
    border-radius: 14px;
    border: 1px solid rgba(255, 255, 255, 0.9);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    cursor: default;
    user-select: none;
    transition: transform 0.15s cubic-bezier(0.1, 0.8, 0.2, 1), box-shadow 0.15s ease;
    
    /* Sleek slate box shadow depth (matches GAFAM deck cockpit family) */
    box-shadow:
      0 10px 0 #b0bec5,
      0 16px 28px rgba(55, 71, 79, 0.16),
      0 0 0 1px rgba(255, 255, 255, 0.85) inset,
      0 0 30px rgba(207, 216, 220, 0.35);
  }

  .phone-card.dragging {
    cursor: grabbing;
    transform: translateY(3px);
    box-shadow:
      0 6px 0 #90a4ae,
      0 12px 20px rgba(55, 71, 79, 0.2);
  }

  .phone-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 14px 16px 12px 16px;
    background: rgba(236, 239, 241, 0.45);
    border-bottom: 1px solid rgba(207, 216, 220, 0.45);
    font-size: 12px;
    font-weight: 800;
    color: #37474f;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .phone-header-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  .phone-signal-bar {
    font-size: 11px;
  }

  .phone-move-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border-radius: 6px;
    background: #ffffff;
    border: 1px solid rgba(207, 216, 220, 0.8);
    color: #455a64;
    cursor: grab;
    transition: all 0.2s;
  }

  .phone-move-btn:hover {
    background: #ffffff;
    color: #1a2327;
    border-color: #90a4ae;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
  }

  .phone-move-btn:active {
    cursor: grabbing;
    transform: scale(0.9);
  }

  .phone-tab-bar {
    display: flex;
    background: rgba(241, 245, 249, 0.6);
    border-bottom: 1px solid rgba(226, 232, 240, 0.8);
    padding: 4px;
    gap: 4px;
  }

  .tab-btn {
    flex: 1;
    padding: 8px 4px;
    font-size: 10px;
    font-weight: 800;
    color: #64748b;
    background: transparent;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    transition: all 0.2s;
  }

  .tab-btn:hover {
    color: #334155;
    background: rgba(255, 255, 255, 0.35);
  }

  .tab-btn.active {
    color: #0f172a;
    background: #ffffff;
    box-shadow:
      0 1px 3px rgba(15, 23, 42, 0.08),
      0 1px 2px rgba(15, 23, 42, 0.04);
  }

  .phone-body {
    padding: 16px;
    height: 350px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    box-sizing: border-box;
  }

  .scroll-container {
    flex: 1;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-right: 4px;
  }

  /* Custom scrollbar */
  .scroll-container::-webkit-scrollbar {
    width: 4px;
  }

  .scroll-container::-webkit-scrollbar-track {
    background: transparent;
  }

  .scroll-container::-webkit-scrollbar-thumb {
    background: #cbd5e1;
    border-radius: 2px;
  }

  .scroll-container::-webkit-scrollbar-thumb:hover {
    background: #94a3b8;
  }

  /* Contacts lists items */
  .contacts-wrapper, .sms-wrapper {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }

  .contact-item {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 8px 10px;
    background: rgba(248, 250, 252, 0.85);
    border: 1px solid rgba(207, 216, 220, 0.55);
    border-radius: 8px;
  }

  .contact-avatar {
    width: 28px;
    height: 28px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    color: #ffffff;
    font-size: 11px;
    font-weight: 800;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  }

  .contact-info {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .c-name {
    font-size: 11px;
    font-weight: 800;
    color: #1e293b;
  }

  .c-phone {
    font-size: 10px;
    font-weight: 700;
    color: #64748b;
    font-family: monospace;
  }

  /* Quick Forms */
  .quick-add-form, .mock-sms-form {
    margin-top: 12px;
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding-top: 12px;
    border-top: 1px solid rgba(226, 232, 240, 0.8);
    box-sizing: border-box;
  }

  .form-title {
    font-size: 9px;
    font-weight: 800;
    color: #64748b;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .input-row {
    display: flex;
    gap: 8px;
  }

  .phone-input {
    flex: 1;
    min-width: 0;
    padding: 8px 10px;
    font-size: 11px;
    font-weight: 600;
    background: #ffffff;
    border: 1px solid #cbd5e1;
    border-radius: 6px;
    outline: none;
    color: #0f172a;
    transition: border-color 0.15s;
  }

  .phone-input:focus {
    border-color: #475569;
  }

  .phone-action-btn {
    width: 100%;
    padding: 9px;
    font-size: 10px;
    font-weight: 800;
    color: #ffffff;
    background: linear-gradient(135deg, #7c3aed 0%, #6d28d9 100%);
    border: none;
    border-radius: 6px;
    cursor: pointer;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    box-shadow: 0 2px 5px rgba(124, 58, 237, 0.2);
    transition: all 0.2s;
  }

  .phone-action-btn:hover {
    background: linear-gradient(135deg, #8b5cf6 0%, #7c3aed 100%);
    box-shadow: 0 4px 10px rgba(124, 58, 237, 0.32);
    transform: translateY(-0.5px);
  }

  .phone-action-btn:active {
    transform: translateY(0);
    box-shadow: 0 2px 4px rgba(124, 58, 237, 0.2);
  }

  .phone-action-btn:disabled {
    opacity: 0.6;
    cursor: not-allowed;
    box-shadow: none;
    transform: none;
  }

  .sms-btn {
    background: linear-gradient(135deg, #0284c7 0%, #0369a1 100%);
    box-shadow: 0 2px 5px rgba(2, 132, 199, 0.2);
  }

  .sms-btn:hover {
    background: linear-gradient(135deg, #0ea5e9 0%, #0284c7 100%);
    box-shadow: 0 4px 10px rgba(2, 132, 199, 0.32);
  }

  .sms-btn:active {
    box-shadow: 0 2px 4px rgba(2, 132, 199, 0.2);
  }

  /* SMS inbox list */
  .sms-item {
    display: flex;
    flex-direction: column;
    gap: 5px;
    padding: 10px;
    background: #ffffff;
    border: 1px solid rgba(226, 232, 240, 0.8);
    border-radius: 8px;
    box-shadow: 0 1px 2px rgba(0, 0, 0, 0.02);
  }

  .sms-meta {
    display: flex;
    justify-content: space-between;
    font-size: 9px;
    font-weight: 800;
    color: #64748b;
  }

  .sms-sender {
    color: #475569;
  }

  .sms-body-text {
    font-size: 11px;
    font-weight: 600;
    color: #0f172a;
    line-height: 1.4;
    font-family: monospace;
    word-break: break-all;
  }

  .empty-text {
    font-size: 10px;
    font-weight: 600;
    color: #94a3b8;
    text-align: center;
    margin: 50px 0;
  }

  /* eSIM Config panel */
  .esim-wrapper {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .esim-section-title {
    font-size: 10px;
    font-weight: 800;
    color: #475569;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    margin-bottom: 4px;
  }

  .esim-option-row {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px;
    background: rgba(248, 250, 252, 0.7);
    border: 1px solid rgba(226, 232, 240, 0.8);
    border-radius: 8px;
    cursor: pointer;
    transition: all 0.15s;
  }

  .esim-option-row:hover {
    border-color: #cbd5e1;
    background: #ffffff;
  }

  .esim-option-row.active {
    border-color: #cbd5e1;
    background: #ffffff;
  }

  .esim-option-row input[type="radio"] {
    margin: 2px 0 0 0;
    cursor: pointer;
    accent-color: #475569;
  }

  .esim-option-desc {
    display: flex;
    flex-direction: column;
    gap: 1px;
  }

  .esim-mode-name {
    font-size: 10px;
    font-weight: 800;
    color: #1e293b;
  }

  .esim-mode-details {
    font-size: 9px;
    font-weight: 600;
    color: #64748b;
    line-height: 1.35;
  }

  .esim-info-card {
    margin-top: 8px;
    padding: 10px;
    background: rgba(2, 132, 199, 0.04);
    border: 1px dashed rgba(2, 132, 199, 0.22);
    border-radius: 6px;
    font-size: 9px;
    line-height: 1.4;
    color: #0369a1;
    font-weight: 600;
  }
</style>
