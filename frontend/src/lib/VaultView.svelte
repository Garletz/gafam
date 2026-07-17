<script lang="ts">
  import { onMount } from 'svelte';

  let {
    vpcUrl = '',
    sessionToken = ''
  }: {
    vpcUrl: string;
    sessionToken: string;
  } = $props();

  type VaultNoteItem = {
    id: string;
    title: string;
    url: string;
    tags?: string;
    fetched_at: string;
    path?: string;
    snippet?: string;
  };

  let query = $state('');
  let searching = $state(false);
  let results: VaultNoteItem[] | null = $state(null);
  let recent: VaultNoteItem[] = $state([]);
  let openNote: any = $state(null);
  let noteLoading = $state(false);
  let errorMsg = $state('');

  function q(params: Record<string, string>) {
    return new URLSearchParams({ vpcUrl, token: sessionToken, ...params }).toString();
  }

  async function loadRecent() {
    try {
      const res = await fetch(`/api/proxy/research?${q({ action: 'notes', limit: '30' })}`);
      if (res.ok) {
        const data: any = await res.json();
        recent = data.notes || [];
      }
    } catch {}
  }

  async function search() {
    if (!query.trim() || searching) return;
    searching = true;
    errorMsg = '';
    try {
      const res = await fetch(`/api/proxy/research?${q({ action: 'search', q: query.trim(), limit: '20' })}`);
      const data: any = await res.json();
      if (res.ok) {
        results = data.results || [];
      } else {
        errorMsg = data.error || 'Search failed';
      }
    } catch (e: any) {
      errorMsg = e.message;
    } finally {
      searching = false;
    }
  }

  async function open(id: string) {
    noteLoading = true;
    openNote = null;
    try {
      const res = await fetch(`/api/proxy/research?${q({ action: 'note', id })}`);
      if (res.ok) openNote = await res.json();
      else errorMsg = 'Note not found';
    } catch {} finally {
      noteLoading = false;
    }
  }

  function formatDate(iso: string): string {
    try {
      return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' });
    } catch {
      return iso;
    }
  }

  function hostOf(url: string): string {
    try {
      return new URL(url).hostname;
    } catch {
      return url;
    }
  }

  onMount(loadRecent);
</script>

