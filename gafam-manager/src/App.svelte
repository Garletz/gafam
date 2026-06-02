<script lang="ts">
  import { invoke } from "@tauri-apps/api/core";

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
    alert("Script copié ! Lance-le sur ton serveur Linux en tant que root.");
  }

  async function startDigitalOceanAuth() {
    doConnecting = true;
    try {
      // Simulate calling the Rust backend to start the OAuth local server and open browser
      // const token = await invoke('start_do_oauth');
      console.log("Ouverture du navigateur pour l'authentification DigitalOcean...");
      setTimeout(() => {
        alert("Authentification DigitalOcean réussie (Simulation).");
        doConnecting = false;
      }, 2000);
    } catch (e) {
      console.error(e);
      doConnecting = false;
    }
  }
</script>

<main class="container">
  <header>
    <h1>GAFAM Manager</h1>
    <p class="subtitle">Initialise ton VPC Privé simplement.</p>
  </header>

  {#if !selectedCloud}
    <div class="grid">
      <button class="card" on:click={() => selectCloud('digitalocean')}>
        <div class="card-content">
          <h3>DigitalOcean</h3>
          <p>Déploiement automatique en 1 clic via OAuth</p>
        </div>
      </button>

      <button class="card" on:click={() => selectCloud('advanced')}>
        <div class="card-content">
          <h3>Serveur Avancé</h3>
          <p>Exécuter le script sur un Linux existant</p>
        </div>
      </button>
    </div>
  {:else if selectedCloud === 'advanced'}
    <div class="panel">
      <button class="back-btn" on:click={() => selectCloud('')}>← Retour</button>
      <h2>Déploiement Manuel</h2>
      <p>Connecte-toi en SSH à ton serveur et lance cette commande :</p>
      
      <div class="code-block">
        <code>{generatedScript}</code>
        <button class="btn-secondary" on:click={copyScript}>Copier</button>
      </div>

      <div class="input-group">
        <label for="jwt">Configuration JSON générée :</label>
        <textarea id="jwt" bind:value={jwtToken} placeholder={`{"apiUrl": "...", "jwtSecret": "..."}`}></textarea>
      </div>

      <button class="btn-primary" disabled={jwtToken.length < 10}>
        Connecter le VPC
      </button>
    </div>
  {:else if selectedCloud === 'digitalocean'}
    <div class="panel">
      <button class="back-btn" on:click={() => selectCloud('')}>← Retour</button>
      <h2>Connexion à DigitalOcean</h2>
      <p>Nous allons te rediriger vers DigitalOcean pour autoriser GAFAM Manager à créer un Droplet pour ton VPC.</p>
      
      <div class="actions">
        <button class="btn-primary" on:click={startDigitalOceanAuth} disabled={doConnecting}>
          {doConnecting ? 'Connexion en cours...' : 'Autoriser DigitalOcean'}
        </button>
      </div>
    </div>
  {/if}
</main>
