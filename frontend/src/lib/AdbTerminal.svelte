<script lang="ts">
  import { onMount, onDestroy } from 'svelte';

  let { vpcUrl = '', sessionToken = '' } = $props();

  let terminalEl: HTMLDivElement;
  let ws: WebSocket | null = null;
  let connected = $state(false);
  let error = $state('');
  let inputLine = $state('');
  let outputLines = $state<string[]>(['Welcome to GAFAM ADB Shell', '$ ']);

  const MSG_TYPE_SHELL = 0x04;

  onMount(() => {
    if (vpcUrl && sessionToken) {
      connect();
    }
  });

  onDestroy(() => {
    disconnect();
  });

  let streamAbort: AbortController | null = null;

  async function connect() {
    if (!vpcUrl || !sessionToken) return;
    
    streamAbort = new AbortController();

    try {
      const response = await fetch(`/api/proxy/scrcpy/shell_stream?vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`, {
        signal: streamAbort.signal
      });

      if (!response.ok) {
        error = 'Shell connection error. Is ADB Shell enabled in Settings?';
        return;
      }

      connected = true;
      error = '';
      appendOutput('Connected to ADB Shell via VPS\n$ ');

      const reader = response.body?.getReader();
      if (!reader) return;

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
              const text = new TextDecoder().decode(frame);
              appendOutput(text);
            }
          } else {
            break;
          }
        }
      }

      connected = false;
      appendOutput('\n[Disconnected]\n');
    } catch (e: any) {
      if (e.name !== 'AbortError') {
        error = 'Shell stream error: ' + e.message;
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
  }

  function appendOutput(text: string) {
    // Split by newlines and append
    const lines = text.split('\n');
    const lastLine = outputLines[outputLines.length - 1] || '';
    outputLines[outputLines.length - 1] = lastLine + lines[0];
    for (let i = 1; i < lines.length; i++) {
      outputLines.push(lines[i]);
    }
    outputLines = [...outputLines]; // trigger reactivity
    scrollToBottom();
  }

  function scrollToBottom() {
    requestAnimationFrame(() => {
      if (terminalEl) {
        terminalEl.scrollTop = terminalEl.scrollHeight;
      }
    });
  }

  function sendCommand() {
    if (!connected || !inputLine.trim()) return;
    
    const cmd = inputLine.trim();
    appendOutput(cmd + '\n');
    
    fetch(`/api/proxy/scrcpy/shell_input?vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`, {
      method: 'POST',
      body: cmd + '\n',
      headers: { 'Content-Type': 'text/plain' }
    }).catch(() => {});

    inputLine = '';
  }

  function handleKeyDown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault();
      sendCommand();
    }
  }
</script>

<div class="adb-terminal">
  <div class="term-header">
    <span class="term-title">🖥️ ADB Shell</span>
    {#if connected}
      <span class="term-status connected">● Connected</span>
    {:else}
      <span class="term-status">○ Disconnected</span>
    {/if}
  </div>

  {#if error}
    <div class="term-error">{error}</div>
  {/if}

  <div class="term-output" bind:this={terminalEl}>
    {#each outputLines as line}
      <div class="term-line">{line}</div>
    {/each}
  </div>

  <div class="term-input-row">
    <span class="term-prompt">$</span>
    <input 
      type="text" 
      class="term-input"
      bind:value={inputLine}
      onkeydown={handleKeyDown}
      placeholder={connected ? 'Type a command...' : 'Not connected'}
      disabled={!connected}
    />
  </div>
</div>

<style>
  .adb-terminal {
    display: flex;
    flex-direction: column;
    background: #0d1117;
    border-radius: 12px;
    overflow: hidden;
    border: 1px solid rgba(255, 255, 255, 0.08);
    height: 100%;
    min-height: 300px;
  }

  .term-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 10px 16px;
    background: rgba(255, 255, 255, 0.03);
    border-bottom: 1px solid rgba(255, 255, 255, 0.06);
  }

  .term-title {
    font-size: 13px;
    font-weight: 600;
    color: #c9d1d9;
  }

  .term-status {
    font-size: 11px;
    font-weight: 600;
    color: #6e7681;
  }

  .term-status.connected {
    color: #3fb950;
  }

  .term-error {
    padding: 8px 16px;
    background: rgba(248, 81, 73, 0.1);
    color: #f85149;
    font-size: 12px;
    border-bottom: 1px solid rgba(248, 81, 73, 0.2);
  }

  .term-output {
    flex: 1;
    overflow-y: auto;
    padding: 12px 16px;
    font-family: 'JetBrains Mono', 'Fira Code', 'Menlo', monospace;
    font-size: 13px;
    line-height: 1.5;
    color: #c9d1d9;
  }

  .term-line {
    white-space: pre-wrap;
    word-break: break-all;
  }

  .term-input-row {
    display: flex;
    align-items: center;
    padding: 8px 16px;
    border-top: 1px solid rgba(255, 255, 255, 0.06);
    background: rgba(255, 255, 255, 0.02);
  }

  .term-prompt {
    color: #3fb950;
    font-family: 'JetBrains Mono', 'Fira Code', 'Menlo', monospace;
    font-size: 13px;
    font-weight: 700;
    margin-right: 8px;
  }

  .term-input {
    flex: 1;
    background: transparent;
    border: none;
    color: #c9d1d9;
    font-family: 'JetBrains Mono', 'Fira Code', 'Menlo', monospace;
    font-size: 13px;
    outline: none;
  }

  .term-input::placeholder {
    color: #484f58;
  }
</style>
