<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";
  import { onMount, onDestroy } from "svelte";
  import QRious from 'qrious';
  
  let isConnected = $state(false);
  let statusText = $state("Disconnected");
  
  interface SavedServer {
    url: string;
    token: string;
    certFingerprint: string;
  }
  let activeServer = $state<SavedServer | null>(null);
  let canvas = $state<HTMLCanvasElement | null>(null);

  interface BridgeStatus {
    active: boolean;
    device: string | null;
  }
  
  let bridgeStatus = $state<BridgeStatus>({ active: false, device: null });
  let interval: any;

  function renderQR() {
    setTimeout(() => {
        if (canvas && activeServer) {
            const data = JSON.stringify({ 
                url: activeServer.url, 
                token: activeServer.token, 
                cert_fingerprint: activeServer.certFingerprint 
            });
            new QRious({
              element: canvas,
              value: data,
              size: 200,
              background: 'white',
              foreground: 'black'
            });
        }
    }, 50);
  }

  function updateServerFromStorage() {
    const saved = localStorage.getItem('gafam_servers');
    if (saved) {
      try {
        const servers = JSON.parse(saved);
        if (servers.length > 0) {
            activeServer = servers[0];
            renderQR();
            return;
        }
      } catch(e) {}
    }
    activeServer = null;
  }

  onMount(() => {
    updateServerFromStorage();
    window.addEventListener('storage', updateServerFromStorage);
    
    pollStatus();
    interval = setInterval(pollStatus, 2000);
  });
  
  onDestroy(() => {
    if (interval) clearInterval(interval);
    window.removeEventListener('storage', updateServerFromStorage);
  });
  
  async function pollStatus() {
      try {
          bridgeStatus = await invoke("scrcpy_get_status");
          isConnected = bridgeStatus.active;
          if (isConnected) {
              statusText = bridgeStatus.device ? `Connected to ${bridgeStatus.device}` : "Connected";
          } else {
              statusText = "Disconnected";
          }
      } catch(e) {
          isConnected = false;
          statusText = "Error connecting";
      }
  }

  async function toggleConnection() {
      if (!activeServer) return;
      
      if (isConnected) {
          statusText = "Disconnecting...";
          await invoke("scrcpy_stop_bridge");
          isConnected = false;
          statusText = "Disconnected";
      } else {
          statusText = "Connecting...";
          try {
              let devices: any[] = await invoke("scrcpy_list_devices");
              if (devices.length > 0) {
                  await invoke("scrcpy_start_bridge", {
                      deviceId: devices[0].serial,
                      vpcUrl: activeServer.url,
                      jwt: activeServer.token
                  });
                  isConnected = true;
                  statusText = `Connected to ${devices[0].serial}`;
              } else {
                  statusText = "No Android found. Please connect via USB.";
              }
          } catch(e) {
              statusText = "Connection failed";
          }
      }
  }

  async function openSettings() {
      await invoke("show_main_window");
  }

  async function quitApp() {
      await invoke("quit_app");
  }
</script>

<div class="tray-container">
  <div class="header">
      <div class="logo">GAFAM</div>
      <div class="header-actions">
          <button class="settings-btn" onclick={openSettings}>SETTINGS</button>
          <button class="quit-btn" onclick={quitApp}>QUIT</button>
      </div>
  </div>
  
  <div class="content-area">
      {#if activeServer}
          <div class="qr-container">
              <canvas bind:this={canvas}></canvas>
          </div>
          
          <div class="server-info">
              {activeServer.url.replace(/^https?:\/\//, '')}
          </div>
          
          <div class="adb-section">
              <div class="status-indicator">
                  <span class="dot {isConnected ? 'active' : ''}"></span>
                  <span class="status-text">{statusText}</span>
              </div>
              
              <button 
                  class="adb-btn {isConnected ? 'danger' : 'primary'}" 
                  onclick={toggleConnection}
              >
                  {isConnected ? 'Stop ADB Bridge' : 'Scan & Start ADB'}
              </button>
          </div>
      {:else}
          <div class="empty-state">
              <p>No VPC configured.</p>
              <button class="adb-btn primary" onclick={openSettings}>Open Settings</button>
          </div>
      {/if}
  </div>
</div>

<style>
  :global(body) {
      margin: 0;
      padding: 0;
      background: #ffffff;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
      overflow: hidden;
      border-radius: 10px;
      user-select: none;
  }
  
  .tray-container {
      display: flex;
      flex-direction: column;
      height: 100vh;
      width: 100vw;
  }
  
  .header {
      display: flex;
      justify-content: space-between;
      align-items: center;
      padding: 15px 20px;
      background: #f8f9fa;
      border-bottom: 1px solid #eaeaea;
  }
  
  .logo {
      font-weight: bold;
      font-size: 16px;
      color: #333;
  }
  
  .header-actions {
      display: flex;
      gap: 15px;
      align-items: center;
  }
  
  .settings-btn, .quit-btn {
      background: none;
      border: none;
      font-size: 11px;
      letter-spacing: 1px;
      cursor: pointer;
      opacity: 0.6;
      transition: opacity 0.2s;
  }
  
  .settings-btn:hover, .quit-btn:hover {
      opacity: 1;
  }

  .quit-btn {
      color: #ef4444;
  }
  
  .quit-btn:hover {
      color: #dc2626;
  }
  
  .content-area {
      flex: 1;
      display: flex;
      flex-direction: column;
      align-items: center;
      justify-content: flex-start;
      padding: 20px;
      gap: 15px;
  }
  
  .qr-container {
      background: #fff;
      padding: 10px;
      border: 1px solid #eaeaea;
      border-radius: 8px;
  }
  
  .server-info {
      font-size: 14px;
      font-family: monospace;
      color: #333;
      background: #f8f9fa;
      padding: 6px 12px;
      border-radius: 4px;
      border: 1px solid #eaeaea;
  }
  
  .adb-section {
      width: 100%;
      margin-top: auto;
      display: flex;
      flex-direction: column;
      gap: 10px;
  }
  
  .status-indicator {
      display: flex;
      align-items: center;
      justify-content: center;
      gap: 8px;
  }
  
  .dot {
      width: 8px;
      height: 8px;
      border-radius: 50%;
      background: #ccc;
  }
  
  .dot.active {
      background: #10b981;
      box-shadow: 0 0 8px rgba(16, 185, 129, 0.4);
  }
  
  .status-text {
      font-size: 13px;
      color: #555;
  }
  
  .adb-btn {
      width: 100%;
      padding: 12px;
      border: none;
      border-radius: 6px;
      font-size: 14px;
      font-weight: 600;
      cursor: pointer;
      transition: all 0.2s;
  }
  
  .adb-btn.primary {
      background: #111;
      color: white;
  }
  
  .adb-btn.primary:hover {
      background: #333;
  }
  
  .adb-btn.danger {
      background: #fee2e2;
      color: #ef4444;
  }
  
  .adb-btn.danger:hover {
      background: #fecaca;
  }
  
  .empty-state {
      margin-top: 50px;
      text-align: center;
      color: #888;
      display: flex;
      flex-direction: column;
      gap: 20px;
  }
</style>
