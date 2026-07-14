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
    statusMsg = 'Starting browser...';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
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
</style>
