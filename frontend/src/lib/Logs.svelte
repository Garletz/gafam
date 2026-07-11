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
  });
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

  {#if selectedDay}
    <div class="logs-stream" bind:this={streamEl}>
      {#each filteredEntries as e (e.ts + e.tag + e.message.slice(0, 48))}
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
    background: #000;
    color: #e8e8e8;
  }
  .logs-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 12px;
    background: #0a0a0a;
    border-bottom: 1px solid #222;
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
    font-size: 13px;
    color: #fff;
    letter-spacing: 0.02em;
  }
  .logs-count {
    font-size: 11px;
    color: #888;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .live-btn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    background: transparent;
    border: 1px solid #333;
    color: #888;
    border-radius: 2px;
    padding: 3px 8px;
    font-size: 10px;
    font-weight: 700;
    letter-spacing: 0.08em;
    cursor: pointer;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .live-btn.on {
    border-color: #fff;
    color: #fff;
  }
  .live-btn.busy .live-dot {
    opacity: 0.4;
  }
  .live-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: #444;
  }
  .live-btn.on .live-dot {
    background: #fff;
    box-shadow: 0 0 0 0 #fff;
    animation: pulse 1.6s ease-out infinite;
  }
  @keyframes pulse {
    0% {
      box-shadow: 0 0 0 0 rgba(255, 255, 255, 0.45);
    }
    70% {
      box-shadow: 0 0 0 6px rgba(255, 255, 255, 0);
    }
    100% {
      box-shadow: 0 0 0 0 rgba(255, 255, 255, 0);
    }
  }
  .logs-toolbar select,
  .logs-toolbar input[type='search'] {
    background: #000;
    border: 1px solid #333;
    color: #e8e8e8;
    border-radius: 2px;
    padding: 4px 8px;
    font-size: 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .logs-toolbar input[type='search'] {
    width: 150px;
  }
  .btn {
    background: #111;
    color: #e8e8e8;
    border: 1px solid #333;
    border-radius: 2px;
    padding: 4px 10px;
    font-size: 12px;
    cursor: pointer;
  }
  .btn:hover:not(:disabled) {
    background: #1a1a1a;
    border-color: #555;
  }
  .btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .btn-ghost {
    background: transparent;
    color: #888;
  }
  .btn-ghost:hover:not(:disabled) {
    color: #fff;
    border-color: #666;
  }
  .logs-stream {
    flex: 1;
    overflow: auto;
    padding: 4px 0;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 12px;
    line-height: 1.4;
  }
  .log-line {
    display: flex;
    gap: 8px;
    padding: 1px 10px;
    white-space: nowrap;
  }
  .log-line:hover {
    background: #111;
  }
  .c-time {
    color: #666;
    flex: 0 0 64px;
  }
  .c-src {
    color: #aaa;
    flex: 0 0 36px;
    text-transform: uppercase;
    font-size: 10px;
    letter-spacing: 0.04em;
  }
  .c-lvl {
    flex: 0 0 12px;
    font-weight: 700;
    color: #fff;
  }
  .c-tag {
    color: #999;
    flex: 0 0 110px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .c-msg {
    color: #ddd;
    flex: 1;
    min-width: 0;
    white-space: pre-wrap;
    word-break: break-word;
  }
  .level-E .c-lvl,
  .level-E .c-msg {
    color: #fff;
    font-weight: 600;
  }
  .level-W .c-lvl {
    color: #ccc;
  }
  .level-D .c-lvl,
  .level-V .c-lvl {
    color: #777;
  }
  .logs-footer {
    flex-shrink: 0;
    display: flex;
    justify-content: center;
    align-items: center;
    padding: 8px;
    border-top: 1px solid #222;
    background: #0a0a0a;
  }
  .footer-meta {
    font-size: 11px;
    color: #666;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
  }
  .empty-stream {
    color: #666;
    text-align: center;
    padding: 24px;
    font-size: 12px;
  }
  .empty-stream.pad {
    padding-top: 80px;
  }
  .error {
    color: #fff;
    padding: 6px 12px;
    font-size: 12px;
    background: #1a1a1a;
    border-bottom: 1px solid #333;
  }
</style>
