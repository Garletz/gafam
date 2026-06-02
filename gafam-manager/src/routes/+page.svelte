<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";
  import { t } from "svelte-i18n";
  import { get } from "svelte/store";
  import QRious from 'qrious';

  let selectedCloud = "";
  let vpcIp = "";
  let generatedScript = 'sudo bash -c "$(curl -sSL https://raw.githubusercontent.com/TonRepo/GAFAM/main/deploy-vpc.sh)"';
  let jwtToken = "";
  let vpcUrl = "";
  let doConnecting = false;
  let canvas: HTMLCanvasElement;

  function selectCloud(provider: string) {
    selectedCloud = provider;
  }

  function copyScript() {
    navigator.clipboard.writeText(generatedScript);
    alert(get(t)('alerts.script_copied'));
  }

  async function startDigitalOceanAuth() {
    doConnecting = true;
    try {
      console.log(get(t)('alerts.auth_opening'));
      const response = await invoke('start_do_oauth') as string;
      const data = JSON.parse(response);
      alert(get(t)('alerts.auth_success') + " " + data.url);
      
      vpcUrl = data.url; 
      jwtToken = data.token;
      
      doConnecting = false;
      showPairingScreen();
    } catch (e) {
      console.error(e);
      alert(get(t)('alerts.auth_error') + " " + e);
      doConnecting = false;
    }
  }

  function handleManualConnect() {
    try {
      const config = JSON.parse(jwtToken);
      if (config.apiUrl && config.jwtSecret) {
        vpcUrl = config.apiUrl;
        jwtToken = config.jwtSecret;
        showPairingScreen();
      } else {
        alert("Invalid JSON format. Expected apiUrl and jwtSecret.");
      }
    } catch(e) {
      alert("Invalid JSON data.");
    }
  }

  function showPairingScreen() {
    selectedCloud = 'paired';
    setTimeout(() => {
        if (canvas) {
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
</script>

<main class="container">
  <header>
    <h1>{$t('app.title')}</h1>
    <p class="subtitle">{$t('app.subtitle')}</p>
  </header>

  {#if !selectedCloud}
    <div class="grid">
      <button class="card" on:click={() => selectCloud('digitalocean')}>
        <div class="card-content">
          <h3>{$t('cloud.do.title')}</h3>
          <p>{$t('cloud.do.desc')}</p>
        </div>
      </button>

      <button class="card" on:click={() => selectCloud('advanced')}>
        <div class="card-content">
          <h3>{$t('cloud.advanced.title')}</h3>
          <p>{$t('cloud.advanced.desc')}</p>
        </div>
      </button>
    </div>
  {:else if selectedCloud === 'advanced'}
    <div class="panel">
      <button class="back-btn" on:click={() => selectCloud('')}>{$t('manual.back')}</button>
      <h2>{$t('manual.title')}</h2>
      <p>{$t('manual.instructions')}</p>
      
      <div class="code-block">
        <code>{generatedScript}</code>
        <button class="btn-secondary" on:click={copyScript}>{$t('manual.copy')}</button>
      </div>

      <div class="input-group">
        <label for="jwt">{$t('manual.json_label')}</label>
        <textarea id="jwt" bind:value={jwtToken} placeholder="Paste JSON configuration here..."></textarea>
      </div>

      <button class="btn-primary" on:click={handleManualConnect} disabled={jwtToken.length < 10}>
        {$t('manual.connect_btn')}
      </button>
    </div>
  {:else if selectedCloud === 'digitalocean'}
    <div class="panel">
      <button class="back-btn" on:click={() => selectCloud('')}>{$t('oauth.back')}</button>
      <h2>{$t('oauth.title')}</h2>
      <p>{$t('oauth.desc')}</p>
      
      <div class="actions">
        <button class="btn-primary" on:click={startDigitalOceanAuth} disabled={doConnecting}>
          {#if doConnecting}
            {$t('oauth.connecting')}
          {:else}
            {$t('oauth.authorize_btn')}
          {/if}
        </button>
      </div>
    </div>
  {:else if selectedCloud === 'paired'}
    <div class="panel" style="text-align: center;">
      <button class="back-btn" on:click={() => selectCloud('')}>Disconnect</button>
      <h2>VPC Connected</h2>
      <p>Scan this QR Code with your GAFAM Android Relay app to link it securely to your new VPC.</p>
      
      <div style="background: white; padding: 20px; border-radius: 12px; display: inline-block; margin-top: 20px;">
         <canvas bind:this={canvas}></canvas>
      </div>
      
      <p style="margin-top: 20px; font-size: 0.9em; opacity: 0.7;">
         VPC URL: <code>{vpcUrl}</code>
      </p>
    </div>
  {/if}
</main>
