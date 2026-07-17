<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import FileTree from './FileTree.svelte';
  import type { TreeNode } from './FileTreeNode.svelte';

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

  let fileTree = $state<{ refresh: () => Promise<void> } | undefined>(undefined);

  onMount(() => { fetchStatus(); pollInterval = setInterval(fetchStatus, 8000); });
  onDestroy(() => { if (pollInterval) clearInterval(pollInterval); });

  async function fetchStatus() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'status' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      if (res.ok) {
        const data: any = await res.json();
        const wasRunning = sandboxRunning;
        sandboxRunning = data.running;
        if (data.error) errorMsg = data.error;
        if (sandboxRunning && !wasRunning) { fileTree?.refresh(); loadStorage(); }
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
      if (res.ok) { sandboxRunning = true; statusMsg = 'Sandbox ready'; fileTree?.refresh(); loadStorage(); }
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
      if (res.ok) {
        sandboxRunning = false; terminalOutput = ''; vpcStorage = null;
        selectedNode = null; filePreview = ''; shellCwd = '/sandbox';
        statusMsg = 'Stopped';
      } else { errorMsg = data.error || 'Failed'; statusMsg = ''; }
    } catch (err: any) { errorMsg = err.message; statusMsg = ''; }
    finally { isLoading = false; }
  }

  // ─── Persistent shell terminal (shared with kāraka via sandbox.shell) ───
  const SHELL_SESSION = 'main';
  let terminalInput = $state('');
  let terminalOutput = $state('');
  let shellCwd = $state('/sandbox');
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
    terminalOutput += `${shortCwd(shellCwd)} $ ${cmd}\n`;
    execBusy = true;
    await tick();
    scrollTerm();
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'shell' });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: cmd, session_id: SHELL_SESSION, timeout: 120 })
      });
      const data: any = await res.json().catch(() => ({}));
      if (data.output) terminalOutput += data.output + '\n';
      if (data.cwd) shellCwd = data.cwd;
      if (data.error) {
        terminalOutput += `[${data.error}]${data.note ? ' ' + data.note : ''}\n`;
        if (data.error === 'session_dead') terminalOutput += '(fresh shell will start on next command)\n';
      } else if (typeof data.exit_code === 'number' && data.exit_code !== 0) {
        terminalOutput += `(exit ${data.exit_code})\n`;
      }
      // A command may have created/changed files — refresh tree quietly.
      fileTree?.refresh();
    } catch (err: any) { terminalOutput += `Error: ${err.message}\n`; }
    finally { execBusy = false; await tick(); scrollTerm(); }
  }

  function shortCwd(cwd: string): string {
    return cwd.replace(/^\/sandbox\/?/, '~/').replace(/^~$/, '~');
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

  // ─── Tree selection & file preview ───
  let selectedNode: TreeNode | null = $state(null);
  let filePreview: string = $state('');
  let filePreviewType: 'text' | 'image' | 'binary' = $state('binary');

  async function handleSelect(node: TreeNode) {
    selectedNode = node;
    if (node.type === 'dir') { selectedFileReset(); return; }
    filePreview = ''; filePreviewType = 'binary';
    const ext = node.name.split('.').pop()?.toLowerCase() || '';
    if (['txt', 'md', 'json', 'js', 'ts', 'py', 'sh', 'yml', 'yaml', 'csv', 'xml', 'html', 'css', 'log', 'conf'].includes(ext)) {
      filePreviewType = 'text';
      try {
        const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'file', path: node.path });
        const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
        if (res.ok) filePreview = await res.text();
      } catch { filePreview = 'Failed to load'; }
    } else if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp'].includes(ext)) {
      filePreviewType = 'image';
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'file', path: node.path });
      filePreview = `/api/proxy/sandbox?${params.toString()}`;
    }
  }

  function selectedFileReset() { filePreview = ''; filePreviewType = 'binary'; }

  async function handleDelete(node: TreeNode) {
    if (!confirm(`Delete ${node.path}?`)) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, path: node.path });
      await fetch(`/api/proxy/sandbox?${params.toString()}`, { method: 'DELETE' });
      fileTree?.refresh();
      if (selectedNode?.path === node.path) { selectedNode = null; selectedFileReset(); }
      loadStorage();
    } catch {}
  }

  async function handleDownload(node: TreeNode) {
    const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'file', path: node.path, download: '1' });
    const a = document.createElement('a');
    a.href = `/api/proxy/sandbox?${params.toString()}`;
    a.download = node.name;
    a.click();
  }

  // Upload target: selected dir, or parent dir of selected file, fallback /files.
  function uploadDir(): string {
    if (!selectedNode) return '/files';
    if (selectedNode.type === 'dir') return selectedNode.path;
    return selectedNode.path.split('/').slice(0, -1).join('/') || '/files';
  }

  async function uploadFiles(list: FileList | File[]) {
    for (const f of Array.from(list)) {
      const path = `${uploadDir()}/${f.name}`;
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, path });
      await fetch(`/api/proxy/sandbox?${params.toString()}`, {
        method: 'PUT',
        headers: { 'Content-Type': f.type || 'application/octet-stream' },
        body: f
      });
    }
    fileTree?.refresh();
    loadStorage();
  }

  async function uploadViaInput(e: Event) {
    const target = e.target as HTMLInputElement;
    if (target.files) await uploadFiles(target.files);
    target.value = '';
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
      <!-- LEFT: File tree -->
      <div class="sb-left">
        <FileTree
          bind:this={fileTree}
          {vpcUrl}
          {sessionToken}
          running={sandboxRunning}
          selectedPath={selectedNode?.path ?? ''}
          onselect={handleSelect}
          ondownload={handleDownload}
          ondelete={handleDelete}
          ondropfiles={(files) => uploadFiles(files)}
        />

        <div class="sb-left__footer">
          <label class="upload-btn" title="Upload to {uploadDir()}">
            + Upload → {uploadDir()}
            <input type="file" multiple onchange={uploadViaInput} hidden />
          </label>
        </div>

        <!-- File preview -->
        {#if selectedNode && selectedNode.type === 'file' && filePreviewType !== 'binary'}
          <div class="preview">
            <div class="preview__header">
              <span class="preview__name">{selectedNode.path}</span>
              <button class="file-action" onclick={() => { selectedNode = null; selectedFileReset(); }}>✕</button>
            </div>
            <div class="preview__body">
              {#if filePreviewType === 'text'}
                <pre>{filePreview || 'Loading...'}</pre>
              {:else if filePreviewType === 'image'}
                <img src={filePreview} alt={selectedNode.name} />
              {/if}
            </div>
          </div>
        {/if}
      </div>

      <!-- RIGHT: Terminal + Storage -->
      <div class="sb-right">
        <div class="sb-right-top">
          <div class="sb-section-title">
            <span>Terminal <span class="sb-session-tag">session "{SHELL_SESSION}" · shared with kāraka</span></span>
            <button class="btn-sm btn-ghost" onclick={clearTerminal}>Clear</button>
          </div>
          <div class="terminal" bind:this={termScroll}>
            <pre class="terminal__out">{terminalOutput || `$ Persistent bash (session "${SHELL_SESSION}") — cwd & env survive between commands.\n$ Kāraka reach this same shell via sandbox.shell.\n`}</pre>
          </div>
          <div class="terminal__input-row">
            <span class="terminal__prompt">{shortCwd(shellCwd)} $</span>
            <input
              type="text"
              bind:value={terminalInput}
              onkeydown={handleTermKeydown}
              disabled={execBusy}
              placeholder="curl -s https://example.com | jq ."
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
      <span>Persistent shell · File tree · Storage</span>
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
  .sb-left { flex: 0 0 44%; min-width: 0; background: #fff; display: flex; flex-direction: column; overflow: hidden; }
  .sb-left__footer { flex-shrink: 0; border-top: 1px solid #f1f3f4; }
  .sb-right { flex: 1; min-width: 0; background: #fff; display: flex; flex-direction: column; overflow: hidden; }
  .sb-right-top { flex: 1; min-height: 0; display: flex; flex-direction: column; overflow: hidden; }
  .sb-right-bottom { flex: 0 0 auto; max-height: 220px; overflow-y: auto; border-top: 1px solid #dfe1e5; }

  .sb-section-title { padding: 6px 12px; font-size: 11px; font-weight: 700; text-transform: uppercase; letter-spacing: .04em; color: #80868b; border-bottom: 1px solid #f1f3f4; display: flex; align-items: center; justify-content: space-between; flex-shrink: 0; }
  .sb-session-tag { font-weight: 400; text-transform: none; letter-spacing: 0; color: #9aa0a6; font-size: 10px; margin-left: 6px; }

  .file-empty { padding: 16px; text-align: center; color: #80868b; font-size: 12px; }
  .file-action { flex-shrink: 0; border: none; background: transparent; cursor: pointer; font-size: 12px; color: #5f6368; padding: 2px 4px; border-radius: 3px; }
  .file-action:hover { background: #e8eaed; }

  .upload-btn { display: block; padding: 6px 12px; margin: 4px 8px; text-align: center; border: 1px solid #dfe1e5; border-radius: 4px; font-size: 11px; font-weight: 600; color: #5f6368; cursor: pointer; font-family: 'SF Mono', Menlo, monospace; }
  .upload-btn:hover { background: #f1f3f4; }

  .preview { border-top: 1px solid #dfe1e5; flex-shrink: 0; max-height: 200px; display: flex; flex-direction: column; }
  .preview__header { display: flex; align-items: center; justify-content: space-between; padding: 4px 12px; background: #f8f9fa; }
  .preview__name { font-size: 11px; font-weight: 600; color: #202124; font-family: 'SF Mono', Menlo, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .preview__body { flex: 1; overflow: auto; padding: 8px 12px; }
  .preview__body pre { margin: 0; font-size: 11px; white-space: pre-wrap; word-break: break-all; font-family: 'SF Mono', Menlo, monospace; color: #202124; }
  .preview__body img { max-width: 100%; max-height: 160px; object-fit: contain; }

  .terminal { flex: 1; min-height: 0; overflow-y: auto; background: #1a1a1a; padding: 8px 12px; }
  .terminal__out { margin: 0; font-size: 12px; font-family: 'SF Mono', Menlo, monospace; color: #e0e0e0; white-space: pre-wrap; word-break: break-all; line-height: 1.4; }
  .terminal__input-row { display: flex; align-items: center; gap: 6px; padding: 6px 12px; background: #1a1a1a; border-top: 1px solid #333; }
  .terminal__prompt { color: #4fc3f7; font-family: 'SF Mono', Menlo, monospace; font-size: 12px; flex-shrink: 0; }
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
