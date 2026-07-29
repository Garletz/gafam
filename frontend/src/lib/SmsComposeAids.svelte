<script lang="ts">
  import { onMount } from 'svelte';

  export type ComposePlace = {
    id: number;
    label: string;
    address?: string;
    lat?: number | null;
    lon?: number | null;
    use_count?: number;
  };

  export type ComposeTime = {
    id: number;
    label: string;
    kind: string;
    value: string;
    sort?: number;
    use_count?: number;
  };

  let {
    vpcUrl,
    sessionToken,
    body = $bindable(''),
    textareaEl = $bindable<HTMLTextAreaElement | null>(null)
  }: {
    vpcUrl: string;
    sessionToken: string;
    body?: string;
    textareaEl?: HTMLTextAreaElement | null;
  } = $props();

  let places = $state<ComposePlace[]>([]);
  let times = $state<ComposeTime[]>([]);
  let placeQ = $state('');
  let showManage = $state(false);
  let customHH = $state('');
  let customMM = $state('');
  let inMinutes = $state('15');
  let msg = $state('');

  // New place form
  let newLabel = $state('');
  let newAddress = $state('');
  let newCoords = $state(''); // "48.85,2.35"

  function qs(extra: Record<string, string> = {}) {
    return new URLSearchParams({ vpcUrl, token: sessionToken, ...extra });
  }

  async function loadPlaces(q = '') {
    try {
      const extra: Record<string, string> = { kind: 'places' };
      if (q.trim()) extra.q = q.trim();
      const res = await fetch(`/api/proxy/compose?${qs(extra)}`);
      if (!res.ok) return;
      const data: any = await res.json();
      places = data.places || [];
    } catch {
      /* ignore */
    }
  }

  async function loadTimes() {
    try {
      const res = await fetch(`/api/proxy/compose?${qs({ kind: 'times' })}`);
      if (!res.ok) return;
      const data: any = await res.json();
      times = data.times || [];
    } catch {
      /* ignore */
    }
  }

  onMount(() => {
    loadPlaces();
    loadTimes();
  });

  $effect(() => {
    const q = placeQ;
    const t = setTimeout(() => loadPlaces(q), 200);
    return () => clearTimeout(t);
  });

  function insertText(snippet: string) {
    const piece = snippet.trim();
    if (!piece) return;
    const el = textareaEl;
    if (el && typeof el.selectionStart === 'number') {
      const start = el.selectionStart;
      const end = el.selectionEnd;
      const before = body.slice(0, start);
      const after = body.slice(end);
      const needsSpace = before.length > 0 && !/\s$/.test(before);
      const insert = (needsSpace ? ' ' : '') + piece;
      body = before + insert + after;
      requestAnimationFrame(() => {
        const pos = start + insert.length;
        el.focus();
        el.setSelectionRange(pos, pos);
      });
    } else {
      const needsSpace = body.length > 0 && !/\s$/.test(body);
      body = body + (needsSpace ? ' ' : '') + piece;
    }
  }

  function formatPlace(p: ComposePlace): string {
    const parts: string[] = [];
    if (p.label) parts.push(p.label);
    if (p.address && p.address !== p.label) parts.push(p.address);
    let s = parts.join(' — ');
    if (p.lat != null && p.lon != null && !Number.isNaN(p.lat) && !Number.isNaN(p.lon)) {
      s += ` (${p.lat.toFixed(5)},${p.lon.toFixed(5)})`;
    }
    return `au ${s}`;
  }

  function formatTimePreset(t: ComposeTime): string {
    const v = t.value || '';
    if (v.startsWith('+') && v.endsWith('m')) {
      const n = parseInt(v.slice(1), 10);
      if (!Number.isNaN(n)) {
        if (n < 60) return `dans ${n} min`;
        if (n % 60 === 0) return `dans ${n / 60} h`;
        return `dans ${Math.floor(n / 60)} h ${n % 60}`;
      }
    }
    if (v.startsWith('tomorrow_')) {
      const hhmm = v.slice('tomorrow_'.length);
      return `demain à ${hhmm.replace(':', 'h')}`;
    }
    if (v.startsWith('today_')) {
      const hhmm = v.slice('today_'.length);
      return `aujourd'hui à ${hhmm.replace(':', 'h')}`;
    }
    if (/^\d{1,2}:\d{2}$/.test(v)) {
      return `à ${v.replace(':', 'h')}`;
    }
    return t.label || v;
  }

  async function bumpUse(kind: 'places' | 'times', id: number) {
    try {
      await fetch(
        `/api/proxy/compose?${qs({ kind, action: 'use', id: String(id) })}`,
        { method: 'POST' }
      );
    } catch {
      /* ignore */
    }
  }

  async function onPlaceChip(p: ComposePlace) {
    insertText(formatPlace(p));
    await bumpUse('places', p.id);
    loadPlaces(placeQ);
  }

  async function onTimeChip(t: ComposeTime) {
    insertText(formatTimePreset(t));
    await bumpUse('times', t.id);
    loadTimes();
  }

  function insertCustomClock() {
    const h = parseInt(customHH, 10);
    const m = parseInt(customMM || '0', 10);
    if (Number.isNaN(h) || h < 0 || h > 23 || Number.isNaN(m) || m < 0 || m > 59) {
      msg = 'Heure invalide (HH / MM)';
      setTimeout(() => (msg = ''), 2500);
      return;
    }
    const mm = String(m).padStart(2, '0');
    insertText(`à ${h}h${mm}`);
  }

  function insertInMinutes() {
    const n = parseInt(inMinutes, 10);
    if (Number.isNaN(n) || n <= 0) return;
    insertText(n < 60 ? `dans ${n} min` : `dans ${Math.floor(n / 60)} h${n % 60 ? ` ${n % 60}` : ''}`);
  }

  function parseCoords(raw: string): { lat?: number; lon?: number } {
    const m = raw.trim().match(/(-?\d+(?:\.\d+)?)\s*[,;\s]\s*(-?\d+(?:\.\d+)?)/);
    if (!m) return {};
    const lat = parseFloat(m[1]);
    const lon = parseFloat(m[2]);
    if (Number.isNaN(lat) || Number.isNaN(lon)) return {};
    return { lat, lon };
  }

  async function savePlace() {
    const label = newLabel.trim();
    if (!label) {
      msg = 'Nom du lieu requis';
      setTimeout(() => (msg = ''), 2500);
      return;
    }
    const { lat, lon } = parseCoords(newCoords);
    const res = await fetch(`/api/proxy/compose?${qs({ kind: 'places' })}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        label,
        address: newAddress.trim(),
        lat: lat ?? null,
        lon: lon ?? null
      })
    });
    if (res.ok) {
      newLabel = '';
      newAddress = '';
      newCoords = '';
      msg = 'Lieu enregistré';
      loadPlaces(placeQ);
    } else {
      msg = 'Échec enregistrement';
    }
    setTimeout(() => (msg = ''), 2500);
  }

  async function deletePlace(id: number) {
    const res = await fetch(`/api/proxy/compose?${qs({ kind: 'places', id: String(id) })}`, {
      method: 'DELETE'
    });
    if (res.ok) loadPlaces(placeQ);
  }
</script>

<div class="sca">
  <div class="sca__row">
    <span class="sca__label">Heure</span>
    <div class="sca__chips">
      {#each times as t (t.id)}
        <button type="button" class="sca__chip" onclick={() => onTimeChip(t)} title={t.value}>
          {t.label}
        </button>
      {/each}
    </div>
    <div class="sca__mini">
      <input class="sca__num" type="text" inputmode="numeric" maxlength="2" placeholder="HH" bind:value={customHH} />
      <span>:</span>
      <input class="sca__num" type="text" inputmode="numeric" maxlength="2" placeholder="MM" bind:value={customMM} />
      <button type="button" class="sca__mini-btn" onclick={insertCustomClock}>+</button>
      <span class="sca__sep">|</span>
      <input class="sca__num sca__num--wide" type="text" inputmode="numeric" bind:value={inMinutes} />
      <span class="sca__hint">min</span>
      <button type="button" class="sca__mini-btn" onclick={insertInMinutes}>+</button>
    </div>
  </div>

  <div class="sca__row">
    <span class="sca__label">Lieu</span>
    <input
      class="sca__search"
      type="search"
      placeholder="Chercher…"
      bind:value={placeQ}
    />
    <div class="sca__chips">
      {#each places.slice(0, 12) as p (p.id)}
        <button type="button" class="sca__chip sca__chip--place" onclick={() => onPlaceChip(p)}>
          {p.label}
        </button>
      {:else}
        <span class="sca__empty">aucun lieu — ouvrir Gérer</span>
      {/each}
    </div>
    <button type="button" class="sca__manage" onclick={() => (showManage = !showManage)}>
      {showManage ? 'Fermer' : 'Gérer'}
    </button>
  </div>

  {#if showManage}
    <div class="sca__panel">
      <p class="sca__panel-title">Carnet de lieux (SQL VPC — pas d’API carto)</p>
      <div class="sca__form">
        <input type="text" placeholder="Nom (ex. Café de la Gare)" bind:value={newLabel} />
        <input type="text" placeholder="Adresse libre (optionnel)" bind:value={newAddress} />
        <input type="text" placeholder="lat,lon (ex. 48.8566,2.3522)" bind:value={newCoords} />
        <button type="button" class="sca__save" onclick={savePlace}>Enregistrer</button>
      </div>
      <ul class="sca__list">
        {#each places as p (p.id)}
          <li>
            <div>
              <strong>{p.label}</strong>
              {#if p.address}<span class="muted"> — {p.address}</span>{/if}
              {#if p.lat != null && p.lon != null}
                <span class="mono"> ({p.lat.toFixed(4)},{p.lon.toFixed(4)})</span>
              {/if}
            </div>
            <button type="button" class="sca__del" onclick={() => deletePlace(p.id)}>Suppr.</button>
          </li>
        {/each}
      </ul>
    </div>
  {/if}

  {#if msg}<p class="sca__msg">{msg}</p>{/if}
</div>

<style>
  .sca {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 8px;
  }
  .sca__row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px 8px;
  }
  .sca__label {
    font-size: 11px;
    font-weight: 700;
    color: #5f6368;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    min-width: 40px;
  }
  .sca__chips {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    flex: 1;
    min-width: 120px;
  }
  .sca__chip {
    border: 1px solid #dadce0;
    background: #fff;
    color: #202124;
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 14px;
    cursor: pointer;
  }
  .sca__chip:hover {
    background: #f1f3f4;
    border-color: #bdc1c6;
  }
  .sca__chip--place {
    background: #e8f0fe;
    border-color: #aecbfa;
    color: #174ea6;
  }
  .sca__empty {
    font-size: 12px;
    color: #9aa0a6;
  }
  .sca__mini {
    display: inline-flex;
    align-items: center;
    gap: 4px;
  }
  .sca__num {
    width: 36px;
    padding: 4px 6px;
    border: 1px solid #dadce0;
    border-radius: 6px;
    font-size: 12px;
    text-align: center;
  }
  .sca__num--wide {
    width: 44px;
  }
  .sca__hint {
    font-size: 11px;
    color: #5f6368;
  }
  .sca__sep {
    color: #dadce0;
    margin: 0 2px;
  }
  .sca__mini-btn {
    border: none;
    background: #202124;
    color: #fff;
    width: 24px;
    height: 24px;
    border-radius: 6px;
    cursor: pointer;
    font-size: 14px;
    line-height: 1;
  }
  .sca__search {
    width: 110px;
    padding: 4px 8px;
    border: 1px solid #dadce0;
    border-radius: 6px;
    font-size: 12px;
  }
  .sca__manage {
    border: 1px solid #dadce0;
    background: #fff;
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 6px;
    cursor: pointer;
    color: #5f6368;
  }
  .sca__panel {
    border: 1px solid #e8eaed;
    border-radius: 8px;
    padding: 10px 12px;
    background: #f8f9fa;
  }
  .sca__panel-title {
    margin: 0 0 8px;
    font-size: 12px;
    color: #5f6368;
  }
  .sca__form {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 6px;
    margin-bottom: 10px;
  }
  .sca__form input {
    padding: 8px 10px;
    border: 1px solid #dadce0;
    border-radius: 6px;
    font-size: 13px;
  }
  .sca__save {
    grid-column: 1 / -1;
    padding: 8px;
    border: none;
    border-radius: 6px;
    background: #1a73e8;
    color: #fff;
    font-weight: 600;
    cursor: pointer;
  }
  .sca__list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 160px;
    overflow: auto;
  }
  .sca__list li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    padding: 6px 0;
    border-top: 1px solid #e8eaed;
    font-size: 13px;
  }
  .sca__del {
    border: none;
    background: transparent;
    color: #c5221f;
    cursor: pointer;
    font-size: 12px;
  }
  .muted {
    color: #5f6368;
  }
  .mono {
    font-family: ui-monospace, monospace;
    font-size: 11px;
    color: #5f6368;
  }
  .sca__msg {
    margin: 0;
    font-size: 12px;
    color: #188038;
  }
  @media (max-width: 640px) {
    .sca__form {
      grid-template-columns: 1fr;
    }
  }
</style>
