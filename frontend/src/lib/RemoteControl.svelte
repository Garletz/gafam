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
  let configBuffer: Uint8Array | null = null;
  let hasReceivedKeyFrame = $state(false);

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

  let streamAbort: AbortController | null = null;

  async function connectToStream() {
    if (!vpcUrl || !sessionToken) {
      error = 'Missing VPC URL or session token';
      return;
    }

    error = '';
    initDecoder();
    
    streamAbort = new AbortController();

    try {
      const response = await fetch(`/api/proxy/scrcpy/video_stream?vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`, {
        signal: streamAbort.signal
      });

      if (!response.ok) {
        error = 'Failed to connect: ' + response.status;
        return;
      }
      
      connected = true;

      // Initial device info might be in headers
      const deviceHeader = response.headers.get('X-Scrcpy-Device');
      if (deviceHeader) {
        try {
          deviceInfo = JSON.parse(deviceHeader);
          if (deviceInfo && canvas) {
            canvas.width = deviceInfo.width;
            canvas.height = deviceInfo.height;
          }
        } catch(e) {}
      }

      const reader = response.body?.getReader();
      if (!reader) {
        error = 'Streaming not supported by browser';
        return;
      }

      let buffer = new Uint8Array(0);

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;

        if (value) {
          const newBuffer = new Uint8Array(buffer.length + value.length);
          newBuffer.set(buffer);
          newBuffer.set(value, buffer.length);
          buffer = newBuffer;
        }

        while (buffer.length >= 4) {
          const len = new DataView(buffer.buffer, buffer.byteOffset, buffer.byteLength).getUint32(0, false);
          if (buffer.length >= 4 + len) {
            const frame = buffer.slice(4, 4 + len);
            buffer = buffer.slice(4 + len);
            
            if (frame.length > 0) {
              if (frame[0] === MSG_TYPE_VIDEO) {
                handleVideoFrame(frame.slice(1));
              } else if (frame[0] === MSG_TYPE_DEVICE_INFO) {
                const infoJson = new TextDecoder().decode(frame.slice(1));
                try {
                  deviceInfo = JSON.parse(infoJson);
                  if (deviceInfo && canvas) {
                    canvas.width = deviceInfo.width;
                    canvas.height = deviceInfo.height;
                  }
                } catch(e) {}
              }
            }
          } else {
            break;
          }
        }
      }
      
      connected = false;
      // Auto-reconnect if dropped
      if (!streamAbort) {
        setTimeout(() => {
          if (vpcUrl && sessionToken && browser && !connected) connectStream();
        }, 2000);
      }
    } catch (e: any) {
      if (e.name !== 'AbortError') {
        error = 'Stream error: ' + e.message;
        connected = false;
        // Auto-reconnect on error
        setTimeout(() => {
          if (vpcUrl && sessionToken && browser && !connected) connectStream();
        }, 2000);
      }
    }
  }

  function disconnect() {
    if (streamAbort) {
      streamAbort.abort();
      streamAbort = null;
    }
    if (decoder) {
      try { decoder.close(); } catch(e) {}
      decoder = null;
    }
    configBuffer = null;
    hasReceivedKeyFrame = false;
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

  function handleVideoFrame(chunkData: Uint8Array) {
    if (!decoder || decoder.state === 'closed') return;

    let isKeyFrame = false;
    let isConfigOnly = true;

    // Check NAL unit types in the chunk
    for (let i = 0; i < Math.min(chunkData.length - 4, 200); i++) {
      if (chunkData[i] === 0 && chunkData[i+1] === 0 && chunkData[i+2] === 0 && chunkData[i+3] === 1) {
        const nalType = chunkData[i+4] & 0x1f;
        if (nalType === 5) {
          isKeyFrame = true;
          isConfigOnly = false; // Has IDR frame
        } else if (nalType === 1) {
          isConfigOnly = false; // Has P-frame
        } else if (nalType === 7 || nalType === 8) {
          isKeyFrame = true; // SPS/PPS imply keyframe config
        }
      }
    }

    // If chunk contains ONLY config (SPS/PPS), save it and do NOT decode yet!
    if (isConfigOnly && isKeyFrame) {
      configBuffer = chunkData;
      return;
    }

    let dataToDecode = chunkData;

    // Prepend config buffer to keyframes if we have one
    if (isKeyFrame && configBuffer && !isConfigOnly) {
      dataToDecode = new Uint8Array(configBuffer.length + chunkData.length);
      dataToDecode.set(configBuffer);
      dataToDecode.set(chunkData, configBuffer.length);
    }

    if (!isKeyFrame && !hasReceivedKeyFrame) {
      // Drop delta frames if we haven't seen a keyframe yet after connection
      return;
    }
    
    if (isKeyFrame && !isConfigOnly) {
      hasReceivedKeyFrame = true;
    }

    try {
      const chunk = new (window as any).EncodedVideoChunk({
        type: isKeyFrame ? 'key' : 'delta',
        timestamp: performance.now() * 1000, // microseconds
        data: dataToDecode,
      });
      decoder.decode(chunk);
    } catch (e) {
      console.error("WebCodecs decode error:", e);
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
    if (!connected) return;
    const json = JSON.stringify(event);
    fetch(`/api/proxy/scrcpy/input?vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`, {
      method: 'POST',
      body: json,
      headers: { 'Content-Type': 'application/json' }
    }).catch(() => {}); // Fire and forget
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
      <span>{error}</span>
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
        <span class="rc-device-name">Device: {deviceInfo.name}</span>
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
    {#if connected && !hasReceivedKeyFrame}
      <div class="rc-loading-overlay">
        <div class="rc-spinner"></div>
        <span>Waiting for Video Stream...</span>
      </div>
    {/if}
  </div>
</div>

<style>
  .remote-control {
    display: flex;
    flex-direction: column;
    height: 100%;
    background: #ffffff;
    overflow: hidden;
  }

  .rc-toolbar {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 16px;
    background: #ffffff;
    border-bottom: 1px solid #e5e5e5;
  }

  .rc-toolbar-left, .rc-toolbar-right {
    display: flex;
    align-items: center;
    gap: 12px;
  }

  .rc-device-name {
    font-size: 13px;
    font-weight: 600;
    color: #000;
    letter-spacing: 1px;
    font-family: 'Courier New', Courier, monospace;
  }

  .rc-resolution {
    font-size: 11px;
    font-family: 'Courier New', Courier, monospace;
    color: #888;
  }

  .rc-status {
    font-size: 11px;
    font-weight: bold;
    padding: 2px 8px;
    border-radius: 0;
    font-family: 'Courier New', Courier, monospace;
    letter-spacing: 1px;
    border: 1px solid transparent;
  }

  .rc-status.connected {
    color: #000;
    background: transparent;
    border-color: #000;
  }

  .rc-btn {
    background: transparent;
    border: 1px solid #ccc;
    color: #000;
    padding: 6px 12px;
    border-radius: 0;
    cursor: pointer;
    font-size: 14px;
    transition: all 0.2s;
    font-family: 'Courier New', Courier, monospace;
  }

  .rc-btn:hover {
    background: #000;
    color: #fff;
    border-color: #000;
  }

  .rc-canvas-container {
    flex: 1;
    display: flex;
    justify-content: center;
    align-items: center;
    overflow: hidden;
    outline: none;
    position: relative;
    background: transparent;
  }

  .rc-loading-overlay {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    background: rgba(255, 255, 255, 0.8);
    display: flex;
    flex-direction: column;
    justify-content: center;
    align-items: center;
    color: #000;
    font-family: 'Courier New', Courier, monospace;
    font-size: 14px;
    gap: 16px;
    backdrop-filter: blur(2px);
    z-index: 10;
    pointer-events: none;
  }

  .rc-spinner {
    width: 40px;
    height: 40px;
    border: 3px solid rgba(0,0,0,0.1);
    border-radius: 50%;
    border-top-color: #000;
    animation: spin 1s ease-in-out infinite;
  }

  @keyframes spin {
    to { transform: rotate(360deg); }
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
    background: #111;
    border-bottom: 1px solid #555;
    color: #fff;
    font-size: 13px;
    font-family: 'Courier New', Courier, monospace;
  }

  .rc-error button {
    background: transparent;
    border: 1px solid #fff;
    color: #fff;
    padding: 4px 12px;
    cursor: pointer;
    font-family: 'Courier New', Courier, monospace;
  }

  .rc-error button:hover {
    background: #fff;
    color: #000;
  }

  .rc-connecting {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    padding: 60px 20px;
    color: #888;
    font-family: 'Courier New', Courier, monospace;
  }

  .rc-spinner {
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
</style>
