<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  // Props
  let { vpcUrl = '', sessionToken = '' } = $props();

  // State
  let canvas: HTMLCanvasElement;
  let canvasCtx: CanvasRenderingContext2D | null = null;
  let wsVideo: WebSocket | null = null;
  let connected = $state(false);
  let deviceInfo = $state<{ name: string; width: number; height: number; rotation: number } | null>(null);
  let error = $state('');
  let decoder: any = null;
  let frameCount = $state(0);
  let isFullscreen = $state(false);

  // Message types matching vpc-relay/scrcpy_hub.go
  const MSG_TYPE_VIDEO = 0x01;
  const MSG_TYPE_INPUT = 0x02;
  const MSG_TYPE_DEVICE_INFO = 0x03;

  onMount(() => {
    if (canvas) {
      canvasCtx = canvas.getContext('2d');
    }
    if (vpcUrl && sessionToken) {
      connectToStream();
    }
  });

  onDestroy(() => {
    disconnect();
  });

  function connectToStream() {
    if (!vpcUrl || !sessionToken) {
      error = 'Missing VPC URL or session token';
      return;
    }

    const wsUrl = vpcUrl
      .replace('https://', 'wss://')
      .replace('http://', 'ws://');

    wsVideo = new WebSocket(`${wsUrl}/ws/scrcpy/view?token=${sessionToken}`);
    wsVideo.binaryType = 'arraybuffer';

    wsVideo.onopen = () => {
      connected = true;
      error = '';
      initDecoder();
    };

    wsVideo.onmessage = (event) => {
      const data = new Uint8Array(event.data);
      if (data.length === 0) return;

      switch (data[0]) {
        case MSG_TYPE_VIDEO:
          handleVideoFrame(data.slice(1));
          break;
        case MSG_TYPE_DEVICE_INFO:
          const infoJson = new TextDecoder().decode(data.slice(1));
          try {
            deviceInfo = JSON.parse(infoJson);
            if (deviceInfo && canvas) {
              canvas.width = deviceInfo.width;
              canvas.height = deviceInfo.height;
            }
          } catch (e) {}
          break;
      }
    };

    wsVideo.onerror = () => {
      error = 'WebSocket connection error';
    };

    wsVideo.onclose = () => {
      connected = false;
    };
  }

  function disconnect() {
    if (wsVideo) {
      wsVideo.close();
      wsVideo = null;
    }
    if (decoder) {
      try { decoder.close(); } catch(e) {}
      decoder = null;
    }
    connected = false;
  }

  function initDecoder() {
    // Use WebCodecs API if available (Chrome 94+, Edge, Safari 16.4+)
    if ('VideoDecoder' in window) {
      decoder = new (window as any).VideoDecoder({
        output: (frame: any) => {
          if (canvasCtx && canvas) {
            canvasCtx.drawImage(frame, 0, 0, canvas.width, canvas.height);
            frame.close();
            frameCount++;
          }
        },
        error: (e: any) => {
          console.error('Decoder error:', e);
        }
      });

      decoder.configure({
        codec: 'avc1.640028', // H.264 High Profile Level 4.0
        optimizeForLatency: true,
      });
    } else {
      error = 'WebCodecs API not supported. Use Chrome, Edge, or Safari 16.4+.';
    }
  }

  function handleVideoFrame(nalData: Uint8Array) {
    if (!decoder || decoder.state === 'closed') return;

    // Determine if this is a keyframe by checking NAL unit type
    const nalType = nalData.length > 0 ? (nalData[0] & 0x1f) : 0;
    const isKeyFrame = nalType === 5 || nalType === 7; // IDR or SPS

    try {
      const chunk = new (window as any).EncodedVideoChunk({
        type: isKeyFrame ? 'key' : 'delta',
        timestamp: performance.now() * 1000, // microseconds
        data: nalData,
      });
      decoder.decode(chunk);
    } catch (e) {
      // Skip frames that can't be decoded
    }
  }

  // Input handling
  function getScaledCoords(e: MouseEvent): { x: number; y: number } {
    if (!canvas || !deviceInfo) return { x: 0, y: 0 };
    const rect = canvas.getBoundingClientRect();
    const scaleX = deviceInfo.width / rect.width;
    const scaleY = deviceInfo.height / rect.height;
    return {
      x: Math.round((e.clientX - rect.left) * scaleX),
      y: Math.round((e.clientY - rect.top) * scaleY),
    };
  }

  function sendInputEvent(event: any) {
    if (!wsVideo || wsVideo.readyState !== WebSocket.OPEN) return;
    const json = JSON.stringify(event);
    const jsonBytes = new TextEncoder().encode(json);
    const msg = new Uint8Array(1 + jsonBytes.length);
    msg[0] = MSG_TYPE_INPUT;
    msg.set(jsonBytes, 1);
    wsVideo.send(msg.buffer);
  }

  function handleMouseDown(e: MouseEvent) {
    const { x, y } = getScaledCoords(e);
    sendInputEvent({ type: 'touch', action: 'down', x, y });
  }

  function handleMouseMove(e: MouseEvent) {
    if (e.buttons !== 1) return; // Only when dragging
    const { x, y } = getScaledCoords(e);
    sendInputEvent({ type: 'touch', action: 'move', x, y });
  }

  function handleMouseUp(e: MouseEvent) {
    const { x, y } = getScaledCoords(e);
    sendInputEvent({ type: 'touch', action: 'up', x, y });
  }

  function handleKeyDown(e: KeyboardEvent) {
    e.preventDefault();
    sendInputEvent({ type: 'key', action: 'down', keycode: mapKeyCode(e.code) });
  }

  function handleKeyUp(e: KeyboardEvent) {
    e.preventDefault();
    sendInputEvent({ type: 'key', action: 'up', keycode: mapKeyCode(e.code) });
  }

  function handleWheel(e: WheelEvent) {
    const { x, y } = getScaledCoords(e as any);
    sendInputEvent({ type: 'scroll', x, y, dx: e.deltaX, dy: -e.deltaY });
  }

  // Android navigation buttons
  function sendBack() { sendInputEvent({ type: 'key', action: 'press', keycode: 4 }); }
  function sendHome() { sendInputEvent({ type: 'key', action: 'press', keycode: 3 }); }
  function sendRecent() { sendInputEvent({ type: 'key', action: 'press', keycode: 187 }); }
  function sendPower() { sendInputEvent({ type: 'key', action: 'press', keycode: 26 }); }

  function mapKeyCode(code: string): number {
    const map: Record<string, number> = {
      'Enter': 66, 'Backspace': 67, 'Escape': 111, 'ArrowUp': 19,
      'ArrowDown': 20, 'ArrowLeft': 21, 'ArrowRight': 22, 'Space': 62,
      'Tab': 61, 'Delete': 112,
    };
    return map[code] || 0;
  }

  function toggleFullscreen() {
    const container = canvas?.parentElement;
    if (!container) return;
    if (!document.fullscreenElement) {
      container.requestFullscreen();
      isFullscreen = true;
    } else {
      document.exitFullscreen();
      isFullscreen = false;
    }
  }
