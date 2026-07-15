<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let {
    vpcUrl = '',
    sessionToken = ''
  }: {
    vpcUrl: string;
    sessionToken: string;
  } = $props();

  let sandboxRunning = $state(false);
  let isLoading = $state(false);
  let errorMsg = $state('');
  let statusMsg = $state('');
  let subTab: 'terminal' | 'files' | 'storage' = $state('terminal');

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
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) {
        const data: any = await res.json();
        sandboxRunning = data.running;
        if (data.error) errorMsg = data.error;
      }
    } catch { }
  }

  async function wake() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true;
    errorMsg = '';
    statusMsg = 'Starting sandbox...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) {
        sandboxRunning = true;
        statusMsg = 'Sandbox ready';
        loadFiles();
      } else {
        errorMsg = data.error || 'Failed to start';
        statusMsg = '';
      }
    } catch (err: any) {
      errorMsg = err.message || 'Network error';
      statusMsg = '';
    } finally {
      isLoading = false;
    }
  }

  async function stop() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true;
    errorMsg = '';
    statusMsg = 'Stopping sandbox...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'stop' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) {
        sandboxRunning = false;
        terminalOutput = '';
        files = [];
        vpcStorage = null;
        statusMsg = 'Sandbox stopped';
      } else {
        errorMsg = data.error || 'Failed to stop';
        statusMsg = '';
      }
    } catch (err: any) {
      errorMsg = err.message || 'Network error';
      statusMsg = '';
    } finally {
      isLoading = false;
    }
  }

  // --- Terminal ---
  let terminalInput = $state('');
  let terminalOutput = $state('');
  let execBusy = $state(false);

  async function execCommand() {
    if (!vpcUrl || !sessionToken || !terminalInput.trim() || execBusy) return;
    const cmd = terminalInput;
    terminalInput = '';
    terminalOutput += `\n$ ${cmd}\n`;
    execBusy = true;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'exec' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: cmd, timeout: 30 })
      });
      const data: any = await res.json().catch(() => ({}));
      if (!res.ok) {
        terminalOutput += `Error: ${data.error || res.status}\n`;
      } else {
        if (data.stdout) terminalOutput += data.stdout;
        if (data.stderr) terminalOutput += data.stderr;
        if (data.error) terminalOutput += `Error: ${data.error}\n`;
      }    } catch (err: any) {
      terminalOutput += `Error: ${err.message}\n`;
    } finally {
      execBusy = false;
    }
  }

  function handleTermKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault();
      execCommand();
    }
  }

  // --- Files ---
  let files: any[] = $state([]);
  let currentPath = $state('/files');
  let fileBusy = $state(false);

  const DIR_LABELS: Record<string, string> = {
    '/files': 'Files',
    '/tmp': 'Tmp',
    '/downloads': 'Downloads',
    '/screenshots': 'Screenshots',
    '/scripts': 'Scripts'
  };

  async function loadFiles() {
    if (!vpcUrl || !sessionToken) return;
    fileBusy = true;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'files', path: currentPath });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) {
        const data: any = await res.json();
        files = data.entries || [];
      }
    } catch { } finally {
      fileBusy = false;
    }
  }

  function navigateTo(path: string) {
    currentPath = path;
    loadFiles();
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
  }

  // --- Storage ---
  let vpcStorage: any = $state(null);
  let storageBusy = $state(false);

  async function loadStorage() {
    if (!vpcUrl || !sessionToken) return;
    storageBusy = true;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'storage-vpc' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) {
        vpcStorage = await res.json();
      }
    } catch { } finally {
      storageBusy = false;
    }
  }

  function tabLoad() {
    if (subTab === 'files') loadFiles();
    if (subTab === 'storage') loadStorage();
  }

  $effect(() => {
    if (sandboxRunning) tabLoad();
  });
</script>

