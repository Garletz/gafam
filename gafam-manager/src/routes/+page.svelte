<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";
  import { t } from "svelte-i18n";
  import { get } from "svelte/store";
  import QRious from 'qrious';
  import { onMount, onDestroy } from "svelte";

  interface SavedServer {
    id: string;
    name: string;
    url: string;
    token: string;
    certFingerprint: string;
    createdAt: number;
  }

  let savedServers = $state<SavedServer[]>([]);
  let currentView = $state<'dashboard' | 'add_choice' | 'digitalocean' | 'advanced' | 'paired'>('dashboard');
  
  let generatedScript = $state('sudo bash -c "$(curl -sSL https://raw.githubusercontent.com/TonRepo/GAFAM/main/deploy-vpc.sh)"');
  let jwtToken = $state("");
  let certFingerprint = $state("");
  let vpcUrl = $state("");
  let doConnecting = $state(false);
  let loadingText = $state("");
  let canvas: HTMLCanvasElement;
  let activeServer: SavedServer | null = $state(null);

  // Scrcpy Bridge State (Manifest 14)
  interface AdbDevice {
    serial: string;
    model: string;
    state: string;
  }

  interface BridgeStatus {
    active: boolean;
    device: string | null;
    vpc_connected: boolean;
    uptime_secs: number;
    frames_sent: number;
  }

  let adbDevices = $state<AdbDevice[]>([]);
  let selectedDevice = $state("");
  let bridgeStatus = $state<BridgeStatus>({ active: false, device: null, vpc_connected: false, uptime_secs: 0, frames_sent: 0 });
  let adbScanning = $state(false);
  let bridgeStarting = $state(false);
  let wifiAdbIp = $state("");
  let wifiConnecting = $state(false);
  let statusInterval: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    const saved = localStorage.getItem('gafam_servers');
    if (saved) {
      try {
        savedServers = JSON.parse(saved);
      } catch(e) {}
    }
  });

  function saveServers() {
    localStorage.setItem('gafam_servers', JSON.stringify(savedServers));
  }

  function addServerToList(name: string, url: string, token: string, certFingerprint: string) {
    const newServer: SavedServer = {
      id: crypto.randomUUID(),
      name,
      url,
      token,
      certFingerprint,
      createdAt: Date.now()
    };
    savedServers = [...savedServers, newServer];
    saveServers();
    return newServer;
  }

  function deleteServer(id: string) {
    // Note: confirm() is blocked in Tauri WebViews, so we delete directly.
    savedServers = savedServers.filter(s => s.id !== id);
    saveServers();
    if (activeServer?.id === id) {
      activeServer = null;
      currentView = 'dashboard';
    }
  }

  function showServerDetails(server: SavedServer) {
    activeServer = server;
    vpcUrl = server.url;
    jwtToken = server.token;
    certFingerprint = server.certFingerprint;
    currentView = 'paired';
    renderQR();
    startStatusPolling();
  }

  // Scrcpy Bridge Functions (Manifest 14)

  function startStatusPolling() {
    stopStatusPolling();
    pollBridgeStatus();
    statusInterval = setInterval(pollBridgeStatus, 3000);
  }

  function stopStatusPolling() {
    if (statusInterval) {
      clearInterval(statusInterval);
      statusInterval = null;
    }
  }

  onDestroy(() => {
    stopStatusPolling();
  });

  async function pollBridgeStatus() {
    try {
      bridgeStatus = await invoke('scrcpy_get_status');
    } catch(e) {
      // Silent fail — bridge may not be available
    }
  }

  async function scanAdbDevices() {
    adbScanning = true;
    try {
      adbDevices = await invoke('scrcpy_list_devices');
      if (adbDevices.length > 0 && !selectedDevice) {
        selectedDevice = adbDevices[0].serial;
      }
    } catch(e) {
      console.error("ADB scan failed:", e);
    }
    adbScanning = false;
  }

  async function connectWifiAdb() {
    if (!wifiAdbIp) return;
    wifiConnecting = true;
    try {
      await invoke('scrcpy_connect_wifi', { ip: wifiAdbIp });
      await scanAdbDevices();
    } catch(e) {
      console.error("WiFi ADB failed:", e);
    }
    wifiConnecting = false;
  }

  async function startBridge() {
    if (!selectedDevice || !activeServer) return;
    bridgeStarting = true;
    try {
      await invoke('scrcpy_start_bridge', {
        deviceId: selectedDevice,
        vpcUrl: activeServer.url,
        jwt: activeServer.token
      });
      // Give it a moment to start
      await new Promise(r => setTimeout(r, 2000));
      await pollBridgeStatus();
    } catch(e) {
      console.error("Bridge start failed:", e);
    }
    bridgeStarting = false;
  }

  async function stopBridge() {
    try {
      await invoke('scrcpy_stop_bridge');
      await new Promise(r => setTimeout(r, 500));
      await pollBridgeStatus();
    } catch(e) {
      console.error("Bridge stop failed:", e);
    }
  }

  function formatUptime(secs: number): string {
    const h = Math.floor(secs / 3600);
    const m = Math.floor((secs % 3600) / 60);
    const s = secs % 60;
    if (h > 0) return `${h}h ${m}m ${s}s`;
    if (m > 0) return `${m}m ${s}s`;
    return `${s}s`;
  }

  function copyScript() {
    navigator.clipboard.writeText(generatedScript);
    alert(get(t)('alerts.script_copied'));
  }

  async function startDigitalOceanAuth() {
    doConnecting = true;
    loadingText = "Waiting for your authorization in the browser...";
    
    let stepIndex = 0;
    const steps = [
      "Creating DigitalOcean Droplet...",
      "Waiting for Droplet to boot...",
      "Retrieving public IPv4 address...",
      "Waiting for Cloud-Init script to finish...",
      "Installing Docker on the Droplet (this takes ~60s)...",
      "Downloading GAFAM backend...",
      "Starting Secure API...",
      "Finalizing VPC configuration..."
    ];
    
    const progressInterval = setInterval(() => {
        if (stepIndex < steps.length) {
            loadingText = steps[stepIndex];
            stepIndex++;
        }
    }, 12000);

    try {
      console.log(get(t)('alerts.auth_opening'));
      const response = await invoke('start_do_oauth') as string;
      const data = JSON.parse(response);
      
      vpcUrl = data.url; 
      jwtToken = data.token;
      certFingerprint = data.cert_fingerprint;
      
      loadingText = "Droplet created! Waiting for VPC API to become ready (can take up to 5 minutes)...";
      stepIndex = steps.length;
      
      let isReady = false;
      for (let i = 0; i < 150; i++) {
          await new Promise(r => setTimeout(r, 2000));
          try {
              const pingOk = await invoke('ping_vpc', { url: vpcUrl });
              if (pingOk) {
                  isReady = true;
                  break;
              }
          } catch(err) {
              console.log("Ping failed, retrying...");
          }
      }
      
      clearInterval(progressInterval);
      
      if (!isReady) {
          throw new Error("VPC failed to become ready after 5 minutes.");
      }
      
      alert(get(t)('alerts.auth_success') + " " + data.url);
      
      const newServer = addServerToList(`DigitalOcean VPC (${new Date().toLocaleDateString()})`, data.url, data.token, data.cert_fingerprint);
      
      doConnecting = false;
      showServerDetails(newServer);
    } catch (e) {
      clearInterval(progressInterval);
      console.error(e);
      alert(get(t)('alerts.auth_error') + " " + e);
      doConnecting = false;
    }
  }

  function handleManualConnect() {
    try {
      const config = JSON.parse(jwtToken);
      if (config.apiUrl && config.jwtSecret && config.certFingerprint) {
        const newServer = addServerToList(`Manual Server (${new Date().toLocaleDateString()})`, config.apiUrl, config.jwtSecret, config.certFingerprint);
        showServerDetails(newServer);
      } else {
        alert("Invalid JSON format. Expected apiUrl, jwtSecret, certFingerprint.");
      }
    } catch(e) {
      alert("Invalid JSON data.");
    }
  }

  function renderQR() {
    setTimeout(() => {
        if (canvas && vpcUrl && jwtToken && certFingerprint) {
            const data = JSON.stringify({ url: vpcUrl, token: jwtToken, cert_fingerprint: certFingerprint });
            new QRious({
              element: canvas,
              value: data,
              size: 250,
              background: 'white',
              foreground: 'black'
            });
        }
    }, 50);
  }

  function formatDate(ts: number) {
    return new Date(ts).toLocaleDateString() + ' ' + new Date(ts).toLocaleTimeString([], {hour: '2-digit', minute:'2-digit'});
  }
