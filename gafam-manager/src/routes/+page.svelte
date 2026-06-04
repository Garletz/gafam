<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";
  import { t } from "svelte-i18n";
  import { get } from "svelte/store";
  import QRious from 'qrious';
  import { onMount } from "svelte";

  interface SavedServer {
    id: string;
    name: string;
    url: string;
    token: string;
    createdAt: number;
  }

  let savedServers = $state<SavedServer[]>([]);
  let currentView = $state<'dashboard' | 'add_choice' | 'digitalocean' | 'advanced' | 'paired'>('dashboard');
  
  let generatedScript = $state('sudo bash -c "$(curl -sSL https://raw.githubusercontent.com/TonRepo/GAFAM/main/deploy-vpc.sh)"');
  let jwtToken = $state("");
  let vpcUrl = $state("");
  let doConnecting = $state(false);
  let loadingText = $state("");
  let canvas: HTMLCanvasElement;
  let activeServer: SavedServer | null = $state(null);

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

  function addServerToList(name: string, url: string, token: string) {
    const newServer: SavedServer = {
      id: crypto.randomUUID(),
      name,
      url,
      token,
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
    currentView = 'paired';
    renderQR();
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
      
      const newServer = addServerToList(`DigitalOcean VPC (${new Date().toLocaleDateString()})`, data.url, data.token);
      
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
      if (config.apiUrl && config.jwtSecret) {
        const newServer = addServerToList(`Manual Server (${new Date().toLocaleDateString()})`, config.apiUrl, config.jwtSecret);
        showServerDetails(newServer);
      } else {
        alert("Invalid JSON format. Expected apiUrl and jwtSecret.");
      }
    } catch(e) {
      alert("Invalid JSON data.");
    }
  }

  function renderQR() {
    setTimeout(() => {
        if (canvas && vpcUrl && jwtToken) {
            const data = JSON.stringify({ url: vpcUrl, token: jwtToken });
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
</style>
