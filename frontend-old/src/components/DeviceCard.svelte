<script lang="ts">
  export let cardX: number;
  export let cardY: number;
  export let activeDragType: string | null;
  export let startDrag: (e: MouseEvent, type: 'card' | 'note' | 'phone') => void;
  export let currentRotateX: number;
  export let machineKey: string;
  export let trustedFingerprint: string;
  export let activeIP: string;
  export let partnerPhone: string;
  export let delegationStatus: string;
  export let delegationTokenReceived: string;
  export let requestP2PDelegation: () => Promise<void>;
  export let cycleTrustSignature: () => Promise<void>;
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div 
  id="test-card" 
  class="drag-card {activeDragType === 'card' ? 'dragging' : ''}"
  style="left: {cardX}%; top: {cardY}%; z-index: {activeDragType === 'card' ? 999 : 10};"
  on:mousedown={(e) => startDrag(e, 'card')}
>
  <!-- Card Glassmorphic Header -->
  <div class="drag-card__header">
    <span class="node-status-dot"></span>
    <span class="drag-card__header-text">Device Node [00_LOC]</span>
    
    <!-- Drag Grab Handle -->
    <button 
      type="button" 
      class="card-move-btn" 
      aria-label="Grab Node"
      title="Hold and drag card across platform"
    >
      <svg class="card-dpad-icon" viewBox="0 0 36 36" width="12" height="12">
        <rect x="13" y="4" width="10" height="9" rx="2" fill="currentColor" />
        <rect x="13" y="23" width="10" height="9" rx="2" fill="currentColor" />
        <rect x="4" y="13" width="9" height="10" rx="2" fill="currentColor" />
        <rect x="23" y="13" width="9" height="10" rx="2" fill="currentColor" />
        <circle cx="18" cy="18" r="4" fill="currentColor" opacity="0.4" />
      </svg>
    </button>
  </div>

  <!-- Card Content Body -->
  <div class="drag-card__content">
    <div class="info-group">
      <span class="info-title">REAL-TIME ALIGNMENT</span>
      <div class="stats-grid">
        <div class="stat-box">
          <span class="stat-lbl">X POS</span>
          <span class="stat-val">{cardX.toFixed(1)}%</span>
        </div>
        <div class="stat-box">
          <span class="stat-lbl">Y POS</span>
          <span class="stat-val">{cardY.toFixed(1)}%</span>
        </div>
        <div class="stat-box">
          <span class="stat-lbl">TILT X</span>
          <span class="stat-val">{currentRotateX.toFixed(1)}°</span>
        </div>
      </div>
    </div>

    <div class="info-group">
      <span class="info-title">TRUST SYSTEM (RankMyAura-like)</span>
      <div class="meta-row">
        <span class="meta-lbl">DEVICE KEY:</span>
        <span class="meta-val mono">{machineKey}</span>
      </div>
      <div class="meta-row">
        <span class="meta-lbl">FINGERPRINT:</span>
        <span class="meta-val mono truncate">{trustedFingerprint}</span>
      </div>
      <div class="meta-row">
        <span class="meta-lbl">DEVICE IP:</span>
        <span class="meta-val">{activeIP}</span>
      </div>
    </div>
    
    <div class="info-group">
      <span class="info-title">CIRCLE OF TRUST (P2P DELEGATION)</span>
      <div class="meta-row">
        <span class="meta-lbl">PARTNER PHONE:</span>
        <span class="meta-val">{partnerPhone}</span>
      </div>
      <div class="meta-row">
        <span class="meta-lbl">P2P STATUS:</span>
        <span class="meta-val {delegationStatus === 'Dispatched!' ? 'text-green' : ''}">{delegationStatus}</span>
      </div>
      {#if delegationTokenReceived}
        <div class="meta-row token-reveal">
           <span class="meta-lbl">DELEGATED KEY:</span>
           <span class="meta-val mono text-highlight">{delegationTokenReceived}</span>
        </div>
      {/if}
      <button 
        type="button" 
        class="secondary-btn"
        on:click={requestP2PDelegation}
      >
        Request Backup P2P Token
      </button>
    </div>

    <!-- Action to cycle/generate signature credentials -->
    <div class="card-footer">
      <button 
        type="button" 
        class="action-btn"
        on:click={cycleTrustSignature}
      >
        Regenerate Node Signature
      </button>
      <div class="secure-tag">
        <svg viewBox="0 0 24 24" width="10" height="10" fill="currentColor" style="vertical-align: middle; margin-right: 2px;">
          <path d="M18 8h-1V6c0-2.76-2.24-5-5-5S7 3.24 7 6v2H6c-1.1 0-2 .9-2 2v10c0 1.1.9 2 2 2h12c1.1 0 2-.9 2-2V10c0-1.1-.9-2-2-2zm-6 9c-1.1 0-2-.9-2-2s.9-2 2-2 2 .9 2 2-.9 2-2 2zm3.1-9H8.9V6c0-1.71 1.39-3.1 3.1-3.1 1.71 0 3.1 1.39 3.1 3.1v2z"/>
        </svg>
        Cryptographic Trust Active
      </div>
    </div>
  </div>
</div>

<style>
  .drag-card {
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
    box-shadow:
      0 10px 0 #b0bec5,
      0 16px 28px rgba(55, 71, 79, 0.16),
      0 0 0 1px rgba(255, 255, 255, 0.85) inset,
      0 0 30px rgba(207, 216, 220, 0.35);
  }

  .drag-card.dragging {
    cursor: grabbing;
    transform: translateY(3px);
    box-shadow:
      0 6px 0 #90a4ae,
      0 12px 20px rgba(55, 71, 79, 0.2);
  }

  .drag-card__header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 14px 16px 12px 16px;
    background: rgba(236, 239, 241, 0.45);
    border-bottom: 1px solid rgba(207, 216, 220, 0.45);
    font-size: 13px;
    font-weight: 800;
    color: #37474f;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .node-status-dot {
    width: 8px;
    height: 8px;
    background-color: #2e7d32;
    border-radius: 50%;
    display: inline-block;
    box-shadow: 0 0 8px #4caf50;
    animation: statusPulse 2s infinite;
  }

  @keyframes statusPulse {
    0% { opacity: 0.6; }
    50% { opacity: 1; }
    100% { opacity: 0.6; }
  }

  .drag-card__header-text {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-move-btn {
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

  .card-move-btn:hover {
    background: #ffffff;
    color: #1a2327;
    border-color: #90a4ae;
    box-shadow: 0 2px 4px rgba(0, 0, 0, 0.05);
  }

  .card-move-btn:active {
    cursor: grabbing;
    transform: scale(0.9);
  }

  .drag-card__content {
    padding: 18px;
    display: flex;
    flex-direction: column;
    gap: 14px;
  }

  .info-group {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .info-title {
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 0.09em;
    color: #78909c;
    text-transform: uppercase;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(3, 1fr);
    gap: 8px;
  }

  .stat-box {
    background: rgba(248, 250, 252, 0.95);
    border: 1px solid rgba(207, 216, 220, 0.6);
    padding: 8px 6px;
    border-radius: 8px;
    text-align: center;
    display: flex;
    flex-direction: column;
    justify-content: center;
  }

  .stat-lbl {
    font-size: 9px;
    font-weight: 700;
    color: #78909c;
    margin-bottom: 2px;
  }

  .stat-val {
    font-size: 13px;
    font-weight: 800;
    color: #1e293b;
    font-family: 'Courier New', Courier, monospace;
  }

  .meta-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 11px;
    line-height: 1.4;
    padding: 5px 0;
    border-bottom: 1px solid rgba(226, 232, 240, 0.8);
  }

  .meta-lbl {
    color: #475569;
    font-weight: 700;
  }

  .meta-val {
    color: #0f172a;
    font-weight: 700;
  }

  .meta-val.mono {
    font-family: 'Courier New', Courier, monospace;
    font-weight: 800;
  }

  .truncate {
    max-width: 150px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .card-footer {
    margin-top: 8px;
    display: flex;
    flex-direction: column;
    gap: 10px;
    align-items: center;
  }

  .action-btn {
    width: 100%;
    padding: 10px;
    font-size: 11px;
    font-weight: 800;
    color: #ffffff;
    background: linear-gradient(135deg, #37474f 0%, #212d32 100%);
    border: none;
    border-radius: 8px;
    cursor: pointer;
    box-shadow: 0 3px 8px rgba(38, 50, 56, 0.28);
    transition: all 0.2s;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .action-btn:hover {
    background: linear-gradient(135deg, #455a64 0%, #2d3d44 100%);
    box-shadow: 0 5px 12px rgba(38, 50, 56, 0.38);
    transform: translateY(-1px);
  }

  .action-btn:active {
    transform: translateY(0);
    box-shadow: 0 2px 4px rgba(38, 50, 56, 0.25);
  }

  .secure-tag {
    font-size: 9px;
    font-weight: 800;
    color: #2e7d32;
    display: flex;
    align-items: center;
    letter-spacing: 0.03em;
    opacity: 0.95;
  }

  .secondary-btn {
    width: 100%;
    padding: 9px;
    font-size: 10px;
    font-weight: 800;
    color: #37474f;
    background: #eceff1;
    border: 1px solid #cfd8dc;
    border-radius: 8px;
    cursor: pointer;
    text-transform: uppercase;
    transition: all 0.15s ease;
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.05);
  }

  .secondary-btn:hover {
    background: #cfd8dc;
    color: #212d32;
    border-color: #b0bec5;
  }

  .text-green {
    color: #2e7d32 !important;
    text-shadow: 0 0 4px rgba(46, 125, 50, 0.2);
  }

  .text-highlight {
    color: #0277bd !important;
  }

  .token-reveal {
    background: rgba(2, 119, 189, 0.04);
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px dashed rgba(2, 119, 189, 0.35);
    animation: pulseGlow 1.8s infinite ease-in-out;
  }

  @keyframes pulseGlow {
    0% { opacity: 0.85; }
    50% { opacity: 1; }
    100% { opacity: 0.85; }
  }
</style>
