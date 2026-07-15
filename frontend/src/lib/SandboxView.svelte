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
  let pollInterval: ReturnType<typeof setInterval> | null = null;

  onMount(() => { fetchStatus(); pollInterval = setInterval(fetchStatus, 8000); });
  onDestroy(() => { if (pollInterval) clearInterval(pollInterval); });

  async function fetchStatus() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'status' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) {
        const data: any = await res.json();
        sandboxRunning = data.running;
        if (data.error) errorMsg = data.error;
        if (sandboxRunning) { loadFiles(); loadStorage(); }
      }
    } catch {}
  }

  async function wake() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true; errorMsg = ''; statusMsg = 'Starting sandbox...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) { sandboxRunning = true; statusMsg = 'Sandbox ready'; loadFiles(); loadStorage(); }
      else { errorMsg = data.error || 'Failed to start'; statusMsg = ''; }
    } catch (err: any) { errorMsg = err.message || 'Network error'; statusMsg = ''; }
    finally { isLoading = false; }
  }

  async function stop() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true; errorMsg = ''; statusMsg = 'Stopping...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'stop' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) { sandboxRunning = false; terminalOutput = ''; files = []; vpcStorage = null; statusMsg = 'Stopped'; }
      else { errorMsg = data.error || 'Failed'; statusMsg = ''; }
    } catch (err: any) { errorMsg = err.message; statusMsg = ''; }
    finally { isLoading = false; }
  }

  // ─── Terminal ───
  let terminalInput = $state('');
  let terminalOutput = $state('');
  let execBusy = $state(false);
  let cmdHistory: string[] = [];
  let historyIdx = $state(-1);
  let termScroll: HTMLDivElement | null = null;

  async function execCommand() {
    if (!terminalInput.trim() || execBusy) return;
    const cmd = terminalInput;
    cmdHistory.unshift(cmd);
    if (cmdHistory.length > 50) cmdHistory.pop();
    historyIdx = -1;
    terminalInput = '';
    terminalOutput += `$ ${cmd}\n`;
    execBusy = true;
    await tick();
    scrollTerm();
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'exec' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: cmd, timeout: 30 })
      });
      const data: any = await res.json().catch(() => ({}));
      if (data.stdout) terminalOutput += data.stdout;
      if (data.stderr) terminalOutput += data.stderr;
      if (data.error) terminalOutput += `Error: ${data.error}\n`;
    } catch (err: any) { terminalOutput += `Error: ${err.message}\n`; }
    finally { execBusy = false; await tick(); scrollTerm(); }
  }

  function scrollTerm() { if (termScroll) termScroll.scrollTop = termScroll.scrollHeight; }
  async function tick() { await new Promise(r => requestAnimationFrame(() => r(null))); }

  function handleTermKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); execCommand(); }
    else if (e.key === 'ArrowUp') {
      e.preventDefault();
      if (cmdHistory.length === 0) return;
      historyIdx = Math.min(historyIdx + 1, cmdHistory.length - 1);
      terminalInput = cmdHistory[historyIdx] || '';
    } else if (e.key === 'ArrowDown') {
      e.preventDefault();
      historyIdx = Math.max(historyIdx - 1, -1);
      terminalInput = historyIdx === -1 ? '' : cmdHistory[historyIdx];
    } else if (e.key === 'l' && e.ctrlKey) {
      e.preventDefault(); terminalOutput = '';
    }
  }

  function clearTerminal() { terminalOutput = ''; }

  // ─── Files ───
  let files: any[] = $state([]);
  let currentPath = $state('/files');
  let fileBusy = $state(false);
  let selectedFile: any | null = $state(null);
  let filePreview: string = $state('');
  let filePreviewType: 'text' | 'image' | 'binary' = $state('binary');
  let dragOver = $state(false);

  const DIRS = [
    { path: '/files', label: 'Files' },
    { path: '/downloads', label: 'Downloads' },
    { path: '/screenshots', label: 'Screenshots' },
    { path: '/scripts', label: 'Scripts' },
    { path: '/tmp', label: 'Tmp' },
  ];

  async function loadFiles() {
    if (!vpcUrl || !sessionToken) return;
    fileBusy = true; selectedFile = null; filePreview = '';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'files', path: currentPath });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) { const data: any = await res.json(); files = data.entries || []; }
    } catch {} finally { fileBusy = false; }
  }

  function navigateTo(path: string) { currentPath = path; loadFiles(); }

  async function clickFile(f: any) {
    if (f.type === 'dir') {
      currentPath = currentPath === '/' ? `/${f.name}` : `${currentPath}/${f.name}`;
      loadFiles();
      return;
    }
    selectedFile = f;
    filePreview = ''; filePreviewType = 'binary';
    const ext = f.name.split('.').pop()?.toLowerCase() || '';
    if (['txt', 'md', 'json', 'js', 'ts', 'py', 'sh', 'yml', 'yaml', 'csv', 'xml', 'html', 'css', 'log', 'conf'].includes(ext)) {
      filePreviewType = 'text';
      try {
        const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'file', path: `${currentPath}/${f.name}` });
        const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
        if (res.ok) filePreview = await res.text();
      } catch { filePreview = 'Failed to load'; }
    } else if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp'].includes(ext)) {
      filePreviewType = 'image';
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'file', path: `${currentPath}/${f.name}` });
      filePreview = `/api/proxy/sandbox?${params.toString()}`;
    }
  }

  async function deleteFile(f: any) {
    if (!confirm(`Delete ${f.name}?`)) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, path: `${currentPath}/${f.name}` });
      await fetch(`/api/proxy/sandbox?${params.toString()}`, { method: 'DELETE' });
      loadFiles();
      if (selectedFile?.name === f.name) { selectedFile = null; filePreview = ''; }
    } catch {}
  }

  async function downloadFile(f: any) {
    const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'file', path: `${currentPath}/${f.name}`, download: '1' });
    const a = document.createElement('a');
    a.href = `/api/proxy/sandbox?${params.toString()}`;
    a.download = f.name;
    a.click();
  }

  async function handleDrop(e: DragEvent) {
    e.preventDefault();
    dragOver = false;
    const droppedFiles = e.dataTransfer?.files;
    if (!droppedFiles || droppedFiles.length === 0) return;
    for (const f of Array.from(droppedFiles)) {
      const path = `${currentPath}/${f.name}`;
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, path });
      await fetch(`/api/proxy/sandbox?${params.toString()}`, {
        method: 'PUT',
        headers: { 'Content-Type': f.type || 'application/octet-stream' },
        body: f
      });
    }
    loadFiles();
  }

  async function uploadViaInput(e: Event) {
    const target = e.target as HTMLInputElement;
    const uploaded = target.files;
    if (!uploaded) return;
    for (const f of Array.from(uploaded)) {
      const path = `${currentPath}/${f.name}`;
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, path });
      await fetch(`/api/proxy/sandbox?${params.toString()}`, {
        method: 'PUT',
        headers: { 'Content-Type': f.type || 'application/octet-stream' },
        body: f
      });
    }
    loadFiles();
    target.value = '';
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  function formatDate(ts: number): string {
    return new Date(ts * 1000).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
  }

  // ─── Storage ───
  let vpcStorage: any = $state(null);

  async function loadStorage() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'storage-vpc' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) vpcStorage = await res.json();
    } catch {}
  }

  function storageColor(pct: number): string {
    if (pct < 60) return '#1e8e3e';
    if (pct < 80) return '#f9ab00';
    return '#d93025';
  }