<div class="vault">
  <header class="vault__head">
    <div class="vault__head-left">
      <h3>Vault</h3>
      <span class="vault__sub">research memory · markdown is truth, SQLite is cache</span>
    </div>
  </header>

  <div class="vault__searchbar">
    <input
      type="text"
      placeholder="Search the vault — words are AND-ed…"
      bind:value={query}
      onkeydown={(e) => e.key === 'Enter' && search()}
      disabled={searching}
    />
    <button type="button" class="btn-primary" onclick={search} disabled={searching || !query.trim()}>
      {searching ? '…' : 'Search'}
    </button>
    {#if results !== null}
      <button type="button" class="btn-ghost" onclick={() => { results = null; query = ''; }}>Reset</button>
    {/if}
  </div>

  {#if errorMsg}
    <p class="vault__error">{errorMsg}</p>
  {/if}

  <div class="vault__body">
    <!-- Notes list -->
    <div class="vault__list">
      {#if results !== null}
        <div class="vault__list-title">{results.length} result{results.length === 1 ? '' : 's'}</div>
        {#each results as note}
          <button type="button" class="note-row" class:is-open={openNote?.id === note.id} onclick={() => open(note.id)}>
            <span class="note-row__title">{note.title}</span>
            {#if note.snippet}
              <span class="note-row__snippet">{@html note.snippet}</span>
            {/if}
            <span class="note-row__meta">{hostOf(note.url)} · {formatDate(note.fetched_at)}</span>
          </button>
        {:else}
          <p class="vault__empty">Nothing in the vault matches — fetch it first (research.fetch).</p>
        {/each}
      {:else}
        <div class="vault__list-title">Recent notes</div>
        {#each recent as note}
          <button type="button" class="note-row" class:is-open={openNote?.id === note.id} onclick={() => open(note.id)}>
            <span class="note-row__title">{note.title}</span>
            <span class="note-row__meta">{hostOf(note.url)} · {formatDate(note.fetched_at)}</span>
          </button>
        {:else}
          <div class="vault__empty vault__empty--big">
            <p>The vault is empty.</p>
            <span>Notes land here when a kāraka fetches sources (research.fetch),<br />and every future research reuses them.</span>
          </div>
        {/each}
      {/if}
    </div>

    <!-- Reader -->
    <div class="vault__reader">
      {#if noteLoading}
        <p class="vault__empty">Loading…</p>
      {:else if openNote}
        <div class="reader__head">
          <h4 class="reader__title">{openNote.title}</h4>
          <a class="reader__url" href={openNote.url} target="_blank" rel="noopener noreferrer">{openNote.url}</a>
          <div class="reader__meta">
            <span>{formatDate(openNote.fetched_at)}</span>
            {#if openNote.suggested_by}<span>· from {openNote.suggested_by}</span>{/if}
            {#if openNote.tags}<span>· {openNote.tags}</span>{/if}
          </div>
        </div>
        <pre class="reader__body">{openNote.text}</pre>
      {:else}
        <div class="vault__empty vault__empty--big">
          <p>Select a note to read it.</p>
        </div>
      {/if}
    </div>
  </div>
</div>

<style>
  .vault { display: flex; flex-direction: column; height: 100%; background: #fff; overflow: hidden; }

  .vault__head { padding: 8px 16px; border-bottom: 1px solid #dfe1e5; flex-shrink: 0; }
  .vault__head-left { display: flex; align-items: baseline; gap: 8px; }
  .vault__head h3 { margin: 0; font-size: 15px; font-weight: 600; color: #202124; }
  .vault__sub { font-size: 11px; color: #80868b; }

  .vault__searchbar { display: flex; gap: 8px; padding: 10px 16px; border-bottom: 1px solid #f1f3f4; flex-shrink: 0; }
  .vault__searchbar input {
    flex: 1; min-width: 0; padding: 8px 12px;
    border: 1px solid #dadce0; border-radius: 6px; font-size: 13px; color: #202124;
  }
  .vault__searchbar input:focus { outline: none; border-color: #202124; }

  .btn-primary { padding: 8px 14px; border: none; border-radius: 6px; background: #202124; color: #fff; font-size: 13px; font-weight: 600; cursor: pointer; }
  .btn-primary:disabled { opacity: .5; cursor: not-allowed; }
  .btn-ghost { padding: 8px 12px; border: 1px solid #dfe1e5; border-radius: 6px; background: #fff; color: #5f6368; font-size: 13px; font-weight: 600; cursor: pointer; }
  .btn-ghost:hover { background: #f1f3f4; }

  .vault__error { margin: 0; padding: 6px 16px; font-size: 12px; color: #d93025; background: #fce8e6; }

  .vault__body { flex: 1; min-height: 0; display: flex; gap: 1px; background: #dfe1e5; overflow: hidden; }
  .vault__list { flex: 0 0 42%; min-width: 0; background: #fff; overflow-y: auto; }
  .vault__reader { flex: 1; min-width: 0; background: #fff; display: flex; flex-direction: column; overflow: hidden; }

  .vault__list-title {
    padding: 8px 14px; font-size: 11px; font-weight: 700; text-transform: uppercase;
    letter-spacing: .04em; color: #80868b; border-bottom: 1px solid #f1f3f4;
    position: sticky; top: 0; background: #fff;
  }

  .note-row {
    display: flex; flex-direction: column; gap: 3px; width: 100%;
    padding: 10px 14px; border: none; border-bottom: 1px solid #f8f9fa;
    background: transparent; cursor: pointer; text-align: left;
  }
  .note-row:hover { background: #f8f9fa; }
  .note-row.is-open { background: #e8f0fe; }
  .note-row__title { font-size: 13px; font-weight: 600; color: #202124; line-height: 1.3; }
  .note-row__snippet {
    font-size: 11.5px; color: #5f6368; line-height: 1.4;
    display: -webkit-box; -webkit-line-clamp: 2; line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;
  }
  .note-row__snippet :global(b) { color: #1a73e8; font-weight: 700; }
  .note-row__meta { font-size: 10.5px; color: #9aa0a6; font-family: ui-monospace, monospace; }

  .vault__empty { padding: 24px 16px; text-align: center; color: #9aa0a6; font-size: 12.5px; }
  .vault__empty--big { display: flex; flex-direction: column; gap: 8px; align-items: center; justify-content: center; height: 100%; }
  .vault__empty--big p { margin: 0; font-size: 14px; font-weight: 600; color: #5f6368; }
  .vault__empty--big span { font-size: 12px; line-height: 1.5; }

  .reader__head { padding: 14px 18px 10px; border-bottom: 1px solid #f1f3f4; flex-shrink: 0; }
  .reader__title { margin: 0 0 4px; font-size: 15px; font-weight: 600; color: #202124; line-height: 1.3; }
  .reader__url { font-size: 11px; color: #1a73e8; font-family: ui-monospace, monospace; word-break: break-all; text-decoration: none; }
  .reader__url:hover { text-decoration: underline; }
  .reader__meta { display: flex; gap: 6px; margin-top: 6px; font-size: 10.5px; color: #9aa0a6; font-family: ui-monospace, monospace; flex-wrap: wrap; }

  .reader__body {
    flex: 1; min-height: 0; overflow-y: auto; margin: 0; padding: 14px 18px;
    font-family: ui-monospace, 'SF Mono', Menlo, monospace; font-size: 12px; line-height: 1.6;
    color: #202124; white-space: pre-wrap; word-break: break-word;
  }
</style>
