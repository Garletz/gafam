<script lang="ts">
  import { onMount, onDestroy } from 'svelte';
  import { e2eFetch } from './e2eFetch';

  let {
    vpcUrl = '',
    sessionToken = ''
  }: {
    vpcUrl: string;
    sessionToken: string;
  } = $props();

  type Run = {
    id: string;
    instruction: string;
    status: string;
    phase: string;
    repo?: string;
    branch?: string;
    pr_url?: string;
    summary?: string;
    error?: string;
    log?: string;
    auto_merge?: boolean;
    created_at?: string;
    updated_at?: string;
  };
  type Provider = { id: string; name: string; model: string; enabled: boolean };
  type Session = { id: string; title: string; source?: string; directory?: string };

  let status = $state<any>(null);
  let runs: Run[] = $state([]);
  let sessions: Session[] = $state([]);
  let selected: Run | null = $state(null);
  let sessionPreview = $state<any>(null);
  let instruction = $state('');
  let autoMerge = $state(true);
  let busy = $state(false);
  let message = $state('');
  let errorMsg = $state('');
  let settings = $state({
    enabled: true,
    repo: 'Garletz/gafam',
    auto_merge: true,
    has_token: false,
    token_hint: '',
    provider_id: '',
    model: ''
  });
  let tokenInput = $state('');
  let providers: Provider[] = $state([]);
  let importMsg = $state('');
  let importFile: HTMLInputElement | undefined = $state();
  let pollTimer: ReturnType<typeof setInterval> | null = null;

  function q(params: Record<string, string>) {
    return new URLSearchParams({ vpcUrl, token: sessionToken, ...params }).toString();
  }

  async function loadStatus() {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'status' })}`);
      if (res.ok) status = await res.json();
    } catch {}
  }

  async function loadRuns() {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'runs' })}`);
      if (res.ok) {
        const data: any = await res.json();
        runs = data.runs || [];
        if (selected) {
          const fresh = runs.find((r) => r.id === selected?.id);
          if (fresh) await openRun(fresh.id, false);
        }
        if (runs.some((r) => ['queued', 'running'].includes(r.status))) ensurePoll();
        else stopPoll();
      }
    } catch {}
  }

  async function loadSettings() {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'settings' })}`);
      if (res.ok) {
        settings = { ...settings, ...(await res.json()) };
        autoMerge = settings.auto_merge;
      }
    } catch {}
  }

  async function loadProviders() {
    try {
      const res = await fetch(`/api/proxy/llm?${q({ action: 'providers' })}`);
      if (res.ok) {
        const data: any = await res.json();
        providers = (data.providers || []).filter((p: Provider) => p.enabled);
      }
    } catch {}
  }

  async function loadSessions() {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'sessions' })}`);
      if (res.ok) {
        const data: any = await res.json();
        sessions = data.sessions || [];
      }
    } catch {}
  }

  async function openRun(id: string, scroll = true) {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'run', id })}`);
      if (res.ok) {
        selected = await res.json();
        if (scroll) setTimeout(() => document.getElementById('daksina-log')?.scrollTo(0, 0), 50);
        if (selected && !['done', 'failed', 'cancelled', 'pr_open', 'interrupted'].includes(selected.status)) {
          ensurePoll();
        }
      }
    } catch {}
  }

  async function openSession(id: string) {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'session', id })}`);
      if (res.ok) sessionPreview = await res.json();
    } catch {}
  }

  async function startRun() {
    if (!instruction.trim() || busy) return;
    busy = true;
    errorMsg = '';
    message = '';
    try {
      const data = await e2eFetch(
        `/api/proxy/daksina?${q({ action: 'runs' })}`,
        sessionToken,
        { method: 'POST', body: JSON.stringify({ instruction: instruction.trim(), auto_merge: autoMerge }) }
      );
      if (data?.error) {
        errorMsg = data.error;
      } else {
        message = `Mission ${data.id} lancée — ${data.status}`;
        instruction = '';
        await loadRuns();
        if (data.id) await openRun(data.id);
        ensurePoll();
      }
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function cancelRun(id: string) {
    try {
      const res = await fetch(`/api/proxy/daksina?${q({ action: 'run', id })}`, { method: 'DELETE' });
      if (res.ok) {
        message = `Mission ${id} annulée`;
        await loadRuns();
      }
    } catch {}
  }

  async function wake() {
    busy = true;
    try {
      const r = await e2eFetch(`/api/proxy/daksina?${q({ action: 'wake' })}`, sessionToken, { method: 'POST' });
      if (r?.error) errorMsg = r.error;
      await loadStatus();
    } finally {
      busy = false;
    }
  }

  async function stop() {
    busy = true;
    try {
      const r = await e2eFetch(`/api/proxy/daksina?${q({ action: 'stop' })}`, sessionToken, { method: 'POST' });
      if (r?.error) errorMsg = r.error;
      await loadStatus();
    } finally {
      busy = false;
    }
  }

  async function saveSettings() {
    if (busy) return;
    busy = true;
    errorMsg = '';
    try {
      const payload: any = {
        enabled: settings.enabled,
        repo: settings.repo,
        auto_merge: settings.auto_merge,
        provider_id: settings.provider_id,
        model: settings.model
      };
      if (tokenInput.trim()) payload.github_token = tokenInput.trim();
      const data = await e2eFetch(
        `/api/proxy/daksina?${q({ action: 'settings' })}`,
        sessionToken,
        { method: 'POST', body: JSON.stringify(payload) }
      );
      if (data?.error) errorMsg = data.error;
      else {
        tokenInput = '';
        message = 'Réglages Dakṣiṇā enregistrés ✓';
        await loadSettings();
        await loadStatus();
      }
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function clearToken() {
    if (busy) return;
    busy = true;
    try {
      const data = await e2eFetch(
        `/api/proxy/daksina?${q({ action: 'settings' })}`,
        sessionToken,
        { method: 'POST', body: JSON.stringify({ clear_token: true }) }
      );
      if (data?.error) errorMsg = data.error;
      else {
        message = 'Token GitHub supprimé';
        await loadSettings();
        await loadStatus();
      }
    } finally {
      busy = false;
    }
  }

  async function importSessions() {
    const file = importFile?.files?.[0];
    if (!file) return;
    busy = true;
    importMsg = '';
    try {
      const text = await file.text();
      const data = await e2eFetch(
        `/api/proxy/daksina?${q({ action: 'import-session' })}`,
        sessionToken,
        { method: 'POST', body: text }
      );
      const n = data?.imported?.length || 0;
      importMsg = n ? `${n} session(s) importée(s) ✓` : 'Aucune session valide dans ce fichier';
      if (importFile) importFile.value = '';
      await loadSessions();
    } catch (e: any) {
      importMsg = e.message || 'Import failed';
    } finally {
      busy = false;
    }
  }

  function ensurePoll() {
    if (pollTimer) return;
    pollTimer = setInterval(async () => {
      await loadRuns();
      if (selected) await openRun(selected.id, false);
      await loadStatus();
    }, 4000);
  }

  function stopPoll() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  function formatDate(value?: string): string {
    if (!value) return '';
    try {
      return new Date(value.replace(' ', 'T') + 'Z').toLocaleString(undefined, {
        month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
      });
    } catch {
      return value;
    }
  }

  function statusLabel(r: Run | null): string {
    if (!r) return '';
    const map: Record<string, string> = {
      queued: 'en attente',
      running: 'en cours',
      done: 'terminée',
      failed: 'échec',
      cancelled: 'annulée',
      pr_open: 'PR ouverte',
      interrupted: 'interrompue'
    };
    return map[r.status] || r.status;
  }

  onMount(async () => {
    await Promise.all([loadStatus(), loadRuns(), loadSettings(), loadProviders(), loadSessions()]);
    ensurePoll();
  });

  onDestroy(stopPoll);
</script>

<div class="daksina">
  <header class="daksina__head">
    <div>
      <h3>Dakṣiṇā</h3>
      <p>opencode headless · GitHub auto-merge si tests verts · manifeste 27</p>
    </div>
    <div class="daksina__state">
      <span class="dot" class:on={status?.running} class:healthy={status?.healthy}></span>
      <span>{status?.running ? (status?.healthy ? 'runner prêt' : 'runner en démarrage') : 'runner endormi'}</span>
      {#if status?.running}
        <button type="button" class="btn-ghost" onclick={stop} disabled={busy}>Stop</button>
      {:else}
        <button type="button" class="btn-ghost" onclick={wake} disabled={busy}>Wake</button>
      {/if}
    </div>
  </header>

  {#if errorMsg}
    <p class="banner banner--error">{errorMsg}</p>
  {/if}
  {#if message}
    <p class="banner banner--ok">{message}</p>
  {/if}

  <div class="daksina__grid">
    <section class="card">
      <h4>Lancer une mission</h4>
      <textarea
        bind:value={instruction}
        placeholder="Ex: Corrige le bug de resize du browser, ajoute un test de régression, puis laisse le runner merger si tout est vert."
        rows="5"
      ></textarea>
      <label class="check">
        <input type="checkbox" bind:checked={autoMerge} />
        Merge automatique si les tests passent
      </label>
      <button type="button" class="btn-primary" onclick={startRun} disabled={busy || !instruction.trim() || !settings.has_token}>
        {busy ? '…' : 'Lancer Dakṣiṇā'}
      </button>
      {#if !settings.has_token}
        <p class="hint">Configure un token GitHub ci-dessous avant de lancer.</p>
      {/if}
    </section>

    <section class="card">
      <h4>Réglages</h4>
      <label class="field">
        <span>Dépôt GitHub</span>
        <input type="text" bind:value={settings.repo} placeholder="Garletz/gafam" />
      </label>
      <label class="field">
        <span>Token GitHub {#if settings.has_token}<em>configuré {settings.token_hint}</em>{/if}</span>
        <input type="password" bind:value={tokenInput} placeholder={settings.has_token ? 'laisser vide pour conserver' : 'ghp_…'} autocomplete="off" />
      </label>
      <p class="hint">
        Ce token est utilisé par le harnais <em>et</em> par l'agent opencode (GH_TOKEN) : il a donc
        un accès GitHub complet. Utilise un token dédié, révocable (contents, pull requests, workflows).
      </p>
      <label class="field">
        <span>Provider LLM</span>
        <select bind:value={settings.provider_id}>
          <option value="">Premier provider disponible</option>
          {#each providers as p}
            <option value={p.id}>{p.name} — {p.model}</option>
          {/each}
        </select>
      </label>
      <label class="field">
        <span>Modèle (optionnel)</span>
        <input type="text" bind:value={settings.model} placeholder="override du modèle" />
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={settings.enabled} />
        Dakṣiṇā activée (SMS /d autorisé)
      </label>
      <label class="check">
        <input type="checkbox" bind:checked={settings.auto_merge} />
        Auto-merge par défaut
      </label>
      <button type="button" class="btn-primary" onclick={saveSettings} disabled={busy}>Enregistrer</button>
      {#if settings.has_token}
        <button type="button" class="btn-danger token-clear" onclick={clearToken} disabled={busy}>Supprimer le token</button>
      {/if}
    </section>
  </div>

  <section class="card">
    <div class="card__head">
      <h4>Missions</h4>
      <span class="muted">{runs.length} run(s)</span>
    </div>
    <div class="runs">
      {#each runs as run}
        <button type="button" class="run" class:active={selected?.id === run.id} onclick={() => openRun(run.id)}>
          <span class="run__id">{run.id}</span>
          <span class="run__instruction">{run.instruction}</span>
          <span class="pill pill--{run.status}">{statusLabel(run)}</span>
          {#if run.pr_url}
            <a class="run__pr" href={run.pr_url} target="_blank" rel="noopener noreferrer" onclick={(e) => e.stopPropagation()}>PR ↗</a>
          {/if}
          <span class="run__date">{formatDate(run.updated_at)}</span>
        </button>
      {:else}
        <p class="muted pad">Aucune mission. Envoie <code>/d &lt;instruction&gt;</code> par SMS ou lance-en une ici.</p>
      {/each}
    </div>
  </section>

  {#if selected}
    <section class="card">
      <div class="card__head">
        <h4>{selected.id} · {statusLabel(selected)}</h4>
        <div class="card__actions">
          {#if selected.pr_url}
            <a class="btn-ghost" href={selected.pr_url} target="_blank" rel="noopener noreferrer">Voir la PR ↗</a>
          {/if}
          {#if !['done', 'failed', 'cancelled', 'pr_open', 'interrupted'].includes(selected.status)}
            <button type="button" class="btn-danger" onclick={() => selected && cancelRun(selected.id)}>Annuler</button>
          {/if}
        </div>
      </div>
      <div class="run-meta">
        <span>phase: <strong>{selected.phase || '—'}</strong></span>
        {#if selected.branch}<span>branche: <code>{selected.branch}</code></span>{/if}
        <span>auto-merge: <strong>{selected.auto_merge ? 'oui' : 'non'}</strong></span>
      </div>
      {#if selected.error}<p class="banner banner--error">{selected.error}</p>{/if}
      {#if selected.summary}<p class="summary">{selected.summary}</p>{/if}
      <pre id="daksina-log" class="log">{selected.log || 'Pas encore de log — le runner démarre…'}</pre>
    </section>
  {/if}

  <section class="card">
    <div class="card__head">
      <h4>Mémoire opencode</h4>
      <div class="card__actions">
        <label class="btn-ghost file">
          Importer un export
          <input bind:this={importFile} type="file" accept="application/json,.json" onchange={importSessions} />
        </label>
      </div>
    </div>
    <p class="hint">
      Sur ton poste : <code>opencode export &lt;session-id&gt; &gt; session.json</code> puis importe le fichier ici.
      Les sessions importées et celles du runner sont injectées dans le contexte de chaque mission.
    </p>
    {#if importMsg}<p class="banner banner--ok">{importMsg}</p>{/if}
    <div class="sessions">
      {#each sessions as s}
        <button type="button" class="session" onclick={() => openSession(s.id)}>
          <span class="session__title">{s.title || s.id}</span>
          <span class="session__meta">{s.source || 'opencode'} · {s.id}</span>
        </button>
      {:else}
        <p class="muted pad">Aucune conversation pour l'instant.</p>
      {/each}
    </div>
    {#if sessionPreview}
      <div class="preview">
        <div class="card__head">
          <h4>{sessionPreview.info?.title || 'Conversation'}</h4>
          <button type="button" class="btn-ghost" onclick={() => (sessionPreview = null)}>Fermer</button>
        </div>
        <div class="preview__body">
          {#each (sessionPreview.messages || []).slice(-8) as msg}
            <div class="msg">
              <span class="msg__role">{msg.info?.role || '?'}</span>
              {#each (msg.parts || []).filter((p: any) => p.type === 'text') as part}
                <p>{part.text}</p>
              {/each}
            </div>
          {/each}
        </div>
      </div>
    {/if}
  </section>
</div>

<style>
  .daksina { height: 100%; overflow-y: auto; padding: 14px 18px 32px; background: #f7f8fa; color: #202124; }
  .daksina__head { display: flex; justify-content: space-between; gap: 16px; align-items: flex-start; margin-bottom: 12px; }
  .daksina__head h3 { margin: 0; font-size: 17px; font-weight: 700; }
  .daksina__head p { margin: 3px 0 0; font-size: 11.5px; color: #80868b; }
  .daksina__state { display: flex; align-items: center; gap: 8px; font-size: 12px; color: #5f6368; white-space: nowrap; }
  .dot { width: 8px; height: 8px; border-radius: 50%; background: #dadce0; }
  .dot.on { background: #f9ab00; }
  .dot.healthy { background: #34a853; }

  .banner { margin: 0 0 10px; padding: 8px 12px; border-radius: 6px; font-size: 12.5px; }
  .banner--error { background: #fce8e6; color: #c5221f; }
  .banner--ok { background: #e6f4ea; color: #137333; }

  .daksina__grid { display: grid; grid-template-columns: 1.15fr .85fr; gap: 12px; align-items: start; }
  @media (max-width: 900px) { .daksina__grid { grid-template-columns: 1fr; } }

  .card { background: #fff; border: 1px solid #e3e5e8; border-radius: 10px; padding: 14px; margin-bottom: 12px; }
  .card h4 { margin: 0 0 10px; font-size: 13px; font-weight: 700; }
  .card__head { display: flex; justify-content: space-between; align-items: center; gap: 10px; margin-bottom: 8px; }
  .card__head h4 { margin: 0; }
  .card__actions { display: flex; gap: 8px; align-items: center; }

  textarea, input[type='text'], input[type='password'], select {
    width: 100%; box-sizing: border-box; padding: 8px 10px; border: 1px solid #dadce0;
    border-radius: 6px; font-size: 12.5px; font-family: inherit; color: #202124; background: #fff;
  }
  textarea { resize: vertical; margin-bottom: 8px; }
  textarea:focus, input:focus, select:focus { outline: none; border-color: #202124; }
  .field { display: flex; flex-direction: column; gap: 4px; margin-bottom: 8px; }
  .field span { font-size: 11px; color: #5f6368; font-weight: 600; }
  .field em { font-style: normal; color: #188038; font-weight: 500; }
  .check { display: flex; align-items: center; gap: 7px; font-size: 12px; color: #5f6368; margin: 6px 0 10px; }
  .hint { font-size: 11.5px; color: #80868b; line-height: 1.5; }
  .muted { color: #80868b; font-size: 11.5px; }
  .pad { padding: 8px 2px; }
  code { font-family: ui-monospace, monospace; font-size: 11px; background: #f1f3f4; padding: 1px 5px; border-radius: 4px; }

  .btn-primary, .btn-ghost, .btn-danger {
    display: inline-flex; align-items: center; justify-content: center; gap: 6px;
    padding: 7px 12px; border-radius: 6px; font-size: 12px; font-weight: 600; cursor: pointer;
    text-decoration: none; border: 1px solid transparent;
  }
  .btn-primary { background: #202124; color: #fff; width: 100%; }
  .btn-primary:disabled { opacity: .45; cursor: not-allowed; }
  .btn-ghost { background: #fff; border-color: #dadce0; color: #5f6368; }
  .btn-ghost:hover { background: #f1f3f4; }
  .btn-danger { background: #fce8e6; color: #c5221f; }
  .token-clear { margin-top: 8px; width: 100%; }
  .file input { display: none; }

  .runs { display: flex; flex-direction: column; }
  .run {
    display: grid; grid-template-columns: 130px 1fr auto auto auto; align-items: center; gap: 8px;
    width: 100%; padding: 8px 6px; border: none; border-top: 1px solid #f1f3f4;
    background: transparent; text-align: left; cursor: pointer; color: inherit;
  }
  .run:hover { background: #f8f9fa; }
  .run.active { background: #e8f0fe; }
  .run__id { font-family: ui-monospace, monospace; font-size: 10.5px; color: #80868b; }
  .run__instruction { font-size: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .run__date { font-size: 10.5px; color: #9aa0a6; white-space: nowrap; }
  .run__pr { font-size: 11px; color: #1a73e8; text-decoration: none; }
  .pill { font-size: 10px; font-weight: 700; text-transform: uppercase; letter-spacing: .03em; padding: 2px 7px; border-radius: 99px; background: #f1f3f4; color: #5f6368; white-space: nowrap; }
  .pill--running, .pill--queued { background: #fef7e0; color: #b06000; }
  .pill--done { background: #e6f4ea; color: #137333; }
  .pill--failed { background: #fce8e6; color: #c5221f; }
  .pill--pr_open { background: #e8f0fe; color: #1a73e8; }
  .pill--cancelled, .pill--interrupted { background: #f1f3f4; color: #80868b; }

  .run-meta { display: flex; flex-wrap: wrap; gap: 14px; font-size: 11.5px; color: #5f6368; margin-bottom: 8px; }
  .summary { font-size: 12px; color: #3c4043; line-height: 1.5; }
  .log {
    margin: 8px 0 0; max-height: 420px; overflow: auto; background: #202124; color: #e8eaed;
    padding: 12px; border-radius: 8px; font-family: ui-monospace, monospace; font-size: 11px;
    line-height: 1.55; white-space: pre-wrap; word-break: break-word;
  }

  .sessions { display: flex; flex-wrap: wrap; gap: 8px; margin-top: 8px; }
  .session {
    display: flex; flex-direction: column; gap: 2px; max-width: 300px; text-align: left;
    padding: 7px 10px; border: 1px solid #e3e5e8; border-radius: 7px; background: #fff; cursor: pointer;
  }
  .session:hover { background: #f8f9fa; }
  .session__title { font-size: 12px; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .session__meta { font-family: ui-monospace, monospace; font-size: 9.5px; color: #9aa0a6; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .preview { margin-top: 12px; border-top: 1px solid #f1f3f4; padding-top: 10px; }
  .preview__body { max-height: 320px; overflow-y: auto; display: flex; flex-direction: column; gap: 8px; }
  .msg { background: #f8f9fa; border-radius: 6px; padding: 7px 9px; }
  .msg__role { font-size: 10px; font-weight: 700; text-transform: uppercase; color: #80868b; }
  .msg p { margin: 3px 0 0; font-size: 12px; line-height: 1.45; white-space: pre-wrap; }
</style>
