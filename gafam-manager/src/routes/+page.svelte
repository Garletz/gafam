<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";
  import { t } from "svelte-i18n";
  import { get } from "svelte/store";

  let selectedCloud = "";
  let vpcIp = "";
  let generatedScript = 'sudo bash -c "$(curl -sSL https://raw.githubusercontent.com/TonRepo/GAFAM/main/deploy-vpc.sh)"';
  let jwtToken = "";
  let doConnecting = false;

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
      const token = await invoke('start_do_oauth');
      alert(get(t)('alerts.auth_success') + " " + token);
      doConnecting = false;
    } catch (e) {
      console.error(e);
      alert(get(t)('alerts.auth_error') + " " + e);
      doConnecting = false;
    }
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
        <textarea id="jwt" bind:value={jwtToken} placeholder='&#123;"apiUrl": "...", "jwtSecret": "..."&#125;'></textarea>
      </div>

      <button class="btn-primary" disabled={jwtToken.length < 10}>
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
  {/if}
</main>
