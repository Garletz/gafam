<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EDGE_QUICK_PROMPTS, type EdgeInferResult, type EdgeModelStatus, type EdgeQuickPrompt, type EdgeStatus } from './types';
  import {
    edgeStop,
    edgeWake,
    fetchEdgeModelStatus,
    fetchEdgeStatus,
    getRamRequestMb,
    runEdgeInfer,
    setRamRequestMb,
    startEdgeModelInstall
  } from './edgeApi';
  import { buildTodayLogsPrompt, EDGE_INFER_PROMPT_MAX, type PhoneLogEntry } from './edgeLogsPrompt';

  let { vpcUrl, sessionToken }: { vpcUrl: string; sessionToken: string } = $props();

  let status: EdgeStatus | null = $state(null);
  let modelStatus: EdgeModelStatus | null = $state(null);
  let modelMsg = $state('');
  let modelInstalling = $state(false);
  let ramRequest = $state(2048);
  let customPrompt = $state('');
  let running = $state(false);
  let actionMsg = $state('');
  let lastShot: { prompt: string; result: EdgeInferResult } | null = $state(null);
  let clearTimer: ReturnType<typeof setTimeout> | null = null;
  let pollId: ReturnType<typeof setInterval> | null = null;
  let modelPollId: ReturnType<typeof setInterval> | null = null;

  const modelReadyOnVpc = $derived(modelStatus?.ready ?? status?.edge_model_on_vpc ?? false);
  const modelOnPhone = $derived(status?.model_on_device ?? false);
  const installProgress = $derived(modelStatus?.install?.progress ?? 0);
  const installState = $derived(modelStatus?.install?.status ?? 'idle');

  function formatBytes(n: number): string {
    if (!n) return '0 B';
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(0)} KB`;
    return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  }

  async function refreshModelStatus() {
    modelStatus = await fetchEdgeModelStatus(vpcUrl, sessionToken);
    const downloading = modelStatus?.install?.status === 'downloading';
    if (downloading && !modelPollId) {
      modelPollId = setInterval(refreshModelStatus, 3000);
    } else if (!downloading && modelPollId) {
      clearInterval(modelPollId);
      modelPollId = null;
    }
  }

  async function doInstallModel() {
    if (modelInstalling || modelReadyOnVpc) return;
    modelInstalling = true;
    modelMsg = '';
    const r = await startEdgeModelInstall(vpcUrl, sessionToken);
    modelInstalling = false;
    if (!r.ok) {
      modelMsg = r.error || 'Failed to start VPC download';
      return;
    }
    modelMsg = 'Download started on VPC (HuggingFace → disk)…';
    await refreshModelStatus();
    await refreshStatus();
  }

  const ramMax = $derived(
    status?.edge_ram_max_deliverable_mb && status.edge_ram_max_deliverable_mb >= 512
      ? status.edge_ram_max_deliverable_mb
      : 4096
  );

  async function refreshStatus() {
    const prev = status;
    status = await fetchEdgeStatus(vpcUrl, sessionToken);
    const max = status?.edge_ram_max_deliverable_mb && status.edge_ram_max_deliverable_mb >= 512
      ? status.edge_ram_max_deliverable_mb
      : 4096;
    if (ramRequest > max) {
      ramRequest = max;
      setRamRequestMb(ramRequest, max);
    } else if (!prev && status) {
      ramRequest = getRamRequestMb(max);
    }
  }

  function scheduleClear() {
    if (clearTimer) clearTimeout(clearTimer);
    clearTimer = setTimeout(() => {
      lastShot = null;
      clearTimer = null;
    }, 45_000);
  }

  async function fetchTodayLogsForInfer() {
    const today = new Date().toISOString().slice(0, 10);
    let day = today;
    const listParams = new URLSearchParams({ vpcUrl, token: sessionToken });
    const listRes = await fetch(`/api/proxy/logs?${listParams}`);
    if (listRes.ok) {
      const listData = await listRes.json();
      const days: Array<{ day: string; lines: number }> = listData.days || [];
      const pick = days.find((d) => d.day === today) ?? days[0];
      if (pick?.day) day = pick.day;
      else if (days.length === 0) return null;
    }
    const params = new URLSearchParams({
      vpcUrl,
      token: sessionToken,
      day,
      offset: '0',
      limit: '120'
    });
    const res = await fetch(`/api/proxy/logs?${params}`);
    if (!res.ok) return null;
    const data = await res.json();
    const entries: PhoneLogEntry[] = data.entries || [];
    if (entries.length === 0) return null;
    return buildTodayLogsPrompt(entries, day, data.total_lines ?? entries.length);
  }

  async function fireQuick(q: EdgeQuickPrompt) {
    if (q.action === 'today_logs') {
      actionMsg = "Loading today's logs from VPC…";
      const built = await fetchTodayLogsForInfer();
      if (!built) {
        actionMsg = 'No logs found — ship logs from the phone first (Logs tab).';
        return;
      }
      actionMsg = '';
      await fire(built.prompt, built.displayLabel);
      return;
    }
    if (q.action === 'edge_ping') {
      await fire('Reply with exactly the text EDGE_OK and nothing else.');
      return;
    }
    await fire(q.prompt);
  }

  async function fire(prompt: string, displayPrompt?: string) {
    const text = prompt.trim();
    if (!text || running) return;
    if (text.length > EDGE_INFER_PROMPT_MAX) {
      actionMsg = `Prompt too long (${text.length}/${EDGE_INFER_PROMPT_MAX}) — trim logs and retry.`;
      return;
    }
    running = true;
    actionMsg = '';
    if (clearTimer) {
      clearTimeout(clearTimer);
      clearTimer = null;
    }
    lastShot = null;

    const { ok, result, error } = await runEdgeInfer(vpcUrl, sessionToken, text, 'deep', ramRequest);
    running = false;

    if (result) {
      lastShot = { prompt: displayPrompt ?? text, result };
      scheduleClear();
    }
    if (!ok && error) actionMsg = error;
    await refreshStatus();
  }

  async function doWake() {
    actionMsg = '';
    const r = await edgeWake(vpcUrl, sessionToken, ramRequest);
    actionMsg = r.ok
      ? (r.message || 'Wake queued — check phone notif in ~2s')
      : (r.error || 'Wake failed');
    if (status?.scrcpy_blocking) {
      actionMsg += ' (scrcpy active: infer blocked, wake OK)';
    }
    await refreshStatus();
  }

  async function doStop() {
    actionMsg = '';
    const r = await edgeStop(vpcUrl, sessionToken);
    actionMsg = r.ok ? r.message || 'Stop sent' : r.error || 'Stop failed';
    lastShot = null;
    await refreshStatus();
  }

  function onRamChange() {
    setRamRequestMb(ramRequest, ramMax);
  }

  onMount(() => {
    ramRequest = getRamRequestMb();
    refreshStatus();
    refreshModelStatus();
    pollId = setInterval(() => {
      refreshStatus();
      refreshModelStatus();
    }, 10_000);
  });

  onDestroy(() => {
    if (pollId) clearInterval(pollId);
    if (modelPollId) clearInterval(modelPollId);
    if (clearTimer) clearTimeout(clearTimer);
  });
</script>

<section class="panel">
  <div class="panel__intro">
    <p>
      One-shot tester — no history. Edge L2 uses the <strong>APK relay</strong> (HTTP), not the ADB/scrcpy bridge.
      RAM cap is set once on the phone; here you pick how much to use for <strong>this task</strong>.
    </p>
  </div>

  <div class="status-grid">
    <div class="status-card">
      <span class="label">APK relay</span>
      <span class="value" class:on={status?.apk_relay_online ?? status?.phone_reachable}>
        {(status?.apk_relay_online ?? status?.phone_reachable) ? 'online' : 'offline'}
      </span>
      {#if status?.apk_relay_last_seen}
        <span class="sub">last {new Date(status.apk_relay_last_seen).toLocaleTimeString()}</span>
      {/if}
    </div>
    <div class="status-card">
      <span class="label">Bridge scrcpy</span>
      <span class="value" class:on={status?.scrcpy_bridge_connected}>
        {status?.scrcpy_bridge_connected ? 'connected' : 'offline'}
      </span>
      <span class="sub">remote / ADB only</span>
    </div>
    <div class="status-card">
      <span class="label">Edge service</span>
      <span class="value" class:on={status?.edge_service === 'awake'}>{status?.edge_service ?? '—'}</span>
      {#if status?.edge_message}
        <span class="sub" title={status.edge_message}>{status.edge_message}</span>
      {/if}
    </div>
    <div class="status-card">
      <span class="label">Phone cap</span>
      <span class="value mono">{status?.edge_ram_cap_mb ? `${status.edge_ram_cap_mb} MB` : '—'}</span>
      {#if status?.device_ram_avail_mb}
        <span class="sub">free {status.device_ram_avail_mb} / {status.device_ram_total_mb ?? '?'} MB</span>
      {/if}
    </div>
    <div class="status-card">
      <span class="label">Max deliverable</span>
      <span class="value mono">
        {status?.edge_ram_max_deliverable_mb ? `${status.edge_ram_max_deliverable_mb} MB` : '—'}
      </span>
    </div>
    <div class="status-card">
      <span class="label">VPC model</span>
      <span class="value mono" class:on={modelReadyOnVpc}>
        {modelReadyOnVpc ? 'on disk' : installState === 'downloading' ? 'downloading…' : 'missing'}
      </span>
    </div>
    <div class="status-card">
      <span class="label">Phone model</span>
      <span class="value mono" class:on={modelOnPhone}>
        {modelOnPhone ? 'on device' : 'not loaded'}
      </span>
    </div>
    <div class="status-card">
      <span class="label">scrcpy block</span>
      <span class="value" class:warn={status?.scrcpy_blocking}>{status?.scrcpy_blocking ? 'yes' : 'no'}</span>
    </div>
    <div class="status-card">
      <span class="label">Phase</span>
      <span class="value mono">{status?.phase ?? '—'}</span>
    </div>
  </div>

  <div class="model-panel" class:model-panel--ready={modelReadyOnVpc}>
    <div class="model-panel__head">
      <h3>Qwen3 0.6B ONNX (VPC → phone)</h3>
      <span class="model-badge" class:on={modelReadyOnVpc}>
        {modelReadyOnVpc ? 'Ready to push' : installState === 'downloading' ? 'Downloading…' : 'Missing on VPC'}
      </span>
    </div>
    <p class="model-panel__hint">
      Model must be on VPC disk first (~525 MB). On <strong>Wake</strong>, the phone downloads from VPC (not HuggingFace direct).
    </p>
    {#if modelStatus?.files?.length}
      <ul class="model-files">
        {#each modelStatus.files as f}
          <li class:ok={f.size > 0}>
            <span class="fname">{f.name}</span>
            <span class="fsize">{f.size > 0 ? formatBytes(f.size) : '—'}</span>
          </li>
        {/each}
      </ul>
      {#if modelStatus.total_bytes > 0}
        <p class="model-total">Total on disk: {formatBytes(modelStatus.total_bytes)}</p>
      {/if}
    {/if}
    {#if installState === 'downloading'}
      <div class="model-progress">
        <div class="model-progress__bar" style="width: {installProgress}%"></div>
      </div>
      <p class="model-progress__msg">
        {installProgress}% — {modelStatus?.install?.current_file ?? '…'}
        {#if modelStatus?.install?.message}
          <span class="sub">({modelStatus.install.message})</span>
        {/if}
      </p>
    {:else if !modelReadyOnVpc}
      <button
        type="button"
        class="btn btn-primary model-install-btn"
        disabled={modelInstalling || installState === 'downloading'}
        onclick={doInstallModel}
      >
        {modelInstalling ? 'Starting…' : 'Download to VPC'}
      </button>
    {/if}
    {#if modelStatus?.install?.error}
      <p class="model-error">{modelStatus.install.error}</p>
    {/if}
    {#if modelMsg}
      <p class="model-msg">{modelMsg}</p>
    {/if}
  </div>

  <div class="ram-row">
    <label for="ram-request">RAM for this task</label>
    <input
      id="ram-request"
      type="range"
      min="512"
      max={ramMax}
      step="256"
      bind:value={ramRequest}
      onchange={onRamChange}
    />
    <span class="ram-val">{ramRequest} / {ramMax} MB</span>
  </div>

  <div class="actions-row">
    <button type="button" class="btn btn-ghost" disabled={running} onclick={refreshStatus}>Ping</button>
    <button type="button" class="btn btn-ghost" disabled={running} onclick={doWake}>Wake</button>
    <button type="button" class="btn btn-ghost" disabled={running} onclick={doStop}>Stop</button>
  </div>

  {#if actionMsg}
    <p class="action-msg" class:is-error={actionMsg.includes('failed') || actionMsg.includes('offline') || actionMsg.includes('busy')}>{actionMsg}</p>
  {/if}

  {#if status?.scrcpy_blocking}
    <p class="scrcpy-warn">scrcpy/shell active — L2 infer is blocked, but Wake/Stop still work.</p>
  {/if}

  <div class="quick-tests">
    <span class="quick-label">Quick tests</span>
    <div class="quick-btns">
      {#each EDGE_QUICK_PROMPTS as q}
        <button type="button" class="btn btn-quick" disabled={running} onclick={() => fireQuick(q)}>
          {q.label}
        </button>
      {/each}
    </div>
  </div>

  <div class="prompt-row">
    <input
      type="text"
      placeholder="Your prompt (e.g. 1+1=?)"
      bind:value={customPrompt}
      disabled={running}
      onkeydown={(e) => e.key === 'Enter' && fire(customPrompt)}
    />
    <button type="button" class="btn btn-primary" disabled={running || !customPrompt.trim()} onclick={() => fire(customPrompt)}>
      {running ? '…' : 'Run'}
    </button>
  </div>

  {#if lastShot}
    <div class="shot" class:is-stub={lastShot.result.status === 'stub'} class:is-error={lastShot.result.status === 'error'}>
      <div class="shot__you"><span>You</span> {lastShot.prompt}</div>
      <div class="shot__reply">
        <span>Reply</span>
        {#if lastShot.result.content}
          {lastShot.result.content}
        {:else}
          {lastShot.result.error || 'No response'}
        {/if}
      </div>
      <footer class="shot__meta">
        {lastShot.result.engine ?? '—'} · tier {lastShot.result.tier_used ?? '—'} ·
        {lastShot.result.latency_ms ?? 0} ms · {lastShot.result.status ?? '—'}
      </footer>
    </div>
  {:else if !running}
    <p class="shot-hint">Last reply shows here then clears (~45 s).</p>
  {/if}
</section>

<style>
  .panel__intro {
    margin-bottom: 20px;
  }
  .panel__intro p {
    margin: 0;
    font-size: 14px;
    line-height: 1.5;
    color: #5f6368;
  }
  .status-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(130px, 1fr));
    gap: 10px;
    margin-bottom: 16px;
  }
  .status-card {
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    padding: 10px 12px;
    background: #f8f9fa;
  }
  .status-card .label {
    display: block;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: #80868b;
    margin-bottom: 4px;
  }
  .status-card .value {
    font-size: 13px;
    font-weight: 600;
    font-family: ui-monospace, monospace;
  }
  .status-card .value.on {
    color: #188038;
  }
  .status-card .value.warn {
    color: #ea8600;
  }
  .status-card .value.mono {
    font-size: 11px;
  }
  .status-card .sub {
    display: block;
    margin-top: 4px;
    font-size: 10px;
    color: #9aa0a6;
    font-family: ui-monospace, monospace;
  }
  .model-panel {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 14px 16px;
    margin-bottom: 16px;
    background: #fff;
  }
  .model-panel--ready {
    border-color: #ceead6;
    background: #f6fff8;
  }
  .model-panel__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
    margin-bottom: 8px;
  }
  .model-panel__head h3 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .model-badge {
    font-size: 11px;
    font-weight: 600;
    padding: 4px 8px;
    border-radius: 4px;
    background: #fce8e6;
    color: #c5221f;
    font-family: ui-monospace, monospace;
  }
  .model-badge.on {
    background: #ceead6;
    color: #188038;
  }
  .model-panel__hint {
    margin: 0 0 12px;
    font-size: 13px;
    line-height: 1.45;
    color: #5f6368;
  }
  .model-files {
    list-style: none;
    margin: 0 0 8px;
    padding: 0;
    font-size: 12px;
    font-family: ui-monospace, monospace;
  }
  .model-files li {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    padding: 3px 0;
    color: #9aa0a6;
  }
  .model-files li.ok {
    color: #188038;
  }
  .model-total {
    margin: 0 0 12px;
    font-size: 12px;
    color: #5f6368;
    font-family: ui-monospace, monospace;
  }
  .model-progress {
    height: 8px;
    background: #e8eaed;
    border-radius: 4px;
    overflow: hidden;
    margin-bottom: 8px;
  }
  .model-progress__bar {
    height: 100%;
    background: #1a73e8;
    transition: width 0.3s ease;
  }
  .model-progress__msg {
    margin: 0 0 12px;
    font-size: 12px;
    color: #5f6368;
    font-family: ui-monospace, monospace;
  }
  .model-install-btn {
    margin-bottom: 8px;
  }
  .model-error {
    margin: 8px 0 0;
    font-size: 12px;
    color: #d93025;
  }
  .model-msg {
    margin: 8px 0 0;
    font-size: 12px;
    color: #188038;
    font-family: ui-monospace, monospace;
  }
  .ram-row {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 14px;
    font-size: 13px;
    flex-wrap: wrap;
  }
  .ram-row input[type='range'] {
    flex: 1;
    min-width: 120px;
  }
  .ram-val {
    font-family: ui-monospace, monospace;
    font-size: 12px;
    color: #5f6368;
    min-width: 96px;
  }
  .actions-row,
  .quick-btns,
  .prompt-row {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    align-items: center;
  }
  .actions-row {
    margin-bottom: 12px;
  }
  .action-msg {
    margin: 0 0 12px;
    font-size: 12px;
    color: #188038;
    font-family: ui-monospace, monospace;
  }
  .action-msg.is-error {
    color: #d93025;
  }
  .scrcpy-warn {
    margin: 0 0 12px;
    font-size: 12px;
    color: #ea8600;
  }
  .quick-tests {
    margin-bottom: 14px;
  }
  .quick-label {
    display: block;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #80868b;
    margin-bottom: 8px;
  }
  .prompt-row {
    margin-bottom: 16px;
  }
  .prompt-row input {
    flex: 1;
    min-width: 180px;
    padding: 8px 12px;
    border: 1px solid #dadce0;
    border-radius: 4px;
    font-size: 14px;
  }
  .btn {
    border-radius: 4px;
    padding: 8px 14px;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    border: 1px solid #dadce0;
    background: #fff;
    color: #202124;
  }
  .btn:disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .btn-primary {
    background: #202124;
    color: #fff;
    border-color: #202124;
  }
  .btn-ghost:hover,
  .btn-quick:hover {
    background: #f1f3f4;
  }
  .btn-quick {
    font-family: ui-monospace, monospace;
  }
  .shot {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 14px 16px;
    background: #f8f9fa;
    max-width: 560px;
  }
  .shot.is-stub {
    border-color: #fdd663;
    background: #fef7e0;
  }
  .shot.is-error {
    border-color: #f28b82;
    background: #fce8e6;
  }
  .shot__you,
  .shot__reply {
    font-size: 14px;
    line-height: 1.5;
    margin-bottom: 10px;
  }
  .shot__you span,
  .shot__reply span {
    display: inline-block;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: #80868b;
    margin-right: 8px;
    min-width: 36px;
  }
  .shot__meta {
    font-size: 11px;
    color: #5f6368;
    font-family: ui-monospace, monospace;
  }
  .shot-hint {
    margin: 0;
    font-size: 13px;
    color: #9aa0a6;
  }
</style>
