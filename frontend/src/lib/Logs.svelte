<script lang="ts">
  import { onMount } from 'svelte';

  let { vpcUrl, sessionToken }: { vpcUrl: string; sessionToken: string } = $props();

  type LogDay = { day: string; bytes: number; lines: number; updated_at: string };
  type LogEntry = { ts: number; source: string; level: string; tag: string; message: string };

  let days: LogDay[] = $state([]);
  let totalBytes = $state(0);
  let quotaBytes = $state(1 << 30);
  let selectedDay: string | null = $state(null);
  let entries: LogEntry[] = $state([]);
  let totalLines = $state(0);
  let offset = $state(0);
  let loading = $state(false);
  let errorMsg = $state('');
  let filterLevel = $state('ALL');
  let filterText = $state('');

  const limit = 500;

  async function loadDays() {
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
        selectDay(days[0].day);
      }
    } catch (e) {
      errorMsg = 'Network error loading logs';
    }
  }

  async function selectDay(day: string) {
    selectedDay = day;
    offset = 0;
    await loadEntries();
  }

  async function loadEntries(append = false) {
    if (!selectedDay) return;
    loading = true;
    try {
      const params = new URLSearchParams({
        vpcUrl,
        token: sessionToken,
        day: selectedDay,
        offset: String(offset),
        limit: String(limit)
      });
      const res = await fetch(`/api/proxy/logs?${params}`);
      if (!res.ok) {
        errorMsg = 'Failed to load day';
        return;
      }
      const data = await res.json();
      const batch: LogEntry[] = data.entries || [];
      entries = append ? [...entries, ...batch] : batch;
      totalLines = data.total_lines || 0;
      errorMsg = '';
    } catch (e) {
      errorMsg = 'Network error';
    } finally {
      loading = false;
    }
  }

  async function loadMore() {
    offset += limit;
    await loadEntries(true);
  }

  function formatBytes(n: number) {
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / (1024 * 1024)).toFixed(1)} MB`;
  }

  function formatTime(ts: number) {
    try {
      return new Date(ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    } catch {
      return String(ts);
    }
  }

  function formatDayLabel(day: string) {
    try {
      const d = new Date(day + 'T12:00:00Z');
      return d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric', year: 'numeric' });
    } catch {
      return day;
    }
  }

  let filteredEntries = $derived(() => {
    return entries.filter((e) => {
      if (filterLevel !== 'ALL' && e.level !== filterLevel) return false;
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

  onMount(() => {
    loadDays();
    const id = setInterval(loadDays, 15000);
    return () => clearInterval(id);
  });
</script>

<div class="logs-layout">
  <aside class="logs-sidebar">
    <div class="logs-quota">
      <span>Storage</span>
      <strong>{formatBytes(totalBytes)} / {formatBytes(quotaBytes)}</strong>
      <div class="quota-bar">
        <div class="quota-fill" style="width: {Math.min(100, (totalBytes / quotaBytes) * 100)}%"></div>
      </div>
    </div>
    <div class="logs-day-list">
      {#each days as d}
        <button class="day-item {selectedDay === d.day ? 'active' : ''}" onclick={() => selectDay(d.day)}>
          <div class="day-item__title">{formatDayLabel(d.day)}</div>
          <div class="day-item__meta">{d.lines} lines · {formatBytes(d.bytes)}</div>
        </button>
      {/each}
      {#if days.length === 0}
        <p class="empty">No logs yet. Pair the APK and wait a few seconds.</p>
      {/if}
    </div>
  </aside>

  <section class="logs-main">
    {#if selectedDay}
      <header class="logs-header">
        <h3>{formatDayLabel(selectedDay)}</h3>
        <div class="logs-filters">
          <select bind:value={filterLevel}>
            <option value="ALL">All levels</option>
            <option value="E">Error</option>
            <option value="W">Warn</option>
            <option value="I">Info</option>
            <option value="D">Debug</option>
            <option value="V">Verbose</option>
          </select>
          <input type="search" placeholder="Filter tag / message…" bind:value={filterText} />
          <button type="button" class="btn-refresh" onclick={() => { offset = 0; loadEntries(); }}>Refresh</button>
        </div>
      </header>

      {#if errorMsg}<div class="error">{errorMsg}</div>{/if}

      <div class="logs-stream">
        {#each filteredEntries() as e}
          <div class="log-line level-{e.level}">
            <span class="log-time">{formatTime(e.ts)}</span>
            <span class="log-src">{e.source}</span>
            <span class="log-level">{e.level}</span>
            <span class="log-tag">{e.tag}</span>
            <span class="log-msg">{e.message}</span>
          </div>
        {/each}
        {#if filteredEntries().length === 0 && !loading}
          <p class="empty">No entries for this filter.</p>
        {/if}
      </div>

      {#if entries.length < totalLines}
        <button class="btn-more" onclick={loadMore} disabled={loading}>
          {loading ? 'Loading…' : `Load more (${entries.length}/${totalLines})`}
        </button>
      {/if}
    {:else}
      <div class="logs-empty-main">
        <p>Select a day to inspect phone logs</p>
        <p class="hint">APK ships events + process logcat. Full system logcat needs ADB (bonus).</p>
      </div>
    {/if}
  </section>
</div>

<style>
  .logs-layout {
    display: flex;
    height: 100%;
    min-height: 0;
    background: #fafafa;
  }
  .logs-sidebar {
    width: 240px;
    border-right: 1px solid #dfe1e5;
    display: flex;
    flex-direction: column;
    background: #fff;
  }
  .logs-quota {
    padding: 14px 16px;
    border-bottom: 1px solid #dfe1e5;
    font-size: 12px;
    color: #5f6368;
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .logs-quota strong {
    color: #202124;
    font-size: 13px;
  }
  .quota-bar {
    height: 4px;
    background: #e8eaed;
    border-radius: 2px;
    overflow: hidden;
  }
  .quota-fill {
    height: 100%;
    background: #202124;
  }
  .logs-day-list {
    overflow-y: auto;
    flex: 1;
  }
  .day-item {
    width: 100%;
    text-align: left;
    border: none;
    background: transparent;
    padding: 12px 16px;
    border-bottom: 1px solid #f1f3f4;
    cursor: pointer;
  }
  .day-item:hover { background: #f8f9fa; }
  .day-item.active { background: #e8f0fe; }
  .day-item__title { font-weight: 600; color: #202124; font-size: 13px; }
  .day-item__meta { color: #80868b; font-size: 11px; margin-top: 2px; }
  .logs-main {
    flex: 1;
    display: flex;
    flex-direction: column;
    min-width: 0;
  }
  .logs-header {
    padding: 12px 16px;
    border-bottom: 1px solid #dfe1e5;
    background: #fff;
  }
  .logs-header h3 {
    margin: 0 0 10px;
    font-size: 15px;
  }
  .logs-filters {
    display: flex;
    gap: 8px;
  }
  .logs-filters select,
  .logs-filters input {
    padding: 6px 10px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    font-size: 13px;
  }
  .logs-filters input { flex: 1; }
  .btn-refresh, .btn-more {
    padding: 6px 12px;
    background: #202124;
    color: #fff;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-size: 12px;
  }
  .btn-more {
    margin: 12px auto;
    display: block;
  }
  .btn-more:disabled { background: #80868b; }
  .logs-stream {
    flex: 1;
    overflow-y: auto;
    padding: 8px 12px;
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 11.5px;
    background: #0f1115;
    color: #e8eaed;
  }
  .log-line {
    display: grid;
    grid-template-columns: 72px 42px 18px 90px 1fr;
    gap: 8px;
    padding: 3px 0;
    border-bottom: 1px solid #1a1d24;
    align-items: start;
  }
  .log-time { color: #80868b; }
  .log-src { color: #8ab4f8; text-transform: uppercase; font-size: 10px; }
  .log-level { font-weight: 700; }
  .level-E .log-level, .level-E .log-msg { color: #f28b82; }
  .level-W .log-level, .level-W .log-msg { color: #fdd663; }
  .level-I .log-level { color: #81c995; }
  .level-D .log-level { color: #aecbfa; }
  .log-tag { color: #c58af9; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .log-msg { white-space: pre-wrap; word-break: break-word; }
  .empty, .logs-empty-main {
    color: #80868b;
    padding: 24px;
    text-align: center;
  }
  .hint { font-size: 12px; margin-top: 8px; }
  .error {
    color: #d93025;
    padding: 8px 16px;
    font-size: 13px;
  }
</style>
