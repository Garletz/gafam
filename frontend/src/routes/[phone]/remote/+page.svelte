<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import RemoteControl from '$lib/RemoteControl.svelte';
  import AdbTerminal from '$lib/AdbTerminal.svelte';

  let vpcUrl = $state('');
  let sessionToken = $state('');
  let bridgeStatus = $state<any>(null);
  let loading = $state(true);
  let phone = $state('');

  onMount(() => {
    phone = $page.params.phone;
    
    // Read auth data from cookie
    const match = document.cookie.match(new RegExp('(^| )gafam_auth_' + phone + '=([^;]+)'));
    if (match) {
      try {
        const authData = JSON.parse(decodeURIComponent(match[2]));
        vpcUrl = authData.vpcUrl || '';
        sessionToken = authData.sessionToken || '';
      } catch(e) {}
    }

    if (vpcUrl && sessionToken) {
      checkBridgeStatus();
    }
    loading = false;
  });

  async function checkBridgeStatus() {
    try {
      const proxyParams = new URLSearchParams({
        vpcUrl: vpcUrl,
        token: sessionToken,
        certFingerprint: ''
      });
      const res = await fetch(`/api/proxy/scrcpy-status?${proxyParams.toString()}`);
      if (res.ok) {
        bridgeStatus = await res.json();
      }
    } catch(e) {
      console.error('Failed to check bridge status:', e);
    }
  }
</script>

<svelte:head>
  <title>Terminal — GAFAM</title>
</svelte:head>

<div class="remote-page">
  <div class="top-nav">
    <div class="nav-brand">GAFAM Control Center</div>
    <a href="/{phone}" class="back-link">← Return to Messages</a>
  </div>

  {#if loading}
    <div class="remote-loading">
      <div class="spinner"></div>
      <p>Initializing...</p>
    </div>
  {:else if !vpcUrl || !sessionToken}
    <div class="remote-no-auth">
      <h2>Access Denied</h2>
      <p>Authentication required for remote access.</p>
      <a href="/" class="rc-back-link">← Back to Login</a>
    </div>
  {:else}
    <div class="dashboard">
      {#if bridgeStatus?.bridge_connected}
        <div class="dashboard-panel video-panel">
          <RemoteControl {vpcUrl} {sessionToken} />
        </div>
        <div class="dashboard-panel terminal-panel">
          <AdbTerminal {vpcUrl} {sessionToken} />
        </div>
      {:else}
        <div class="no-bridge">
          <div class="no-bridge-icon">⚠</div>
          <h3>VPC Bridge Disconnected</h3>
          <p>Please deploy the VPC via GAFAM Manager and connect your device.</p>
          <button class="rc-retry-btn" onclick={checkBridgeStatus}>RETRY CONNECTION</button>
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  :global(body) {
    background-color: #000;
    color: #fff;
    margin: 0;
    font-family: 'Courier New', Courier, monospace;
  }

  .remote-page {
    display: flex;
    flex-direction: column;
    height: 100vh;
    background: #000;
  }

  .top-nav {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 12px 24px;
    border-bottom: 1px solid #333;
    background: #0a0a0a;
  }

  .nav-brand {
    font-weight: bold;
    letter-spacing: 2px;
    color: #fff;
    text-transform: uppercase;
  }

  .back-link {
    color: #aaa;
    text-decoration: none;
    font-size: 14px;
    transition: color 0.2s;
  }

  .back-link:hover {
    color: #fff;
  }

  .dashboard {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  .dashboard-panel {
    display: flex;
    flex-direction: column;
    height: 100%;
  }

  .video-panel {
    flex: 1;
    border-right: 1px solid #333;
    max-width: 50%;
  }

  .terminal-panel {
    flex: 1;
    background: #000;
  }

  .no-bridge {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    width: 100%;
    color: #888;
    text-align: center;
    font-family: 'Courier New', Courier, monospace;
  }

  .no-bridge-icon {
    font-size: 48px;
    margin-bottom: 20px;
    color: #555;
  }

  .no-bridge h3 {
    color: #fff;
    font-size: 18px;
    margin-bottom: 8px;
    text-transform: uppercase;
    letter-spacing: 1px;
  }

  .no-bridge p {
    font-size: 14px;
    margin-bottom: 24px;
  }

  .rc-retry-btn {
    padding: 10px 24px;
    background: transparent;
    border: 1px solid #fff;
    color: #fff;
    cursor: pointer;
    font-family: 'Courier New', Courier, monospace;
    font-weight: bold;
    letter-spacing: 1px;
    transition: all 0.2s;
  }

  .rc-retry-btn:hover {
    background: #fff;
    color: #000;
  }

  .remote-loading, .remote-no-auth {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #888;
    font-family: 'Courier New', Courier, monospace;
  }

  .rc-back-link {
    margin-top: 16px;
    color: #fff;
    text-decoration: none;
    border-bottom: 1px solid #fff;
  }

  .spinner {
    width: 32px;
    height: 32px;
    border: 2px solid #333;
    border-top-color: #fff;
    border-radius: 50%;
    animation: spin 1s linear infinite;
    margin-bottom: 16px;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }

  /* Responsive layout for smaller screens */
  @media (max-width: 900px) {
    .dashboard {
      flex-direction: column;
    }
    .video-panel {
      max-width: 100%;
      height: 50%;
      border-right: none;
      border-bottom: 1px solid #333;
    }
    .terminal-panel {
      height: 50%;
    }
  }
</style>
