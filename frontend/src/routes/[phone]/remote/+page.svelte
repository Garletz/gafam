<script lang="ts">
  import { page } from '$app/stores';
  import { onMount } from 'svelte';
  import RemoteControl from '$lib/RemoteControl.svelte';
  import AdbTerminal from '$lib/AdbTerminal.svelte';

  let vpcUrl = $state('');
  let sessionToken = $state('');
  let bridgeStatus = $state<any>(null);
  let activeTab = $state<'screen' | 'shell'>('screen');
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
  <title>Remote Control — GAFAM</title>
</svelte:head>

<div class="remote-page">
  {#if loading}
    <div class="remote-loading">
      <div class="spinner"></div>
      <p>Loading...</p>
    </div>
  {:else if !vpcUrl || !sessionToken}
    <div class="remote-no-auth">
      <h2>Not Connected</h2>
      <p>You need to be authenticated to use Remote Control.</p>
      <a href="/" class="rc-back-link">← Back to Login</a>
    </div>
  {:else}
    <div class="remote-tabs">
      <button 
        class="remote-tab" 
        class:active={activeTab === 'screen'}
        onclick={() => activeTab = 'screen'}
      >
        📱 Screen Mirror
      </button>
      <button 
        class="remote-tab" 
        class:active={activeTab === 'shell'}
        onclick={() => activeTab = 'shell'}
      >
        🖥️ ADB Shell
      </button>
      <a href="/{phone}" class="remote-tab back-tab">
        ← Messages
      </a>
    </div>

    <div class="remote-content">
      {#if activeTab === 'screen'}
        {#if bridgeStatus?.bridge_connected}
          <RemoteControl {vpcUrl} {sessionToken} />
        {:else}
          <div class="no-bridge">
            <div class="no-bridge-icon">📱</div>
            <h3>Bridge Not Connected</h3>
            <p>Open GAFAM Manager on your computer, connect your Android via USB, and start the Scrcpy Bridge.</p>
            <button class="rc-retry-btn" onclick={checkBridgeStatus}>🔄 Check Again</button>
          </div>
        {/if}
      {:else if activeTab === 'shell'}
        <AdbTerminal {vpcUrl} {sessionToken} />
      {/if}
    </div>
  {/if}
</div>

<style>
  .remote-page {
    display: flex;
    flex-direction: column;
    height: calc(100vh - 56px);
    background: #0a0a0f;
  }

  .remote-tabs {
    display: flex;
    gap: 0;
    background: rgba(255, 255, 255, 0.03);
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }

  .remote-tab {
    padding: 12px 24px;
    font-size: 13px;
    font-weight: 600;
    color: #64748b;
    background: transparent;
    border: none;
    border-bottom: 2px solid transparent;
    cursor: pointer;
    transition: all 0.15s;
    text-decoration: none;
  }

  .remote-tab:hover {
    color: #e2e8f0;
    background: rgba(255, 255, 255, 0.03);
  }

  .remote-tab.active {
    color: #6366f1;
    border-bottom-color: #6366f1;
  }

  .back-tab {
    margin-left: auto;
    color: #64748b;
  }

  .remote-content {
    flex: 1;
    overflow: hidden;
  }

  .no-bridge {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #64748b;
    text-align: center;
    padding: 40px;
  }

  .no-bridge-icon {
    font-size: 64px;
    margin-bottom: 20px;
    opacity: 0.5;
  }

  .no-bridge h3 {
    color: #e2e8f0;
    font-size: 20px;
    margin-bottom: 8px;
  }

  .no-bridge p {
    max-width: 400px;
    line-height: 1.6;
    font-size: 14px;
  }

  .rc-retry-btn {
    margin-top: 24px;
    padding: 10px 24px;
    background: rgba(99, 102, 241, 0.15);
    border: 1px solid rgba(99, 102, 241, 0.3);
    color: #818cf8;
    border-radius: 8px;
    cursor: pointer;
    font-weight: 600;
    transition: all 0.15s;
  }

  .rc-retry-btn:hover {
    background: rgba(99, 102, 241, 0.25);
  }

  .remote-loading, .remote-no-auth {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: #64748b;
  }

  .rc-back-link {
    margin-top: 16px;
    color: #6366f1;
    text-decoration: none;
  }

  .spinner {
    width: 32px;
    height: 32px;
    border: 3px solid rgba(99, 102, 241, 0.2);
    border-top-color: #6366f1;
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
    margin-bottom: 16px;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
  }
</style>