</script>

<div class="sandbox">
  <!-- Header bar -->
  <header class="sb-header">
    <div class="sb-header__left">
      <h3>Sandbox</h3>
      <span class="sb-header__sub">Yantraśālā</span>
    </div>
    <div class="sb-header__right">
      <span class="dot" class:on={sandboxRunning} class:off={!sandboxRunning && !isLoading} class:load={isLoading}></span>
      <span class="sb-header__status">{sandboxRunning ? 'Running' : isLoading ? 'Loading' : 'Stopped'}</span>
      {#if sandboxRunning}
        <button class="btn-sm btn-ghost" onclick={stop} disabled={isLoading}>Stop</button>
      {:else}
        <button class="btn-sm btn-dark" onclick={wake} disabled={isLoading}>Wake</button>
      {/if}
    </div>
  </header>

  {#if errorMsg}
    <div class="sb-error">{errorMsg}</div>
  {/if}
  {#if statusMsg && !errorMsg}
    <div class="sb-status">{statusMsg}</div>
  {/if}

  {#if sandboxRunning}
    <div class="sb-body">
      <!-- LEFT: Files -->
      <div class="sb-left">
        <div class="sb-section-title">Files</div>
        <div class="dir-tabs">
          {#each DIRS as d}
            <button class="dir-tab" class:active={currentPath === d.path} onclick={() => navigateTo(d.path)}>{d.label}</button>
          {/each}
        </div>
        <div class="breadcrumb">{currentPath}</div>

        <div
          class="file-drop-zone"
          class:dragover={dragOver}
          ondragover={(e) => { e.preventDefault(); dragOver = true; }}
          ondragleave={() => { dragOver = false; }}
          ondrop={handleDrop}
        >
          {#if fileBusy}
            <div class="file-empty">Loading...</div>
          {:else if files.length === 0}
            <div class="file-empty">Empty — drag files here</div>
          {:else}
            {#each files as f}
              <div class="file-row" class:selected={selectedFile?.name === f.name}>
                <button class="file-row__main" onclick={() => clickFile(f)}>
                  <span class="file-icon">{f.type === 'dir' ? '📁' : '📄'}</span>
                  <span class="file-name">{f.name}</span>
                </button>
                <span class="file-size">{f.type === 'dir' ? '' : formatSize(f.size)}</span>
                {#if f.type !== 'dir'}
                  <button class="file-action" title="Download" onclick={() => downloadFile(f)}>⬇</button>
                  <button class="file-action file-action--del" title="Delete" onclick={() => deleteFile(f)}>✕</button>
                {/if}
              </div>
            {/each}
          {/if}
        </div>

        <label class="upload-btn">
          + Upload
          <input type="file" multiple onchange={uploadViaInput} hidden />
        </label>

        <!-- File preview -->
        {#if selectedFile}
          <div class="preview">
            <div class="preview__header">
              <span>{selectedFile.name}</span>
              <button class="file-action" onclick={() => { selectedFile = null; filePreview = ''; }}>✕</button>
            </div>
            <div class="preview__body">
              {#if filePreviewType === 'text'}
                <pre>{filePreview || 'Loading...'}</pre>
              {:else if filePreviewType === 'image'}
                <img src={filePreview} alt={selectedFile.name} />
              {:else}
                <p class="file-empty">Binary file — click ⬇ to download</p>
              {/if}
            </div>
          </div>
        {/if}
      </div>

      <!-- RIGHT: Terminal + Storage -->
      <div class="sb-right">
        <div class="sb-right-top">
          <div class="sb-section-title">
            Terminal
            <button class="btn-sm btn-ghost" onclick={clearTerminal}>Clear</button>
          </div>
          <div class="terminal" bind:this={termScroll}>
            <pre class="terminal__out">{terminalOutput || '$ Sandbox ready. Type a command.\n'}</pre>
          </div>
          <div class="terminal__input-row">
            <span class="terminal__prompt">$</span>
            <input
              type="text"
              bind:value={terminalInput}
              onkeydown={handleTermKeydown}
              disabled={execBusy}
              placeholder="ls -la /sandbox/files"
            />
            {#if execBusy}<span class="terminal__busy">⏳</span>{/if}
          </div>
        </div>

        <div class="sb-right-bottom">
          <div class="sb-section-title">Storage</div>
          {#if vpcStorage}
            <div class="storage-grid">
              {#each vpcStorage.volumes as vol}
                {@const pct = (vol.used_mb / vol.quota_mb) * 100}
                <div class="storage-item">
                  <div class="storage-item__label">{vol.name}</div>
                  <div class="storage-item__bar">
                    <div class="storage-item__fill" style="width: {Math.min(100, pct)}%; background: {storageColor(pct)}"></div>
                  </div>
                  <div class="storage-item__value">{vol.used_mb} / {vol.quota_mb} MB</div>
                </div>
              {/each}
            </div>
          {:else}
            <div class="file-empty">Loading storage...</div>
          {/if}
        </div>
      </div>
    </div>
  {:else}
    <div class="sb-placeholder">
      <p>Alpine Linux Sandbox</p>
      <span>Terminal · Files · Storage</span>
      <span>bash, curl, python3, jq, sqlite3, git, vim, tmux...</span>
    </div>
  {/if}
</div>

<style>
  .sandbox { display: flex; flex-direction: column; height: 100%; background: #fff; overflow: hidden; }

  .sb-header { display: flex; align-items: center; justify-content: space-between; padding: 8px 16px; border-bottom: 1px solid #dfe1e5; flex-shrink: 0; }
  .sb-header__left { display: flex; align-items: baseline; gap: 8px; }
  .sb-header__left h3 { margin: 0; font-size: 15px; font-weight: 600; color: #202124; }
  .sb-header__sub { font-size: 11px; color: #80868b; }
  .sb-header__right { display: flex; align-items: center; gap: 8px; }
  .sb-header__status { font-size: 12px; color: #5f6368; font-weight: 500; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: #bdc1c6; }
  .dot.on { background: #1e8e3e; }
  .dot.off { border: 2px solid #80868b; box-sizing: border-box; }
  .dot.load { background: #202124; animation: pulse 1.2s ease-in-out infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.3} }

  .btn-sm { padding: 4px 10px; border: none; border-radius: 4px; font-size: 12px; font-weight: 600; cursor: pointer; }
  .btn-dark { background: #202124; color: #fff; }
  .btn-dark:hover:not(:disabled) { background: #3c4043; }
  .btn-dark:disabled { opacity: .5; cursor: not-allowed; }
  .btn-ghost { background: transparent; color: #5f6368; border: 1px solid #dfe1e5; }
  .btn-ghost:hover { background: #f1f3f4; }

  .sb-error { padding: 6px 16px; font-size: 12px; color: #d93025; background: #fce8e6; }
  .sb-status { padding: 6px 16px; font-size: 12px; color: #5f6368; }

  .sb-body { flex: 1; min-height: 0; display: flex; gap: 1px; background: #dfe1e5; overflow: hidden; }
  .sb-left { flex: 0 0 42%; min-width: 0; background: #fff; display: flex; flex-direction: column; overflow: hidden; }
  .sb-right { flex: 1; min-width: 0; background: #fff; display: flex; flex-direction: column; overflow: hidden; }
  .sb-right-top { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
  .sb-right-bottom { flex: 0 0 auto; max-height: 220px; overflow-y: auto; border-top: 1px solid #dfe1e5; }

  .sb-section-title { padding: 6px 12px; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; color: #80868b; border-bottom: 1px solid #f1f3f4; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; }

  .dir-tabs { display: flex; gap: 2px; padding: 4px 8px; flex-shrink: 0; }
  .dir-tab { padding: 3px 8px; border: 1px solid #dfe1e5; border-radius: 3px; background: #fff; font-size: 11px; color: #5f6368; cursor: pointer; }
  .dir-tab.active { background: #202124; color: #fff; border-color: #202124; }

  .breadcrumb { padding: 2px 12px; font-size: 11px; color: #80868b; font-family: monospace; flex-shrink: 0; }

  .file-drop-zone { flex: 1; min-height: 0; overflow-y: auto; border: 2px dashed transparent; }
  .file-drop-zone.dragover { border-color: #202124; background: #f8f9fa; }

  .file-empty { padding: 16px; text-align: center; color: #80868b; font-size: 12px; }

  .file-row { display: flex; align-items: center; gap: 4px; padding: 4px 8px; border-bottom: 1px solid #f8f9fa; }
  .file-row:hover { background: #f1f3f4; }
  .file-row.selected { background: #e8f0fe; }
  .file-row__main { flex: 1; min-width: 0; display: flex; align-items: center; gap: 6px; border: none; background: transparent; cursor: pointer; text-align: left; padding: 0; }
  .file-icon { flex-shrink: 0; font-size: 14px; }
  .file-name { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-size: 12px; color: #202124; }
  .file-size { flex-shrink: 0; font-size: 11px; color: #80868b; font-variant-numeric: tabular-nums; }
  .file-action { flex-shrink: 0; border: none; background: transparent; cursor: pointer; font-size: 12px; color: #5f6368; padding: 2px 4px; border-radius: 3px; }
  .file-action:hover { background: #e8eaed; }
  .file-action--del:hover { color: #d93025; }

  .upload-btn { display: block; padding: 6px 12px; margin: 4px 8px; text-align: center; border: 1px solid #dfe1e5; border-radius: 4px; font-size: 12px; font-weight: 600; color: #5f6368; cursor: pointer; }
  .upload-btn:hover { background: #f1f3f4; }

  .preview { border-top: 1px solid #dfe1e5; flex-shrink: 0; max-height: 200px; display: flex; flex-direction: column; }
  .preview__header { display: flex; align-items: center; justify-content: space-between; padding: 4px 12px; background: #f8f9fa; font-size: 12px; font-weight: 600; color: #202124; }
  .preview__body { flex: 1; overflow: auto; padding: 8px 12px; }
  .preview__body pre { margin: 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; font-family: monospace; color: #202124; }
  .preview__body img { max-width: 100%; max-height: 160px; object-fit: contain; }

  .terminal { flex: 1; min-height: 0; overflow-y: auto; background: #1a1a1a; padding: 8px 12px; }
  .terminal__out { margin: 0; font-size: 12px; font-family: 'SF Mono', Menlo, monospace; color: #e0e0e0; white-space: pre-wrap; word-break: break-all; line-height: 1.4; }
  .terminal__input-row { display: flex; align-items: center; gap: 6px; padding: 6px 12px; background: #1a1a1a; border-top: 1px solid #333; }
  .terminal__prompt { color: #4fc3f7; font-family: monospace; font-size: 13px; flex-shrink: 0; }
  .terminal__input-row input { flex: 1; background: transparent; border: none; color: #e0e0e0; font-family: 'SF Mono', Menlo, monospace; font-size: 13px; outline: none; }
  .terminal__busy { font-size: 12px; }

  .storage-grid { padding: 8px 12px; display: flex; flex-direction: column; gap: 6px; }
  .storage-item { display: flex; align-items: center; gap: 8px; }
  .storage-item__label { flex: 0 0 120px; font-size: 11px; font-weight: 600; color: #5f6368; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .storage-item__bar { flex: 1; height: 5px; background: #e8eaed; border-radius: 3px; overflow: hidden; }
  .storage-item__fill { height: 100%; border-radius: 3px; transition: width .3s, background .3s; }
  .storage-item__value { flex: 0 0 auto; font-size: 10px; color: #80868b; font-variant-numeric: tabular-nums; }

  .sb-placeholder { flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 6px; color: #80868b; }
  .sb-placeholder p { margin: 0; font-size: 15px; font-weight: 600; color: #5f6368; }
  .sb-placeholder span { font-size: 13px; }
</style>