</script>

<main class="container">
  <header>
    <h1>{$t('app.title')}</h1>
    <p class="subtitle">{$t('app.subtitle')}</p>
  </header>

  {#if currentView === 'dashboard'}
    <div class="grid">
      <button class="card" onclick={() => currentView = 'digitalocean'}>
        <div class="card-content">
          <h3>{$t('cloud.do.title')}</h3>
          <p>{$t('cloud.do.desc')}</p>
        </div>
      </button>

      <button class="card" onclick={() => currentView = 'advanced'}>
        <div class="card-content">
          <h3>{$t('cloud.advanced.title')}</h3>
          <p>{$t('cloud.advanced.desc')}</p>
        </div>
      </button>
    </div>

    {#if savedServers.length > 0}
      <div class="saved-servers-section">
        <h3 class="section-title">Saved VPCs</h3>
        <div class="server-list">
          {#each savedServers as server}
            <button class="server-card" onclick={() => showServerDetails(server)}>
              <div class="server-info">
                <h3>{server.name}</h3>
                <p>{server.url}</p>
              </div>
              <div class="server-arrow">→</div>
            </button>
          {/each}
        </div>
      </div>
    {/if}

  {:else if currentView === 'advanced'}
    <div class="panel">
      <button class="back-btn" onclick={() => currentView = 'add_choice'}>← Back</button>
      <h2>{$t('manual.title')}</h2>
      <p>{$t('manual.instructions')}</p>
      
      <div class="code-block">
        <code>{generatedScript}</code>
        <button class="btn-secondary" onclick={copyScript}>{$t('manual.copy')}</button>
      </div>

      <div class="input-group">
        <label for="jwt">{$t('manual.json_label')}</label>
        <textarea id="jwt" bind:value={jwtToken} placeholder="Paste JSON configuration here..."></textarea>
      </div>

      <button class="btn-primary" onclick={handleManualConnect} disabled={jwtToken.length < 10}>
        {$t('manual.connect_btn')}
      </button>
    </div>

  {:else if currentView === 'digitalocean'}
    <div class="panel">
      {#if !doConnecting}
        <button class="back-btn" onclick={() => currentView = 'add_choice'}>← Back</button>
        <h2>{$t('oauth.title')}</h2>
        <p>{$t('oauth.desc')}</p>
        
        <div class="actions">
          <button class="btn-primary" onclick={startDigitalOceanAuth}>
            {$t('oauth.authorize_btn')}
          </button>
        </div>
      {:else}
        <div class="loading-container" style="text-align: center; padding: 40px 20px;">
          <div class="spinner"></div>
          <h2 style="margin-top: 20px;">Deploying your VPC</h2>
          <p style="color: var(--primary-color); font-weight: bold; margin-top: 10px;">{loadingText}</p>
          <p style="opacity: 0.6; font-size: 0.9em; margin-top: 15px;">Please do not close this window. This process usually takes up to 5 minutes.</p>
        </div>
      {/if}
    </div>

  {:else if currentView === 'paired'}
    <div class="panel" style="text-align: center;">
      <div style="display: flex; justify-content: space-between; align-items: center; width: 100%;">
        <button class="back-btn" onclick={() => currentView = 'dashboard'} style="margin: 0;">← Dashboard</button>
        {#if activeServer}
          <button class="back-btn" style="color: var(--danger); margin: 0; background: transparent; border: none; cursor: pointer; opacity: 0.7;" onclick={() => deleteServer(activeServer!.id)}>Delete</button>
        {/if}
      </div>
      
      <h2 style="margin-top: 24px;">{activeServer?.name || "VPC Connected"}</h2>
      <p>Scan this QR Code with your GAFAM Android Relay app to link it securely to your VPC.</p>
      
      <div style="background: white; padding: 20px; border-radius: 12px; display: inline-block; margin-top: 20px; border: 1px solid var(--border);">
         <canvas bind:this={canvas}></canvas>
      </div>
      
      <p style="margin-top: 20px; font-size: 0.9em; opacity: 0.7;">
         VPC URL: <code>{vpcUrl}</code>
      </p>

      <!-- Scrcpy Remote Control Section (Manifest 14) -->
      <div class="remote-section">
        <div class="remote-header">
          <h3>📱 REMOTE CONTROL</h3>
          {#if bridgeStatus.active}
            <span class="status-badge active">● Bridge Active</span>
          {:else}
            <span class="status-badge inactive">○ Bridge Inactive</span>
          {/if}
        </div>

        {#if bridgeStatus.active}
          <div class="bridge-info">
            <div class="info-row">
              <span class="info-label">Device</span>
              <span class="info-value">{bridgeStatus.device || 'Unknown'}</span>
            </div>
            <div class="info-row">
              <span class="info-label">VPS</span>
              <span class="info-value" style="color: {bridgeStatus.vpc_connected ? 'var(--success)' : 'var(--danger)'}">
                {bridgeStatus.vpc_connected ? '● Connected' : '○ Disconnected'}
              </span>
            </div>
            <div class="info-row">
              <span class="info-label">Uptime</span>
              <span class="info-value">{formatUptime(bridgeStatus.uptime_secs)}</span>
            </div>
            <div class="info-row">
              <span class="info-label">Frames Sent</span>
              <span class="info-value">{bridgeStatus.frames_sent.toLocaleString()}</span>
            </div>
          </div>

          <button class="btn-danger" onclick={stopBridge}>
            ■ Stop Bridge
          </button>
        {:else}
          <!-- ADB Device Selection -->
          <div class="adb-section">
            <div class="adb-controls">
              <button class="btn-secondary" onclick={scanAdbDevices} disabled={adbScanning}>
                {adbScanning ? '🔍 Scanning...' : '🔌 Scan ADB Devices'}
              </button>
            </div>

            {#if adbDevices.length > 0}
              <div class="device-list">
                {#each adbDevices as device}
                  <button 
                    class="device-item" 
                    class:selected={selectedDevice === device.serial}
                    onclick={() => selectedDevice = device.serial}
                  >
                    <span class="device-icon">📱</span>
                    <div class="device-info-compact">
                      <span class="device-model">{device.model}</span>
                      <span class="device-serial">{device.serial}</span>
                    </div>
                    <span class="device-state" class:online={device.state === 'device'}>
                      {device.state === 'device' ? '● Online' : '○ ' + device.state}
                    </span>
                  </button>
                {/each}
              </div>

              <button 
                class="btn-primary" 
                onclick={startBridge} 
                disabled={!selectedDevice || bridgeStarting}
                style="margin-top: 16px; width: 100%;"
              >
                {bridgeStarting ? 'Starting Bridge...' : 'Start Scrcpy Bridge'}
              </button>
            {:else}
              <p class="hint-text">
                Connect your Android via USB or WiFi ADB, then scan for devices.
              </p>
            {/if}

            <!-- WiFi ADB -->
            <div class="wifi-adb">
              <span class="wifi-label">WiFi ADB:</span>
              <input 
                type="text" 
                bind:value={wifiAdbIp} 
                placeholder="192.168.1.42" 
                class="wifi-input"
              />
              <button class="btn-secondary btn-small" onclick={connectWifiAdb} disabled={wifiConnecting || !wifiAdbIp}>
                {wifiConnecting ? '...' : 'Connect'}
              </button>
            </div>
          </div>
        {/if}
      </div>
    </div>
  {/if}
</main>

<style>
  .saved-servers-section {
    margin-top: 48px;
    animation: fadeIn var(--transition) ease-out forwards;
  }

  .section-title {
    font-size: 13px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-muted);
    margin-bottom: 16px;
    font-weight: 600;
  }

  .server-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  .server-card {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 20px;
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
    text-align: left;
    transition: all var(--transition);
  }

  .server-card:hover {
    border-color: var(--accent);
    transform: translateY(-2px);
    box-shadow: 0 4px 12px rgba(0,0,0,0.05);
  }

  .server-info h3 {
    font-size: 16px;
    font-weight: 600;
    margin-bottom: 4px;
    color: var(--text-primary);
  }

  .server-info p {
    font-size: 13px;
    font-family: monospace;
    color: var(--accent);
    margin-bottom: 8px;
  }

  .server-arrow {
    font-size: 20px;
    color: var(--text-muted);
    transition: transform var(--transition);
  }

  .server-card:hover .server-arrow {
    transform: translateX(4px);
    color: var(--accent);
  }

  /* Scrcpy Remote Control Styles (Manifest 14) */

  .remote-section {
    margin-top: 40px;
    padding-top: 32px;
    border-top: 1px solid var(--border);
    text-align: left;
  }

  .remote-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 20px;
  }

  .remote-header h3 {
    font-size: 14px;
    text-transform: uppercase;
    letter-spacing: 1px;
    color: var(--text-primary);
    font-weight: 700;
    margin: 0;
  }

  .status-badge {
    font-size: 12px;
    font-weight: 600;
    padding: 4px 12px;
    border-radius: 20px;
  }

  .status-badge.active {
    color: #ffffff;
    background: rgba(255, 255, 255, 0.1);
  }

  .status-badge.inactive {
    color: var(--text-muted);
    background: rgba(128, 128, 128, 0.1);
  }

  .bridge-info {
    background: var(--bg-card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    padding: 16px;
    margin-bottom: 20px;
  }

  .info-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 0;
  }

  .info-row:not(:last-child) {
    border-bottom: 1px solid var(--border);
  }

  .info-label {
    font-size: 13px;
    color: var(--text-muted);
    font-weight: 500;
  }

  .info-value {
    font-size: 13px;
    font-family: monospace;
    color: var(--text-primary);
    font-weight: 600;
  }

  .btn-danger {
    width: 100%;
    padding: 12px;
    background: rgba(255, 255, 255, 0.1);
    color: #ffffff;
    border: 1px solid rgba(255, 255, 255, 0.3);
    border-radius: var(--radius);
    font-weight: 600;
    cursor: pointer;
    transition: all var(--transition);
  }

  .btn-danger:hover {
    background: rgba(255, 255, 255, 0.2);
  }

  .adb-section {
    margin-top: 8px;
  }

  .adb-controls {
    display: flex;
    gap: 8px;
    margin-bottom: 16px;
  }

  .device-list {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  .device-item {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 16px;
    background: var(--bg-card);
    border: 2px solid var(--border);
    border-radius: var(--radius);
    cursor: pointer;
    text-align: left;
    transition: all var(--transition);
  }

  .device-item:hover {
    border-color: var(--accent);
  }

  .device-item.selected {
    border-color: var(--accent);
    background: rgba(255, 255, 255, 0.05);
  }

  .device-info-compact {
    flex: 1;
    display: flex;
    flex-direction: column;
  }

  .device-model {
    font-size: 14px;
    font-weight: 600;
    color: var(--text-primary);
  }

  .device-serial {
    font-size: 11px;
    font-family: monospace;
    color: var(--text-muted);
  }

  .device-state {
    font-size: 12px;
    font-weight: 600;
    color: var(--text-muted);
  }

  .device-state.online {
    color: #ffffff;
  }

  .terminal-content {
    color: #ffffff;
  }

  .hint-text {
    font-size: 13px;
    color: var(--text-muted);
    text-align: center;
    padding: 20px 0;
    opacity: 0.7;
  }

  .wifi-adb {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 20px;
    padding-top: 16px;
    border-top: 1px solid var(--border);
  }

  .wifi-label {
    font-size: 12px;
    color: var(--text-muted);
    font-weight: 600;
    white-space: nowrap;
  }

  .wifi-input {
    flex: 1;
    padding: 8px 12px;
    border: 1px solid var(--border);
    border-radius: 6px;
    font-size: 13px;
    font-family: monospace;
    background: var(--bg-card);
    color: var(--text-primary);
  }

  .btn-small {
    padding: 8px 16px !important;
    font-size: 12px !important;
  }
</style>
