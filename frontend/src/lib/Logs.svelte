<script lang="ts">
  import { onMount } from 'svelte';

  type LogDay = { day: string; bytes: number; lines: number; updated_at: string };
  type LogEntry = { ts: number; source: string; level: string; tag: string; message: string };

  let {
    vpcUrl,
    sessionToken,
    selectedDay = $bindable<string | null>(null),
    days = $bindable<LogDay[]>([]),
    totalBytes = $bindable(0),
    quotaBytes = $bindable(1 << 30)
  }: {
    vpcUrl: string;
    sessionToken: string;
    selectedDay?: string | null;
    days?: LogDay[];
    totalBytes?: number;
    quotaBytes?: number;
  } = $props();

  let entries: LogEntry[] = $state([]);
  let totalLines = $state(0);
  let offset = $state(0);
  let loading = $state(false);
  let liveBusy = $state(false);
  let clearing = $state(false);
  let errorMsg = $state('');
  let filterLevel = $state('ALL');
  let filterSource = $state('ALL');
  let filterText = $state('');
  let live = $state(true);
  let streamEl: HTMLDivElement | undefined = $state();
  let loadGen = 0;

  type SuparnaReading = {
    day: string;
    summary: string;
    timeline?: Array<{ time: string; app: string; event: string; severity: string }>;
    alerts?: Array<{ type: string; detail: string; codes?: string[] }>;
    confidence?: string;
    engine?: string;
    model_ready?: boolean;
  };

  let suparnaLoading = $state(false);
  let suparnaReading: SuparnaReading | null = $state(null);
  let suparnaError = $state('');
  let suparnaStatus: { model_on_disk?: boolean; qwen_running?: boolean } | null = $state(null);

  const limit = 800;
  const LIVE_MS = 2000;

  async function refreshDays() {
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/logs?${params}`);
      if (!res.ok) {
        errorMsg = 'Failed to load log days';
        return;
      }
      const data = await res.json();
      days = data.days || [];
      totalBytes = data.total_bytes || 0;
      quotaBytes = data.quota_bytes || 1 << 30;
      errorMsg = '';
      if (!selectedDay && days.length > 0) {
        selectedDay = days[0].day;
      }
    } catch {
      errorMsg = 'Network error loading logs';
    }
  }

  async function loadEntries(opts: { append?: boolean; silent?: boolean } = {}) {
    const { append = false, silent = false } = opts;
    if (!selectedDay) return;
    // Live follow only the newest window; don't clobber "Older lines"
    if (silent && offset > 0) return;
    const gen = ++loadGen;
    if (!silent) loading = true;
    else liveBusy = true;
    try {
      const params = new URLSearchParams({
        vpcUrl,
        token: sessionToken,
        day: selectedDay,
        offset: String(append ? offset : 0),
        limit: String(limit)
      });
      const res = await fetch(`/api/proxy/logs?${params}`);
      if (gen !== loadGen) return;
      if (!res.ok) {
        if (!silent) errorMsg = 'Failed to load day';
        return;
      }
      const data = await res.json();
      if (gen !== loadGen) return;
      const batch: LogEntry[] = data.entries || [];
      if (append) {
        entries = [...entries, ...batch];
      } else {
        offset = 0;
        entries = batch;
      }
      totalLines = data.total_lines || 0;
      errorMsg = '';
      if (!append && !silent && streamEl) {
        requestAnimationFrame(() => {
          if (streamEl) streamEl.scrollTop = 0;
        });
      }
    } catch {
      if (!silent) errorMsg = 'Network error';
    } finally {
      if (gen === loadGen) {
        loading = false;
        liveBusy = false;
      }
    }
  }

  async function loadMore() {
    offset += limit;
    await loadEntries({ append: true });
  }

  async function clearLogs(all: boolean) {
    if (!selectedDay && !all) return;
    const label = all ? 'all archived days' : formatDayLabel(selectedDay!);
    if (!confirm(`Clear ${label}? This cannot be undone.`)) return;
    clearing = true;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      if (!all && selectedDay) params.set('day', selectedDay);
      const res = await fetch(`/api/proxy/logs?${params}`, { method: 'DELETE' });
      if (!res.ok) {
        errorMsg = 'Clear failed — redeploy VPC if DELETE is missing';
        return;
      }
      if (all) {
        selectedDay = null;
        entries = [];
        totalLines = 0;
      } else {
        entries = [];
        totalLines = 0;
      }
      await refreshDays();
      if (selectedDay) await loadEntries({ silent: true });
    } catch {
      errorMsg = 'Clear failed';
    } finally {
      clearing = false;
    }
  }

  function formatTime(ts: number) {
    try {
      const d = new Date(ts);
      return d.toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        second: '2-digit',
        hour12: false
      });
    } catch {
      return String(ts);
    }
  }

  function formatDayLabel(day: string) {
    try {
      const d = new Date(day + 'T12:00:00Z');
      return d.toLocaleDateString(undefined, {
        weekday: 'short',
        month: 'short',
        day: 'numeric'
      });
    } catch {
      return day;
    }
  }

  let filteredEntries = $derived.by(() => {
    return entries.filter((e) => {
      if (filterLevel !== 'ALL' && e.level !== filterLevel) return false;
      if (filterSource !== 'ALL' && e.source !== filterSource) return false;
      if (filterText) {
        const q = filterText.toLowerCase();
        return (
          e.message.toLowerCase().includes(q) ||
          e.tag.toLowerCase().includes(q) ||
          e.source.toLowerCase().includes(q)
        );
      }
      return true;
    });
  });

  $effect(() => {
    if (selectedDay) {
      offset = 0;
      entries = [];
      loadEntries({ append: false });
    }
  });

  $effect(() => {
    if (!live || !selectedDay) return;
    const id = setInterval(() => {
      loadEntries({ silent: true });
      refreshDays();
    }, LIVE_MS);
    return () => clearInterval(id);
  });

  onMount(() => {
    refreshDays();
    fetchSuparnaStatus();
  });

  async function fetchSuparnaStatus() {
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/suparna?${params}`);
      if (res.ok) suparnaStatus = await res.json();
    } catch {
      /* ignore */
    }
  }

  async function invokeSuparna(refresh = false) {
    if (!selectedDay) return;
    suparnaLoading = true;
    suparnaError = '';
    try {
      const params = new URLSearchParams({
        vpcUrl,
        token: sessionToken,
        day: selectedDay,
        refresh: refresh ? '1' : '0'
      });
      const res = await fetch(`/api/proxy/suparna?${params}`, { method: 'POST' });
      const data = await res.json();
      if (!res.ok) {
        suparnaError = data.error || 'Suparna failed';
        return;
      }
      suparnaReading = data;
      await fetchSuparnaStatus();
    } catch {
      suparnaError = 'Network error';
    } finally {
      suparnaLoading = false;
      await fetchSuparnaStatus();
    }
  }