</script>

<div class="remote-control">
  {#if error}
    <div class="rc-error">
      <span>⚠️ {error}</span>
      <button onclick={connectToStream}>Retry</button>
    </div>
  {/if}

  {#if !connected && !error}
    <div class="rc-connecting">
      <div class="rc-spinner"></div>
      <p>Connecting to Android stream...</p>
    </div>
  {/if}

  <div class="rc-toolbar">
    <div class="rc-toolbar-left">
      {#if deviceInfo}
        <span class="rc-device-name">📱 {deviceInfo.name}</span>
        <span class="rc-resolution">{deviceInfo.width}×{deviceInfo.height}</span>
      {/if}
      {#if connected}
        <span class="rc-status connected">● Live</span>
      {/if}
    </div>
    <div class="rc-toolbar-right">
      <button class="rc-btn" onclick={sendBack} title="Back">◀</button>
      <button class="rc-btn" onclick={sendHome} title="Home">●</button>
      <button class="rc-btn" onclick={sendRecent} title="Recent">■</button>
      <button class="rc-btn" onclick={sendPower} title="Power">⏻</button>
      <button class="rc-btn" onclick={toggleFullscreen} title="Fullscreen">⛶</button>
    </div>
  </div>

  <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
  <div 
    class="rc-canvas-container"
    role="application"
    tabindex="0"
    onkeydown={handleKeyDown}
    onkeyup={handleKeyUp}
  >
    <canvas
      bind:this={canvas}
      width={deviceInfo?.width || 1080}
      height={deviceInfo?.height || 2400}
      onmousedown={handleMouseDown}
      onmousemove={handleMouseMove}
      onmouseup={handleMouseUp}
      onmouseleave={handleMouseUp}
      onwheel={handleWheel}
      oncontextmenu={(e) => e.preventDefault()}
    ></canvas>
  </div>
</div>

<style>
  .remote-control {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: #0a0a0f;
    border-radius: 12px;
    overflow: hidden;
  }

  .rc-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 16px;
    background: rgba(255, 255, 255, 0.05);
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
  }

  .rc-toolbar-left, .rc-toolbar-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .rc-device-name {
    font-size: 13px;
    font-weight: 600;
    color: #e2e8f0;
  }

  .rc-resolution {
    font-size: 11px;
    font-family: monospace;
    color: #64748b;
  }

  .rc-status {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 10px;
  }

  .rc-status.connected {
    color: #10b981;
    background: rgba(16, 185, 129, 0.15);
  }

  .rc-btn {
    background: rgba(255, 255, 255, 0.08);
    border: 1px solid rgba(255, 255, 255, 0.1);
    color: #94a3b8;
    padding: 6px 12px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.15s;
  }

  .rc-btn:hover {
    background: rgba(255, 255, 255, 0.15);
    color: #e2e8f0;
  }

  .rc-canvas-container {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: center;
    overflow: hidden;
    outline: none;
  }

  canvas {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    cursor: crosshair;
    image-rendering: auto;
  }

  .rc-error {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 12px 20px;
    background: rgba(239, 68, 68, 0.1);
    color: #f87171;
    font-size: 13px;
  }

  .rc-error button {
    background: rgba(239, 68, 68, 0.2);
    border: 1px solid rgba(239, 68, 68, 0.3);
    color: #f87171;
    padding: 4px 12px;
    border-radius: 4px;
    cursor: pointer;
  }

  .rc-connecting {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 60px 20px;
    color: #64748b;
  }

  .rc-spinner {
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
