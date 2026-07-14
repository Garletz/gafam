<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let {
    vpcUrl = '',
    sessionToken = ''
  }: {
    vpcUrl: string;
    sessionToken: string;
  } = $props();

  let browserRunning = $state(false);
  let isLoading = $state(false);
  let errorMsg = $state('');
  let statusMsg = $state('');
  let iframeRef: HTMLIFrameElement | null = $state(null);

  let pollInterval: ReturnType<typeof setInterval> | null = null;

  onMount(() => {
    fetchStatus();
    pollInterval = setInterval(fetchStatus, 8000);
  });

  onDestroy(() => {
    if (pollInterval) clearInterval(pollInterval);
  });

  async function fetchStatus() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'status' });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`);
      if (res.ok) {
        const data: any = await res.json();
        browserRunning = data.running;
        if (data.docker_error) errorMsg = data.docker_error;
        else if (data.running) errorMsg = '';
      }
    } catch {
    }
  }

  async function wakeBrowser() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true;
    errorMsg = '';
    statusMsg = 'Starting browser...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) {
        browserRunning = true;
        statusMsg = 'Browser ready';
        reloadIframe();
      } else {
        errorMsg = data.error || 'Failed to start browser';
        statusMsg = '';
      }
    } catch (err: any) {
      errorMsg = err.message || 'Network error';
      statusMsg = '';
    } finally {
      isLoading = false;
    }
  }

  async function stopBrowser() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true;
    errorMsg = '';
    statusMsg = 'Stopping browser...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'stop' });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) {
        browserRunning = false;
        statusMsg = 'Browser stopped';
      } else {
        errorMsg = data.error || 'Failed to stop browser';
        statusMsg = '';
      }
    } catch (err: any) {
      errorMsg = err.message || 'Network error';
      statusMsg = '';
    } finally {
      isLoading = false;
    }
  }

  function encodeVpc(vpc: string): string {
    return btoa(vpc).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
  }

  function getNoVncUrl(): string {
    if (!vpcUrl || !sessionToken) return '';
    const vpcEnc = encodeVpc(vpcUrl);
    const tokenPath = encodeURIComponent(sessionToken);
    const base = `/api/proxy/browser/t/${vpcEnc}/${tokenPath}`;
    const params = new URLSearchParams({
      autoconnect: 'true',
      resize: 'scale',
      reconnect: 'true',
      // Force websockify through our CF tunnel (default noVNC path is /websockify at site root).
      path: `${base}/websockify`
    });
    return `${base}/vnc.html?${params.toString()}`;
  }

  function reloadIframe() {
    if (iframeRef) {
      iframeRef.src = getNoVncUrl();
    }
  }
</script>

<div class="browser-view">
  <header class="browser-view__header">
    <div class="browser-view__title">
      <h3>Vātāyana</h3>
      <span class="browser-view__subtitle">Remote Browser</span>
    </div>
    <div class="browser-view__status">
      <span
        class="status-dot"
        class:is-on={browserRunning}
        class:is-off={!browserRunning && !isLoading}
        class:is-loading={isLoading}
      ></span>
      <span class="status-label">
        {browserRunning ? 'Running' : isLoading ? 'Loading...' : 'Stopped'}
      </span>
    </div>
  </header>

  <div class="browser-view__controls">
    {#if !browserRunning}
      <button
        type="button"
        class="btn btn--primary"
        onclick={wakeBrowser}
        disabled={isLoading}
      >
        {isLoading ? 'Starting...' : 'Wake Browser'}
      </button>
    {:else}
      <button
        type="button"
        class="btn btn--ghost"
        onclick={stopBrowser}
        disabled={isLoading}
      >
        {isLoading ? 'Stopping...' : 'Stop Browser'}
      </button>
    {/if}
  </div>

  {#if errorMsg}
    <p class="browser-view__error">{errorMsg}</p>
  {/if}
  {#if statusMsg && !errorMsg}
    <p class="browser-view__status-msg">{statusMsg}</p>
  {/if}

  <div class="browser-view__frame">
    {#if browserRunning}
      <iframe
        bind:this={iframeRef}
        src={getNoVncUrl()}
        title="Vātāyana Remote Browser"
        allow="clipboard-read; clipboard-write"
        sandbox="allow-scripts allow-same-origin allow-popups allow-forms"
      ></iframe>
    {:else}
      <div class="browser-view__placeholder">
        <p>Firefox ESR on VPC</p>
        <span>Click "Wake Browser" to start</span>
      </div>
    {/if}
  </div>
</div>

<style>
  .browser-view {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: #ffffff;
    overflow: hidden;
  }

  .browser-view__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 16px;
    border-bottom: 1px solid #dfe1e5;
    flex-shrink: 0;
  }

  .browser-view__title {
    display: flex;
    align-items: baseline;
    gap: 8px;
  }

  .browser-view__title h3 {
    margin: 0;
    font-size: 16px;
    font-weight: 600;
    color: #202124;
  }

  .browser-view__subtitle {
    font-size: 12px;
    color: #80868b;
  }

  .browser-view__status {
    display: flex;
    align-items: center;
    gap: 6px;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #bdc1c6;
  }

  .status-dot.is-on {
    background: #202124;
  }

  .status-dot.is-off {
    background: transparent;
    border: 2px solid #80868b;
    box-sizing: border-box;
  }

  .status-dot.is-loading {
    background: #202124;
    animation: pulse 1.2s ease-in-out infinite;
  }

  @keyframes pulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.35; }
  }

  .status-label {
    font-size: 12px;
    font-weight: 500;
    color: #5f6368;
  }

  .browser-view__controls {
    padding: 10px 16px;
    border-bottom: 1px solid #e8eaed;
    flex-shrink: 0;
  }

  .btn {
    padding: 8px 16px;
    border: none;
    border-radius: 6px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }

  .btn--primary {
    background: #202124;
    color: #ffffff;
  }

  .btn--primary:hover:not(:disabled) {
    background: #3c4043;
  }

  .btn--primary:disabled {
    background: #bdc1c6;
    cursor: not-allowed;
  }

  .btn--ghost {
    background: #ffffff;
    color: #202124;
    border: 1px solid #dfe1e5;
  }

  .btn--ghost:hover:not(:disabled) {
    background: #f1f3f4;
  }

  .btn--ghost:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  .browser-view__error {
    margin: 0;
    padding: 8px 16px;
    font-size: 13px;
    color: #d93025;
    background: #fce8e6;
  }

  .browser-view__status-msg {
    margin: 0;
    padding: 8px 16px;
    font-size: 13px;
    color: #5f6368;
  }

  .browser-view__frame {
    flex: 1;
    min-height: 0;
    overflow: hidden;
    position: relative;
    background: #f5f5f5;
  }

  .browser-view__frame iframe {
    width: 100%;
    height: 100%;
    border: none;
  }

  .browser-view__placeholder {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 100%;
    gap: 8px;
    color: #80868b;
  }

  .browser-view__placeholder p {
    margin: 0;
    font-size: 15px;
    font-weight: 600;
    color: #5f6368;
  }

  .browser-view__placeholder span {
    font-size: 13px;
  }
</style>