</script>

<section class="logs-viewer">
  <header class="logs-toolbar">
    <div class="logs-toolbar__left">
      <span class="logs-title">{selectedDay ? formatDayLabel(selectedDay) : 'Logs'}</span>
      {#if selectedDay}
        <span class="logs-count">{filteredEntries.length}/{totalLines}</span>
      {/if}
      <button
        type="button"
        class="live-btn {live ? 'on' : ''}"
        class:busy={liveBusy}
        onclick={() => (live = !live)}
        title={live ? 'Live: polling every 2s' : 'Paused — click to follow'}
      >
        <span class="live-dot"></span>
        {live ? 'LIVE' : 'PAUSED'}
      </button>
    </div>
    <div class="logs-toolbar__right">
      <select bind:value={filterSource} title="Source">
        <option value="ALL">src</option>
        <option value="apk">apk</option>
        <option value="event">event</option>
        <option value="adb">adb</option>
      </select>
      <select bind:value={filterLevel} title="Level">
        <option value="ALL">lvl</option>
        <option value="E">E</option>
        <option value="W">W</option>
        <option value="I">I</option>
        <option value="D">D</option>
        <option value="V">V</option>
      </select>
      <input type="search" placeholder="grep…" bind:value={filterText} />
      <button
        type="button"
        class="btn btn-suparna"
        disabled={!selectedDay || suparnaLoading || !suparnaStatus?.model_on_disk}
        onclick={() => invokeSuparna(false)}
        title="Wake Qwen → analyze day → auto-stop (uses RAM briefly)"
      >
        {suparnaLoading ? 'Analyzing…' : 'Suparna'}
      </button>
      <button
        type="button"
        class="btn"
        disabled={loading}
        onclick={() => {
          offset = 0;
          loadEntries({ append: false });
          refreshDays();
        }}
      >
        Refresh
      </button>
      <button
        type="button"
        class="btn btn-ghost"
        disabled={clearing || !selectedDay}
        onclick={() => clearLogs(false)}
        title="Clear selected day"
      >
        Clear day
      </button>
      <button
        type="button"
        class="btn btn-ghost"
        disabled={clearing || days.length === 0}
        onclick={() => clearLogs(true)}
        title="Clear entire archive"
      >
        Clear all
      </button>
    </div>
  </header>

  {#if errorMsg}<div class="error">{errorMsg}</div>{/if}
  {#if suparnaError}<div class="error">{suparnaError}</div>{/if}

  {#if selectedDay}
    {#if !suparnaStatus?.model_on_disk}
      <div class="suparna-hint">Suparna: model not on VPC disk yet (run qwen-install on the node).</div>
    {:else if suparnaLoading}
      <div class="suparna-hint">Loading Qwen into RAM (1–3 min on 1 Go VPS) · then analyzing · auto-stop after</div>
    {/if}
    {#if suparnaReading}
      <div class="suparna-panel">
        <p class="suparna-summary">{suparnaReading.summary}</p>
        {#if suparnaReading.alerts && suparnaReading.alerts.length > 0}
          <ul class="suparna-alerts">
            {#each suparnaReading.alerts as a}
              <li><strong>{a.type}</strong> — {a.detail}</li>
            {/each}
          </ul>
        {/if}
        {#if suparnaReading.timeline && suparnaReading.timeline.length > 0}
          <div class="suparna-timeline">
            {#each suparnaReading.timeline as t}
              <div class="tl-row"><span>{t.time}</span><span>{t.app}</span><span>{t.event}</span></div>
            {/each}
          </div>
        {/if}
        <div class="suparna-foot">
          <span>{suparnaReading.engine} · {suparnaReading.confidence}</span>
          <button type="button" class="btn btn-ghost" disabled={suparnaLoading} onclick={() => invokeSuparna(true)}>
            Re-run
          </button>
        </div>
      </div>
    {/if}

    <div class="logs-stream" bind:this={streamEl}>
      {#each filteredEntries as e}
        <div class="log-line level-{e.level}">
          <span class="c-time">{formatTime(e.ts)}</span>
          <span class="c-src">{e.source}</span>
          <span class="c-lvl">{e.level}</span>
          <span class="c-tag">{e.tag}</span>
          <span class="c-msg">{e.message}</span>
        </div>
      {/each}
      {#if filteredEntries.length === 0 && !loading}
        <div class="empty-stream">No lines match this filter.</div>
      {/if}
      {#if loading && entries.length === 0}
        <div class="empty-stream">Loading…</div>
      {/if}
    </div>

    <footer class="logs-footer">
      {#if entries.length < totalLines}
        <button class="btn" onclick={loadMore} disabled={loading || liveBusy}>
          {loading ? '…' : `Older lines (${entries.length}/${totalLines})`}
        </button>
      {:else}
        <span class="footer-meta">End · {totalLines} lines{#if live} · following{/if}</span>
      {/if}
    </footer>
  {:else}
    <div class="empty-stream pad">Select a day in the archive.</div>
  {/if}
</section>

<style>
  .logs-viewer {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: #ffffff;
    color: #202124;
  }
  .logs-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 10px 16px;
    background: #ffffff;
    border-bottom: 1px solid #dfe1e5;
    flex-shrink: 0;
    flex-wrap: wrap;
  }
  .logs-toolbar__left,
  .logs-toolbar__right {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  .logs-title {
    font-weight: 600;
    font-size: 15px;
    color: #202124;
  }
  .logs-count {
    font-size: 11px;
    color: #80868b;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .live-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px solid #dadce0;
    color: #5f6368;
    border-radius: 4px;
    padding: 3px 8px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    cursor: pointer;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .live-btn.on {
    border-color: #202124;
    color: #202124;
  }
  .live-btn.busy .live-dot {
    opacity: 0.4;
  }
  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #dadce0;
  }
  .live-btn.on .live-dot {
    background: #202124;
    box-shadow: 0 0 0 0 rgba(32, 33, 36, 0.35);
    animation: pulse 1.6s ease-out infinite;
  }
  @keyframes pulse {
    0% {
      box-shadow: 0 0 0 0 rgba(32, 33, 36, 0.35);
    }
    70% {
      box-shadow: 0 0 0 6px rgba(32, 33, 36, 0);
    }
    100% {
      box-shadow: 0 0 0 0 rgba(32, 33, 36, 0);
    }
  }
  .logs-toolbar select,
  .logs-toolbar input[type='search'] {
    background: #ffffff;
    border: 1px solid #dadce0;
    color: #202124;
    border-radius: 4px;
    padding: 4px 8px;
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .logs-toolbar input[type='search'] {
    width: 150px;
  }
  .btn {
    background: #f1f3f4;
    color: #202124;
    border: 1px solid #dadce0;
    border-radius: 4px;
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    background: #e8eaed;
    border-color: #bdc1c6;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .btn-ghost {
    background: transparent;
    color: #5f6368;
  }
  .btn-ghost:hover:not(:disabled) {
    color: #202124;
    border-color: #bdc1c6;
  }
  .logs-stream {
    flex: 1;
    overflow: auto;
    padding: 4px 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 12px;
    line-height: 1.45;
    background: #ffffff;
  }
  .log-line {
    display: flex;
    gap: 8px;
    padding: 2px 14px;
    white-space: nowrap;
  }
  .log-line:hover {
    background: #f8f9fa;
  }
  .c-time {
    color: #80868b;
    flex: 0 0 64px;
  }
  .c-src {
    color: #5f6368;
    flex: 0 0 36px;
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.04em;
  }
  .c-lvl {
    flex: 0 0 12px;
    font-weight: 700;
    color: #202124;
  }
  .c-tag {
    color: #5f6368;
    flex: 0 0 110px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .c-msg {
    color: #202124;
    flex: 1;
    min-width: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .level-E .c-lvl,
  .level-E .c-msg {
    color: #d93025;
    font-weight: 600;
  }
  .level-W .c-lvl {
    color: #e37400;
  }
  .level-I .c-lvl {
    color: #188038;
  }
  .level-D .c-lvl,
  .level-V .c-lvl {
    color: #80868b;
  }
  .logs-footer {
    flex-shrink: 0;
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 8px;
    border-top: 1px solid #dfe1e5;
    background: #ffffff;
  }
  .footer-meta {
    font-size: 11px;
    color: #80868b;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .empty-stream {
    color: #80868b;
    text-align: center;
    padding: 24px;
    font-size: 12px;
  }
  .empty-stream.pad {
    padding-top: 80px;
  }
  .error {
    color: #d93025;
    padding: 6px 16px;
    font-size: 12px;
    background: #fce8e6;
    border-bottom: 1px solid #f5c2c0;
  }
  .btn-suparna {
    background: #202124;
    color: #fff;
    border-color: #202124;
  }
  .btn-suparna:hover:not(:disabled) {
    background: #3c4043;
  }
  .suparna-hint {
    padding: 8px 16px;
    font-size: 12px;
    color: #5f6368;
    background: #f8f9fa;
    border-bottom: 1px solid #e8eaed;
  }
  .suparna-panel {
    padding: 12px 16px;
    border-bottom: 1px solid #dfe1e5;
    background: #ffffff;
    max-height: 220px;
    overflow-y: auto;
    flex-shrink: 0;
  }
  .suparna-summary {
    margin: 0 0 10px;
    font-size: 13px;
    line-height: 1.5;
  }
  .suparna-alerts {
    margin: 0 0 10px;
    padding-left: 18px;
    font-size: 12px;
    color: #5f6368;
  }
  .suparna-timeline {
    font-size: 11px;
    font-family: ui-monospace, monospace;
    color: #5f6368;
    margin-bottom: 8px;
  }
  .tl-row {
    display: grid;
    grid-template-columns: 48px 64px 1fr;
    gap: 8px;
    padding: 2px 0;
  }
  .suparna-foot {
    display: flex;
    justify-content: space-between;
    align-items: center;
    font-size: 11px;
    color: #80868b;
  }
</style>
