<script lang="ts">
  import { onMount } from 'svelte';
  import type { LogDay, SuparnaReading, SuparnaStatus } from './types';
  import { fetchSuparnaStatus, invokeSuparnaAnalysis } from './suparnaApi';

  let {
    vpcUrl,
    sessionToken,
    selectedDay = null,
    days = []
  }: {
    vpcUrl: string;
    sessionToken: string;
    selectedDay?: string | null;
    days?: LogDay[];
  } = $props();

  let status: SuparnaStatus | null = $state(null);
  let loading = $state(false);
  let reading: SuparnaReading | null = $state(null);
  let error = $state('');

  async function refreshStatus() {
    status = await fetchSuparnaStatus(vpcUrl, sessionToken);
  }

  async function runAnalysis(refresh = false) {
    if (!selectedDay) return;
    loading = true;
    error = '';
    const result = await invokeSuparnaAnalysis(vpcUrl, sessionToken, selectedDay, refresh);
    loading = false;
    if (result.ok && result.reading) {
      reading = result.reading;
    } else {
      error = result.error || 'Suparna failed';
    }
    await refreshStatus();
  }

  function formatDayLabel(day: string) {
    try {
      return new Date(day + 'T12:00:00Z').toLocaleDateString(undefined, {
        weekday: 'short',
        month: 'short',
        day: 'numeric'
      });
    } catch {
      return day;
    }
  }

  const dayMeta = $derived(days.find((d) => d.day === selectedDay));

  onMount(() => {
    refreshStatus();
    const id = setInterval(refreshStatus, 8000);
    return () => clearInterval(id);
  });
</script>

<section class="panel">
  <div class="panel__intro panel__intro--row">
    <p>Wake Qwen on the node → analyze → auto-stop. Experimental lab.</p>
    <button type="button" class="btn" disabled={loading} onclick={() => refreshStatus()}>Refresh status</button>
  </div>

  <div class="resource-grid">
    <div class="resource-card">
      <span class="label">Model on disk</span>
      <span class="value">{status?.model_on_disk ? 'yes' : 'no'}</span>
    </div>
    <div class="resource-card">
      <span class="label">Qwen in RAM</span>
      <span class="value" class:on={status?.qwen_running}>{status?.qwen_running ? 'loaded' : 'stopped'}</span>
    </div>
    <div class="resource-card">
      <span class="label">Analysis</span>
      <span class="value" class:on={status?.analyze_running}>{status?.analyze_running ? 'running' : 'idle'}</span>
    </div>
    <div class="resource-card">
      <span class="label">Auto-stop</span>
      <span class="value">{status?.auto_stop !== false ? 'enabled' : 'off'}</span>
    </div>
  </div>

  {#if !selectedDay}
    <div class="empty">Select a day in the archive (left sidebar).</div>
  {:else}
    <div class="day-bar">
      <strong>{formatDayLabel(selectedDay)}</strong>
      {#if dayMeta}
        <span>{dayMeta.lines} lines · {(dayMeta.bytes / 1024).toFixed(1)} KB</span>
      {/if}
    </div>

    {#if !status?.model_on_disk}
      <div class="hint">Model not on VPC disk — run qwen-install on the node.</div>
    {:else if loading}
      <div class="hint">Loading Qwen into RAM (1–3 min on 1 Go VPS) · then analyzing · auto-stop after</div>
    {/if}

    <div class="actions">
      <button
        type="button"
        class="btn btn-primary"
        disabled={loading || !status?.model_on_disk}
        onclick={() => runAnalysis(false)}
      >
        {loading ? 'Analyzing…' : 'Analyze day'}
      </button>
      <button
        type="button"
        class="btn btn-ghost"
        disabled={loading || !reading}
        onclick={() => runAnalysis(true)}
      >
        Re-run
      </button>
    </div>

    {#if error}<div class="error">{error}</div>{/if}

    {#if reading}
      <div class="reading">
        <p class="summary">{reading.summary}</p>
        {#if reading.alerts && reading.alerts.length > 0}
          <ul class="alerts">
            {#each reading.alerts as a}
              <li><strong>{a.type}</strong> — {a.detail}</li>
            {/each}
          </ul>
        {/if}
        {#if reading.timeline && reading.timeline.length > 0}
          <div class="timeline">
            {#each reading.timeline as t}
              <div class="tl-row"><span>{t.time}</span><span>{t.app}</span><span>{t.event}</span></div>
            {/each}
          </div>
        {/if}
        <footer class="reading-foot">
          <span>{reading.engine ?? 'qwen'} · {reading.confidence ?? '—'}</span>
        </footer>
      </div>
    {/if}
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
  .panel__intro--row {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
  }
  .resource-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 10px;
    margin-bottom: 16px;
  }
  .resource-card {
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    padding: 10px 12px;
    background: #f8f9fa;
  }
  .resource-card .label {
    display: block;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: #80868b;
    margin-bottom: 4px;
  }
  .resource-card .value {
    font-size: 13px;
    font-weight: 600;
    font-family: ui-monospace, monospace;
  }
  .resource-card .value.on {
    color: #188038;
  }
  .day-bar {
    display: flex;
    gap: 12px;
    align-items: baseline;
    margin-bottom: 12px;
    font-size: 13px;
  }
  .day-bar span {
    color: #80868b;
    font-family: ui-monospace, monospace;
    font-size: 11px;
  }
  .actions {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
  }
  .btn {
    border-radius: 4px;
    padding: 8px 14px;
    font-size: 13px;
    cursor: pointer;
    border: 1px solid #dadce0;
    background: #f1f3f4;
    color: #202124;
  }
  .btn:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .btn-primary {
    background: #202124;
    color: #fff;
    border-color: #202124;
  }
  .btn-ghost {
    background: transparent;
  }
  .hint {
    padding: 8px 12px;
    font-size: 12px;
    color: #5f6368;
    background: #f8f9fa;
    border: 1px solid #e8eaed;
    border-radius: 4px;
    margin-bottom: 12px;
  }
  .error {
    color: #d93025;
    padding: 8px 12px;
    font-size: 12px;
    background: #fce8e6;
    border-radius: 4px;
    margin-bottom: 12px;
  }
  .reading {
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    padding: 14px 16px;
    background: #fff;
  }
  .summary {
    margin: 0 0 12px;
    font-size: 14px;
    line-height: 1.55;
  }
  .alerts {
    margin: 0 0 12px;
    padding-left: 18px;
    font-size: 12px;
    color: #5f6368;
  }
  .timeline {
    font-size: 11px;
    font-family: ui-monospace, monospace;
    color: #5f6368;
    margin-bottom: 10px;
  }
  .tl-row {
    display: grid;
    grid-template-columns: 48px 64px 1fr;
    gap: 8px;
    padding: 2px 0;
  }
  .reading-foot {
    font-size: 11px;
    color: #80868b;
  }
  .empty {
    color: #80868b;
    padding: 40px 0;
    text-align: center;
    font-size: 13px;
  }
</style>
