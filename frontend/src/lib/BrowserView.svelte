<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let {
    vpcUrl = '',
    sessionToken = ''
  }: {
    vpcUrl: string;
    sessionToken: string;
  } = $props();

  let canvas: HTMLCanvasElement;
  let browserRunning = $state(false);
  let isLoading = $state(false);
  let errorMsg = $state('');
  let statusMsg = $state('');
  let connected = $state(false);
  let streamWidth = $state(1280);
  let streamHeight = $state(720);
  let browserEngine = $state<'firefox' | 'chromium'>('firefox');
  let browserMode = $state<'main' | 'agent'>('main');

  let streamAbort: AbortController | null = null;
  let pollInterval: ReturnType<typeof setInterval> | null = null;
  let pendingFrame: Uint8Array | null = null;
  let drawing = false;
  let lastMoveAt = 0;
  let queuedMove: { x: number; y: number } | null = null;
  let inputBusy = false;
  let inputQueue: any[] = [];

  onMount(() => {
    fetchStatus();
    pollInterval = setInterval(fetchStatus, 8000);
  });

  onDestroy(() => {
    disconnect();
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
        if (browserRunning && !connected) {
          connectToStream();
        }
      }
    } catch {
    }
  }

  async function wakeBrowser() {
    if (!vpcUrl || !sessionToken) return;
    isLoading = true;
    errorMsg = '';
    statusMsg = `Starting ${browserEngine === 'chromium' ? 'Chromium' : 'Firefox'} ${browserMode}...`;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
      if (browserMode !== 'main') params.set('mode', browserMode);
      if (browserEngine !== 'firefox') params.set('engine', browserEngine);
      const res = await fetch(`/api/proxy/browser?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) {
        browserRunning = true;
        statusMsg = 'Browser ready';
        setTimeout(() => connectToStream(), 500);
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
    disconnect();
    isLoading = true;
    errorMsg = '';
    statusMsg = 'Stopping browser...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'stop' });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`, { method: 'POST' });
      const data: any = await res.json();
      if (res.ok) {
        browserRunning = false;
        connected = false;
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

  async function connectToStream() {
    if (!vpcUrl || !sessionToken || !browserRunning) return;

    errorMsg = '';
    streamAbort = new AbortController();

    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'stream' });
      const response = await fetch(`/api/proxy/browser?${params.toString()}`, {
        signal: streamAbort.signal
      });

      if (!response.ok) {
        errorMsg = 'Stream failed: ' + response.status;
        return;
      }

      const w = parseInt(response.headers.get('X-Browser-Width') || '1280');
      const h = parseInt(response.headers.get('X-Browser-Height') || '720');
      streamWidth = w;
      streamHeight = h;
      if (canvas) {
        canvas.width = w;
        canvas.height = h;
      }

      connected = true;
      statusMsg = '';

      const reader = response.body?.getReader();
      if (!reader) {
        errorMsg = 'Streaming not supported';
        return;
      }

      let buffer = new Uint8Array(65536);
      let bufferLen = 0;

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        if (value) {
          if (bufferLen + value.length > buffer.length) {
            const grown = new Uint8Array(bufferLen + value.length + 32768);
            grown.set(buffer.subarray(0, bufferLen));
            buffer = grown;
          }
          buffer.set(value, bufferLen);
          bufferLen += value.length;
        }

        while (bufferLen >= 4) {
          const len = new DataView(
            buffer.buffer,
            buffer.byteOffset,
            bufferLen
          ).getUint32(0, false);

          if (bufferLen >= 4 + len) {
            const jpegData = buffer.slice(4, 4 + len);
            bufferLen -= 4 + len;
            if (bufferLen > 0) {
              buffer.copyWithin(0, 4 + len, 4 + len + bufferLen);
            }
            pendingFrame = jpegData;
            if (!drawing) requestAnimationFrame(() => void pumpFrames());
          } else {
            break;
          }
        }
      }

      connected = false;
    } catch (err: any) {
      if (err.name !== 'AbortError') {
        errorMsg = 'Stream error: ' + err.message;
        connected = false;
      }
    }
  }

  function disconnect() {
    if (streamAbort) {
      streamAbort.abort();
      streamAbort = null;
    }
    connected = false;
    pendingFrame = null;
    inputQueue = [];
    queuedMove = null;
  }

  async function pumpFrames() {
    if (drawing || !canvas) return;
    drawing = true;
    const ctx = canvas.getContext('2d', { alpha: false });
    try {
      while (pendingFrame && ctx) {
        const jpegData = pendingFrame;
        pendingFrame = null;
        const blob = new Blob([jpegData], { type: 'image/jpeg' });
        const bitmap = await createImageBitmap(blob);
        ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height);
        bitmap.close();
      }
    } catch {
      /* ignore decode errors */
    } finally {
      drawing = false;
      if (pendingFrame) requestAnimationFrame(() => void pumpFrames());
    }
  }

  async function flushInputQueue() {
    if (inputBusy) return;
    inputBusy = true;
    try {
      while (inputQueue.length || queuedMove) {
        if (queuedMove) {
          const m = queuedMove;
          queuedMove = null;
          inputQueue.push({ type: 'mouse_move', x: m.x, y: m.y });
        }
        const event = inputQueue.shift();
        if (!event) break;
        const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'input' });
        await fetch(`/api/proxy/browser?${params.toString()}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(event)
        });
      }
    } catch {
    } finally {
      inputBusy = false;
      if (inputQueue.length || queuedMove) void flushInputQueue();
    }
  }

  function sendInput(event: any) {
    if (!vpcUrl || !sessionToken || !connected) return;
    if (event.type === 'mouse_move') {
      queuedMove = { x: event.x, y: event.y };
    } else {
      inputQueue.push(event);
    }
    void flushInputQueue();
  }

  function getCanvasCoords(e: MouseEvent): { x: number; y: number } {
    if (!canvas) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    const canvasAspect = canvas.width / canvas.height;
    const rectAspect = rect.width / rect.height;

    // object-fit: contain → image is letterboxed inside the element box
    let displayW: number;
    let displayH: number;
    let offsetX: number;
    let offsetY: number;
    if (canvasAspect > rectAspect) {
      displayW = rect.width;
      displayH = rect.width / canvasAspect;
      offsetX = 0;
      offsetY = (rect.height - displayH) / 2;
    } else {
      displayH = rect.height;
      displayW = rect.height * canvasAspect;
      offsetX = (rect.width - displayW) / 2;
      offsetY = 0;
    }

    const localX = e.clientX - rect.left - offsetX;
    const localY = e.clientY - rect.top - offsetY;
    const x = Math.round((localX / displayW) * canvas.width);
    const y = Math.round((localY / displayH) * canvas.height);
    return {
      x: Math.max(0, Math.min(canvas.width - 1, x)),
      y: Math.max(0, Math.min(canvas.height - 1, y))
    };
  }

  function handleMouseDown(e: MouseEvent) {
    canvas?.focus();
    const { x, y } = getCanvasCoords(e);
    sendInput({ type: 'mouse_move', x, y });
    sendInput({ type: 'mouse_down', button: e.button + 1 });
  }

  function handleMouseUp(e: MouseEvent) {
    sendInput({ type: 'mouse_up', button: e.button + 1 });
  }

  function handleDblClick(e: MouseEvent) {
    const { x, y } = getCanvasCoords(e);
    sendInput({ type: 'mouse_move', x, y });
    sendInput({ type: 'mouse_click', button: 1, x, y });
    sendInput({ type: 'mouse_click', button: 1, x, y });
  }

  function handleContextMenu(e: MouseEvent) {
    e.preventDefault();
  }

  function handleMouseMove(e: MouseEvent) {
    if (e.buttons === 0) return;
    const now = performance.now();
    if (now - lastMoveAt < 20) {
      const { x, y } = getCanvasCoords(e);
      queuedMove = { x, y };
      return;
    }
    lastMoveAt = now;
    const { x, y } = getCanvasCoords(e);
    sendInput({ type: 'mouse_move', x, y });
  }

  function handleWheel(e: WheelEvent) {
    e.preventDefault();
    sendInput({ type: 'scroll', dy: e.deltaY });
  }

  function handleKeydown(e: KeyboardEvent) {
    e.preventDefault();
    let key = '';
    if (e.key === 'Enter') key = 'Return';
    else if (e.key === 'Backspace') key = 'BackSpace';
    else if (e.key === 'Tab') key = 'Tab';
    else if (e.key === 'Escape') key = 'Escape';
    else if (e.key === ' ') key = 'space';
    else if (e.key.startsWith('Arrow')) key = e.key.replace('Arrow', '');
    else if (e.key === 'Control') key = 'Control_L';
    else if (e.key === 'Shift') key = 'Shift_L';
    else if (e.key === 'Alt') key = 'Alt_L';
    else if (e.key === 'Meta') key = 'Super_L';
    else if (e.key === 'Delete') key = 'Delete';
    else if (e.key === 'Home') key = 'Home';
    else if (e.key === 'End') key = 'End';
    else if (e.key === 'PageUp') key = 'Page_Up';
    else if (e.key === 'PageDown') key = 'Page_Down';
    else if (e.key === 'Insert') key = 'Insert';
    else if (e.key.length === 1) key = e.key;
    else return;

    sendInput({ type: 'key', key });
  }

  // ─── Khadyota console (read the web as text, drive Firefox) ───
  let khadyotaOpen = $state(false);
  let khadyotaUrl = $state('');
  let khadyotaBusy = $state(false);
  let khadyotaResult: any = $state(null);
  let khadyotaError = $state('');
  let windowTitle = $state('');
  let khadyotaTab: 'text' | 'links' = $state('text');

  async function khadyotaFetchWindow() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'window' });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`);
      if (res.ok) { const d: any = await res.json(); windowTitle = d.title || ''; }
    } catch {}
  }

  async function khadyotaNavigate() {
    if (!khadyotaUrl.trim() || khadyotaBusy) return;
    khadyotaBusy = true; khadyotaError = '';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'navigate' });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`, {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: khadyotaUrl.trim() })
      });
      const data: any = await res.json();
      if (!res.ok || data.error || data.ok === false) khadyotaError = data.error || 'navigate failed';
      else setTimeout(khadyotaFetchWindow, 1500);
    } catch (e: any) { khadyotaError = e.message; }
    finally { khadyotaBusy = false; }
  }

  async function khadyotaFetch() {
    if (!khadyotaUrl.trim() || khadyotaBusy) return;
    khadyotaBusy = true; khadyotaError = ''; khadyotaResult = null;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'fetch', url: khadyotaUrl.trim() });
      const res = await fetch(`/api/proxy/browser?${params.toString()}`);
      const data: any = await res.json();
      if (!res.ok || data.error) khadyotaError = data.error || 'fetch failed';
      else { khadyotaResult = data; khadyotaTab = 'text'; }
    } catch (e: any) { khadyotaError = e.message; }
    finally { khadyotaBusy = false; }
  }

  function handleKhadyotaKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') { e.preventDefault(); khadyotaFetch(); }
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
        {browserRunning ? (connected ? 'Streaming' : 'Running') : isLoading ? 'Loading...' : 'Stopped'}
      </span>
    </div>
  </header>

  <div class="browser-view__controls">
    {#if !browserRunning}
      <div class="browser-selects">
        <select bind:value={browserEngine}>
          <option value="firefox">Firefox</option>
          <option value="chromium">Chromium</option>
        </select>
        <select bind:value={browserMode}>
          <option value="main">GUI (main)</option>
          <option value="agent">Headless (agent)</option>
        </select>
      </div>
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
      <canvas
        bind:this={canvas}
        onmousedown={handleMouseDown}
        onmouseup={handleMouseUp}
        ondblclick={handleDblClick}
        oncontextmenu={handleContextMenu}
        onmousemove={handleMouseMove}
        onwheel={handleWheel}
        onkeydown={handleKeydown}
        tabindex="0"
        class="browser-canvas"
      ></canvas>
    {:else}
      <div class="browser-view__placeholder">
        <p>Firefox ESR on VPC</p>
        <span>Click "Wake Browser" to start</span>
      </div>
    {/if}
  </div>

  {#if browserRunning}
    <div class="khadyota">
      <button class="khadyota__toggle" onclick={() => { khadyotaOpen = !khadyotaOpen; if (khadyotaOpen) khadyotaFetchWindow(); }}>
        <span class="khadyota__chevron">{khadyotaOpen ? '▾' : '▸'}</span>
        Khadyota console
        <span class="khadyota__hint">Khadyota — read the web as text</span>
        {#if windowTitle && khadyotaOpen}
          <span class="khadyota__window">· {windowTitle}</span>
        {/if}
      </button>

      {#if khadyotaOpen}
        <div class="khadyota__body">
          <div class="khadyota__bar">
            <input
              class="khadyota__url"
              type="text"
              bind:value={khadyotaUrl}
              onkeydown={handleKhadyotaKeydown}
              placeholder="https://example.com"
              disabled={khadyotaBusy}
            />
            <button class="btn btn--ghost btn--sm" onclick={khadyotaNavigate} disabled={khadyotaBusy || !khadyotaUrl.trim()} title="Drive the visible Firefox to this URL">
              Navigate
            </button>
            <button class="btn btn--primary btn--sm" onclick={khadyotaFetch} disabled={khadyotaBusy || !khadyotaUrl.trim()} title="Fetch the page and read it as text">
              {khadyotaBusy ? '…' : 'Fetch'}
            </button>
            <button class="btn btn--ghost btn--sm" onclick={khadyotaFetchWindow} title="Refresh current window title">
              ⌂
            </button>
          </div>

          {#if khadyotaError}
            <div class="khadyota__error">{khadyotaError}</div>
          {/if}

          {#if khadyotaResult}
            <div class="khadyota__result">
              <div class="khadyota__result-head">
                <div class="khadyota__result-title">
                  {khadyotaResult.title || '(no title)'}
                  <span class="khadyota__result-url">{khadyotaResult.final_url}</span>
                </div>
                <div class="khadyota__tabs">
                  <button class="khadyota__tab" class:active={khadyotaTab === 'text'} onclick={() => (khadyotaTab = 'text')}>text</button>
                  <button class="khadyota__tab" class:active={khadyotaTab === 'links'} onclick={() => (khadyotaTab = 'links')}>
                    links ({(khadyotaResult.links ?? []).length})
                  </button>
                </div>
              </div>
              {#if khadyotaTab === 'text'}
                <pre class="khadyota__text">{khadyotaResult.text || '(empty page text)'}</pre>
              {:else}
                <div class="khadyota__links">
                  {#each khadyotaResult.links ?? [] as link}
                    <button class="khadyota__link" title={link.href} onclick={() => { khadyotaUrl = link.href; }}>
                      <span class="khadyota__link-text">{link.text}</span>
                      <span class="khadyota__link-href">{link.href}</span>
                    </button>
                  {:else}
                    <div class="khadyota__empty">No links found</div>
                  {/each}
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
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
    padding: 10px 16px;
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
    padding: 8px 16px;
    border-bottom: 1px solid #e8eaed;
    flex-shrink: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  .browser-selects {
    display: flex;
    gap: 6px;
  }

  .browser-selects select {
    padding: 6px 8px;
    border: 1px solid #dadce0;
    border-radius: 4px;
    font-size: 12px;
    background: #fff;
    color: #202124;
    cursor: pointer;
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
    background: #1a1a1a;
    display: flex;
    align-items: center;
    justify-content: center;
  }

  .browser-canvas {
    width: 100%;
    height: 100%;
    object-fit: contain;
    cursor: crosshair;
    outline: none;
    background: #111;
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

  /* ─── Khadyota console ─── */
  .khadyota {
    flex-shrink: 0;
    border-top: 1px solid #dfe1e5;
    background: #fff;
    display: flex;
    flex-direction: column;
    max-height: 45%;
  }

  .khadyota__toggle {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 6px 16px;
    border: none;
    background: #f8f9fa;
    cursor: pointer;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #5f6368;
    text-align: left;
  }
  .khadyota__toggle:hover { background: #f1f3f4; }
  .khadyota__chevron { font-size: 10px; color: #80868b; }
  .khadyota__hint { font-weight: 400; text-transform: none; letter-spacing: 0; color: #9aa0a6; }
  .khadyota__window {
    font-weight: 400;
    text-transform: none;
    letter-spacing: 0;
    color: #80868b;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    max-width: 40%;
  }

  .khadyota__body {
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
  }

  .khadyota__bar {
    display: flex;
    gap: 6px;
    padding: 8px 16px;
    border-bottom: 1px solid #f1f3f4;
    flex-shrink: 0;
  }
  .khadyota__url {
    flex: 1;
    min-width: 0;
    border: 1px solid #dfe1e5;
    border-radius: 4px;
    padding: 5px 10px;
    font-size: 12px;
    font-family: 'SF Mono', Menlo, monospace;
    color: #202124;
    outline: none;
  }
  .khadyota__url:focus { border-color: #202124; }

  .btn--sm { padding: 5px 12px; font-size: 12px; }

  .khadyota__error {
    padding: 6px 16px;
    font-size: 12px;
    color: #d93025;
    background: #fce8e6;
    flex-shrink: 0;
  }

  .khadyota__result {
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow: hidden;
  }
  .khadyota__result-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 6px 16px;
    border-bottom: 1px solid #f1f3f4;
    flex-shrink: 0;
  }
  .khadyota__result-title {
    font-size: 12px;
    font-weight: 600;
    color: #202124;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    min-width: 0;
  }
  .khadyota__result-url {
    font-weight: 400;
    font-size: 11px;
    color: #9aa0a6;
    font-family: 'SF Mono', Menlo, monospace;
    margin-left: 6px;
  }
  .khadyota__tabs { display: flex; gap: 4px; flex-shrink: 0; }
  .khadyota__tab {
    border: 1px solid #dfe1e5;
    background: #fff;
    border-radius: 3px;
    font-size: 11px;
    font-family: 'SF Mono', Menlo, monospace;
    color: #5f6368;
    cursor: pointer;
    padding: 2px 8px;
  }
  .khadyota__tab.active { background: #202124; color: #fff; border-color: #202124; }

  .khadyota__text {
    flex: 1;
    min-height: 120px;
    overflow: auto;
    margin: 0;
    padding: 10px 16px;
    font-family: 'SF Mono', Menlo, monospace;
    font-size: 11.5px;
    line-height: 1.5;
    color: #202124;
    white-space: pre-wrap;
    word-break: break-word;
    background: #fafbfc;
  }

  .khadyota__links {
    flex: 1;
    min-height: 120px;
    overflow: auto;
    padding: 4px 8px;
    display: flex;
    flex-direction: column;
  }
  .khadyota__link {
    display: flex;
    gap: 8px;
    align-items: baseline;
    border: none;
    background: transparent;
    text-align: left;
    padding: 3px 8px;
    border-radius: 3px;
    cursor: pointer;
  }
  .khadyota__link:hover { background: #f1f3f4; }
  .khadyota__link-text { font-size: 12px; color: #1a73e8; flex-shrink: 0; max-width: 40%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .khadyota__link-href { font-size: 11px; color: #9aa0a6; font-family: 'SF Mono', Menlo, monospace; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .khadyota__empty { padding: 16px; text-align: center; color: #9aa0a6; font-size: 12px; }
</style>