<div class="sandbox-view">
  <header class="sandbox-view__header">
    <div class="sandbox-view__title">
      <h3>Sandbox</h3>
      <span class="sandbox-view__subtitle">Yantraśālā</span>
    </div>
    <div class="sandbox-view__status">
      <span class="status-dot" class:is-on={sandboxRunning} class:is-off={!sandboxRunning && !isLoading} class:is-loading={isLoading}></span>
      <span class="status-label">
        {sandboxRunning ? 'Running' : isLoading ? 'Loading...' : 'Stopped'}
      </span>
    </div>
  </header>

  <div class="sandbox-view__controls">
    {#if !sandboxRunning}
      <button type="button" class="btn btn--primary" onclick={wake} disabled={isLoading}>
        {isLoading ? 'Starting...' : 'Wake Sandbox'}
      </button>
    {:else}
      <button type="button" class="btn btn--ghost" onclick={stop} disabled={isLoading}>
        {isLoading ? 'Stopping...' : 'Stop Sandbox'}
      </button>
    {/if}
  </div>

  {#if errorMsg}
    <p class="sandbox-view__error">{errorMsg}</p>
  {/if}
  {#if statusMsg && !errorMsg}
    <p class="sandbox-view__status-msg">{statusMsg}</p>
  {/if}

  {#if sandboxRunning}
    <nav class="sandbox-view__subtabs">
      <button class="subtab" class:active={subTab === 'terminal'} onclick={() => { subTab = 'terminal'; }}>Terminal</button>
      <button class="subtab" class:active={subTab === 'files'} onclick={() => { subTab = 'files'; tabLoad(); }}>Files</button>
      <button class="subtab" class:active={subTab === 'storage'} onclick={() => { subTab = 'storage'; tabLoad(); }}>Storage</button>
    </nav>

    <div class="sandbox-view__body">
      {#if subTab === 'terminal'}
        <div class="terminal">
          <div class="terminal__output">{terminalOutput || '$ Terminal ready. Type a command.\n'}</div>
          <div class="terminal__input">
            <span class="terminal__prompt">$</span>
            <input
              type="text"
              bind:value={terminalInput}
              onkeydown={handleTermKeydown}
              disabled={execBusy}
              placeholder="ls -la /sandbox/files"
            />
          </div>
        </div>
      {:else if subTab === 'files'}
        <div class="files">
          <div class="files__nav">
            {#each Object.keys(DIR_LABELS) as dir}
              <button class="files__dir" class:active={currentPath === dir} onclick={() => navigateTo(dir)}>
                {DIR_LABELS[dir]}
              </button>
            {/each}
          </div>
          <div class="files__list">
            {#if fileBusy}
              <p class="files__empty">Loading...</p>
            {:else if files.length === 0}
              <p class="files__empty">Empty directory</p>
            {:else}
              {#each files as f}
                <div class="files__row">
                  <span class="files__icon">{f.type === 'dir' ? '📁' : '📄'}</span>
                  <span class="files__name">{f.name}</span>
                  <span class="files__size">{f.type === 'dir' ? '-' : formatSize(f.size)}</span>
                </div>
              {/each}
            {/if}
          </div>
        </div>
      {:else if subTab === 'storage'}
        <div class="storage">
          {#if storageBusy}
            <p class="files__empty">Loading...</p>
          {:else if vpcStorage}
            <h4 class="storage__title">VPC Volumes</h4>
            {#each vpcStorage.volumes as vol}
              <div class="storage__bar">
                <div class="storage__label">{vol.name}</div>
                <div class="storage__track">
                  <div class="storage__fill" style="width: {Math.min(100, (vol.used_mb / vol.quota_mb) * 100)}%"></div>
                </div>
                <div class="storage__value">{vol.used_mb} / {vol.quota_mb} MB</div>
              </div>
            {/each}
          {:else}
            <p class="files__empty">No storage data</p>
          {/if}
        </div>
      {/if}
    </div>
  {:else}
    <div class="sandbox-view__placeholder">
      <p>Alpine Linux Sandbox</p>
      <span>Terminal · Files · Storage</span>
      <span>bash, curl, python3, jq, sqlite3, git...</span>
    </div>
  {/if}
</div>

<style>
  .sandbox-view {
    display: flex; flex-direction: column; height: 100%; background: #fff; overflow: hidden;
  }
  .sandbox-view__header {
    display: flex; align-items: center; justify-content: space-between; padding: 10px 16px;
    border-bottom: 1px solid #dfe1e5; flex-shrink: 0;
  }
  .sandbox-view__title { display: flex; align-items: baseline; gap: 8px; }
  .sandbox-view__title h3 { margin: 0; font-size: 16px; font-weight: 600; color: #202124; }
  .sandbox-view__subtitle { font-size: 12px; color: #80868b; }
  .sandbox-view__status { display: flex; align-items: center; gap: 6px; }
  .status-dot { width: 8px; height: 8px; border-radius: 50%; background: #bdc1c6; }
  .status-dot.is-on { background: #202124; }
  .status-dot.is-off { background: transparent; border: 2px solid #80868b; box-sizing: border-box; }
  .status-dot.is-loading { background: #202124; animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.35} }
  .status-label { font-size: 12px; font-weight: 500; color: #5f6368; }
  .sandbox-view__controls { padding: 8px 16px; border-bottom: 1px solid #e8eaed; flex-shrink: 0; }
  .btn { padding: 8px 16px; border: none; border-radius: 6px; font-size: 13px; font-weight: 600; cursor: pointer; }
  .btn--primary { background: #202124; color: #fff; }
  .btn--primary:hover:not(:disabled) { background: #3c4043; }
  .btn--primary:disabled { background: #bdc1c6; cursor: not-allowed; }
  .btn--ghost { background: #fff; color: #202124; border: 1px solid #dfe1e5; }
  .btn--ghost:hover:not(:disabled) { background: #f1f3f4; }
  .btn--ghost:disabled { opacity: .6; cursor: not-allowed; }
  .sandbox-view__error { margin: 0; padding: 8px 16px; font-size: 13px; color: #d93025; background: #fce8e6; }
  .sandbox-view__status-msg { margin: 0; padding: 8px 16px; font-size: 13px; color: #5f6368; }
  .sandbox-view__subtabs {
    display: flex; gap: 4px; padding: 0 16px; border-bottom: 1px solid #dfe1e5; flex-shrink: 0;
  }
  .subtab {
    padding: 8px 16px; border: none; background: transparent; font-size: 13px; font-weight: 600;
    color: #5f6368; cursor: pointer; border-bottom: 2px solid transparent;
  }
  .subtab:hover { color: #202124; }
  .subtab.active { color: #202124; border-bottom-color: #202124; }
  .sandbox-view__body { flex: 1; min-height: 0; overflow: auto; }
  .sandbox-view__placeholder {
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    flex: 1; gap: 8px; color: #80868b;
  }
  .sandbox-view__placeholder p { margin: 0; font-size: 15px; font-weight: 600; color: #5f6368; }
  .sandbox-view__placeholder span { font-size: 13px; }

  .terminal { display: flex; flex-direction: column; height: 100%; background: #1a1a1a; padding: 12px; font-family: monospace; font-size: 13px; }
  .terminal__output { flex: 1; overflow: auto; white-space: pre-wrap; color: #e0e0e0; padding-bottom: 8px; }
  .terminal__input { display: flex; align-items: center; gap: 8px; border-top: 1px solid #333; padding-top: 8px; }
  .terminal__prompt { color: #4fc3f7; flex-shrink: 0; }
  .terminal__input input {
    flex: 1; background: transparent; border: none; color: #e0e0e0; font-family: monospace; font-size: 13px; outline: none;
  }

  .files { padding: 12px; }
  .files__nav { display: flex; gap: 4px; margin-bottom: 12px; flex-wrap: wrap; }
  .files__dir {
    padding: 4px 10px; border: 1px solid #dfe1e5; border-radius: 4px; background: #fff;
    font-size: 12px; color: #5f6368; cursor: pointer;
  }
  .files__dir.active { background: #202124; color: #fff; border-color: #202124; }
  .files__list { border: 1px solid #dfe1e5; border-radius: 6px; overflow: hidden; }
  .files__empty { padding: 20px; text-align: center; color: #80868b; font-size: 13px; }
  .files__row { display: flex; align-items: center; gap: 8px; padding: 8px 12px; border-bottom: 1px solid #f1f3f4; font-size: 13px; }
  .files__row:last-child { border-bottom: none; }
  .files__icon { flex-shrink: 0; }
  .files__name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: #202124; }
  .files__size { flex-shrink: 0; color: #80868b; font-variant-numeric: tabular-nums; }

  .storage { padding: 16px; }
  .storage__title { margin: 0 0 12px; font-size: 14px; font-weight: 600; color: #202124; }
  .storage__bar { margin-bottom: 10px; }
  .storage__label { font-size: 11px; font-weight: 600; color: #80868b; text-transform: uppercase; margin-bottom: 4px; }
  .storage__track { height: 6px; background: #e8eaed; border-radius: 3px; overflow: hidden; margin-bottom: 2px; }
  .storage__fill { height: 100%; background: #202124; border-radius: 3px; transition: width .3s; }
  .storage__value { font-size: 11px; color: #5f6368; font-variant-numeric: tabular-nums; }
</style>
