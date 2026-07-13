<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { EDGE_QUICK_PROMPTS, type EdgeInferResult, type EdgeStatus } from './types';
  import {
    edgeStop,
    edgeWake,
    fetchEdgeStatus,
    getRamBudgetMb,
    runEdgeInfer,
    setRamBudgetMb
  } from './edgeApi';

  let { vpcUrl, sessionToken }: { vpcUrl: string; sessionToken: string } = $props();

  let status: EdgeStatus | null = $state(null);
  let ramBudget = $state(2048);
  let customPrompt = $state('');
  let running = $state(false);
  let actionMsg = $state('');
  let lastShot: { prompt: string; result: EdgeInferResult } | null = $state(null);
  let clearTimer: ReturnType<typeof setTimeout> | null = null;
  let pollId: ReturnType<typeof setInterval> | null = null;

  async function refreshStatus() {
    status = await fetchEdgeStatus(vpcUrl, sessionToken);
  }

  function scheduleClear() {
    if (clearTimer) clearTimeout(clearTimer);
    clearTimer = setTimeout(() => {
      lastShot = null;
      clearTimer = null;
    }, 45_000);
  }

  async function fire(prompt: string) {
    const text = prompt.trim();
    if (!text || running) return;
    running = true;
    actionMsg = '';
    if (clearTimer) {
      clearTimeout(clearTimer);
      clearTimer = null;
    }
    lastShot = null;

    const { ok, result, error } = await runEdgeInfer(vpcUrl, sessionToken, text, 'deep');
    running = false;

    if (result) {
      lastShot = { prompt: text, result };
      scheduleClear();
    }
    if (!ok && error) actionMsg = error;
    await refreshStatus();
  }

  async function doWake() {
    actionMsg = '';
    const r = await edgeWake(vpcUrl, sessionToken);
    actionMsg = r.ok ? r.message || 'Wake sent' : r.error || 'Wake failed';
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
    setRamBudgetMb(ramBudget);
  }

  onMount(() => {
    ramBudget = getRamBudgetMb();
    refreshStatus();
    pollId = setInterval(refreshStatus, 10_000);
  });

  onDestroy(() => {
    if (pollId) clearInterval(pollId);
    if (clearTimer) clearTimeout(clearTimer);
  });
</script>

<section class="panel">
  <div class="panel__intro">
    <p>
      One-shot tester — pas d’historique. Envoie un prompt, lis la réponse, ça s’efface tout seul.
      Phase 2b : le VPC répond en stub jusqu’à l’APK EdgeInferenceService.
    </p>
  </div>

  <div class="status-grid">
    <div class="status-card">
      <span class="label">Phone relay</span>
      <span class="value" class:on={status?.phone_reachable}>{status?.phone_reachable ? 'online' : 'offline'}</span>
    </div>
    <div class="status-card">
      <span class="label">Edge service</span>
      <span class="value">{status?.edge_service ?? '—'}</span>
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

  <div class="ram-row">
    <label for="ram-budget">RAM budget (consentement local)</label>
    <input
      id="ram-budget"
      type="range"
      min="512"
      max="4096"
      step="256"
      bind:value={ramBudget}
      onchange={onRamChange}
    />
    <span class="ram-val">{ramBudget} MB</span>
  </div>

  <div class="actions-row">
    <button type="button" class="btn btn-ghost" disabled={running} onclick={refreshStatus}>Ping</button>
    <button type="button" class="btn btn-ghost" disabled={running} onclick={doWake}>Wake</button>
    <button type="button" class="btn btn-ghost" disabled={running} onclick={doStop}>Stop</button>
  </div>

  {#if actionMsg}
    <p class="action-msg">{actionMsg}</p>
  {/if}

  <div class="quick-tests">
    <span class="quick-label">Quick tests</span>
    <div class="quick-btns">
      {#each EDGE_QUICK_PROMPTS as q}
        <button type="button" class="btn btn-quick" disabled={running} onclick={() => fire(q.prompt)}>
          {q.label}
        </button>
      {/each}
    </div>
  </div>

  <div class="prompt-row">
    <input
      type="text"
      placeholder="Ton prompt (ex. 1+1=?)"
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
    <p class="shot-hint">La dernière réponse s’affiche ici puis disparaît (~45 s).</p>
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
    min-width: 64px;
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
    color: #5f6368;
    font-family: ui-monospace, monospace;
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
