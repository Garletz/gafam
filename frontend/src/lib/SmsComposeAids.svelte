<script lang="ts">
  import { onMount } from 'svelte';
  import { decryptAESGCM } from '$lib/crypto';
  import { createComposeMap, type ComposeMapHandle } from '$lib/geoMapLibre';

  export type ComposePlace = {
    id: number;
    label: string;
    address?: string;
    lat?: number | null;
    lon?: number | null;
    use_count?: number;
  };

  type GeoHit = {
    name: string;
    display: string;
    admin1?: string;
    lat: number;
    lon: number;
    source: string;
  };

  let {
    vpcUrl,
    sessionToken,
    nodePhone = '',
    body = $bindable(''),
    textareaEl = $bindable<HTMLTextAreaElement | null>(null)
  }: {
    vpcUrl: string;
    sessionToken: string;
    /** Fallback from URL / directory when VPC self_phone is empty */
    nodePhone?: string;
    body?: string;
    textareaEl?: HTMLTextAreaElement | null;
  } = $props();

  let places = $state<ComposePlace[]>([]);
  let geoHits = $state<GeoHit[]>([]);
  let placeQ = $state('');
  let showManage = $state(false);
  let customHH = $state('');
  let customMM = $state('');
  let msg = $state('');
  /** Phone from current VPC settings (self_phone), preferred over URL */
  let vpcPhone = $state('');
  /** 0 = today, 1 = tomorrow, … */
  let dayOffset = $state(0);
  /** Tick so smart slots refresh with the clock */
  let nowMs = $state(Date.now());
  let geoStatus = $state<{
    imported?: boolean;
    count?: number;
    importing?: boolean;
    countries?: number;
    source?: string;
    bundle?: boolean;
  } | null>(null);

  // New place form
  let newLabel = $state('');
  let newAddress = $state('');
  let newCoords = $state(''); // "48.85,2.35"
  let mapLat = $state(48.8566);
  let mapLon = $state(2.3522);

  let mapEl = $state<HTMLDivElement | null>(null);
  let mapHandle: ComposeMapHandle | null = null;

  const DAYS_FR = ['dimanche', 'lundi', 'mardi', 'mercredi', 'jeudi', 'vendredi', 'samedi'];

  type DayOpt = { offset: number; label: string };
  type TimeSlot = { key: string; label: string; hh: number; mm: number };

  const dayOptions = $derived.by((): DayOpt[] => {
    const base = new Date(nowMs);
    const opts: DayOpt[] = [
      { offset: 0, label: "Aujourd'hui" },
      { offset: 1, label: 'Demain' }
    ];
    for (let o = 2; o <= 6; o++) {
      const d = new Date(base);
      d.setDate(d.getDate() + o);
      opts.push({ offset: o, label: DAYS_FR[d.getDay()] });
    }
    return opts;
  });

  /** Next half-hour clock slots from PC time for the selected day. */
  const smartSlots = $derived.by((): TimeSlot[] => {
    const now = new Date(nowMs);
    const slots: TimeSlot[] = [];
    const push = (hh: number, mm: number) => {
      if (hh > 23) return;
      const label = mm === 0 ? `${hh}h` : `${hh}h${String(mm).padStart(2, '0')}`;
      slots.push({ key: `${hh}:${mm}`, label, hh, mm });
    };

    if (dayOffset === 0) {
      // Round up to next :00 or :30
      let h = now.getHours();
      let m = now.getMinutes();
      if (m === 0) {
        /* exactly on the hour → start at +30min */
        m = 30;
      } else if (m < 30) {
        m = 30;
      } else {
        h += 1;
        m = 0;
      }
      // Offer ~8 upcoming slots (ex. 18:20 → 18:30, 19h, 19:30, 20h, 20:30…)
      for (let i = 0; i < 8 && h <= 23; i++) {
        push(h, m);
        if (m === 0) m = 30;
        else {
          h += 1;
          m = 0;
        }
      }
    } else {
      // Other days: classic RDV window, denser around evening
      const candidates = [
        [9, 0],
        [10, 0],
        [11, 0],
        [12, 0],
        [12, 30],
        [14, 0],
        [15, 0],
        [16, 0],
        [17, 0],
        [18, 0],
        [18, 30],
        [19, 0],
        [19, 30],
        [20, 0],
        [20, 30],
        [21, 0]
      ];
      for (const [hh, mm] of candidates) push(hh, mm);
    }
    return slots;
  });

  function dayPrefix(): string {
    if (dayOffset === 0) return '';
    if (dayOffset === 1) return 'demain ';
    const d = new Date(nowMs);
    d.setDate(d.getDate() + dayOffset);
    return `${DAYS_FR[d.getDay()]} `;
  }

  function insertSmartSlot(slot: TimeSlot) {
    const time =
      slot.mm === 0 ? `${slot.hh}h` : `${slot.hh}h${String(slot.mm).padStart(2, '0')}`;
    const prefix = dayPrefix();
    insertText(prefix ? `${prefix}à ${time}` : `à ${time}`);
  }

  function insertCustomClock() {
    const h = parseInt(customHH, 10);
    const m = parseInt(customMM || '0', 10);
    if (Number.isNaN(h) || h < 0 || h > 23 || Number.isNaN(m) || m < 0 || m > 59) {
      msg = 'Heure invalide (HH / MM)';
      setTimeout(() => (msg = ''), 2500);
      return;
    }
    const time = m === 0 ? `${h}h` : `${h}h${String(m).padStart(2, '0')}`;
    const prefix = dayPrefix();
    insertText(prefix ? `${prefix}à ${time}` : `à ${time}`);
  }

  function toSubdomainPhone(raw: string): string {
    let d = String(raw).trim().replace(/[\s.\-()]/g, '');
    if (d.startsWith('+33')) d = '0' + d.slice(3);
    else if (d.startsWith('33') && d.length >= 11) d = '0' + d.slice(2);
    return d;
  }

  const nodeHost = $derived.by(() => {
    const raw = vpcPhone || nodePhone || '';
    const sub = toSubdomainPhone(raw);
    return sub ? `${sub}.gafam.cloud` : '';
  });

  const signatureLine = $derived(
    nodeHost ? `Envoyé depuis ${nodeHost}` : 'Envoyé depuis gafam.cloud'
  );

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

  async function loadGeoSearch(q: string) {
    if (!q.trim() || q.trim().length < 2) {
      geoHits = [];
      return;
    }
    try {
      const res = await fetch(
        `/api/proxy/geo?${qs({ action: 'search', q: q.trim(), limit: '12' })}`
      );
      if (!res.ok) {
        geoHits = [];
        return;
      }
      const data: any = await res.json();
      geoHits = Array.isArray(data.results) ? data.results : [];
    } catch {
      geoHits = [];
    }
  }

  async function loadGeoStatus() {
    try {
      const res = await fetch(`/api/proxy/geo?${qs({ action: 'status' })}`);
      if (res.ok) geoStatus = await res.json();
    } catch {
      /* ignore */
    }
  }

  async function triggerGeoImport() {
    msg = 'Rechargement bundle GeoNames…';
    try {
      await fetch(`/api/proxy/geo?${qs({ action: 'import' })}`, { method: 'POST' });
      msg = 'Seed offline lancé sur le VPC';
      setTimeout(() => loadGeoStatus(), 3000);
    } catch {
      msg = 'Seed échoué';
    }
    setTimeout(() => (msg = ''), 4000);
  }

  function applyGeoHit(h: GeoHit) {
    newLabel = h.name;
    newAddress = h.display;
    newCoords = `${h.lat.toFixed(5)},${h.lon.toFixed(5)}`;
    mapLat = h.lat;
    mapLon = h.lon;
    showManage = true;
    syncMapMarker(h.lat, h.lon);
    msg = 'Coords remplies — Enregistrer pour le carnet';
    setTimeout(() => (msg = ''), 3000);
  }

  function insertGeoChip(h: GeoHit) {
    const s =
      h.admin1 && h.admin1 !== h.name
        ? `au ${h.name} — ${h.admin1} (${h.lat.toFixed(5)},${h.lon.toFixed(5)})`
        : `au ${h.name} (${h.lat.toFixed(5)},${h.lon.toFixed(5)})`;
    insertText(s);
  }

  function geoProxyUrl(action: string, extra: Record<string, string> = {}) {
    const params = new URLSearchParams({ vpcUrl, token: sessionToken, action, ...extra });
    return `/api/proxy/geo?${params}`;
  }

  async function ensureMap() {
    if (typeof window === 'undefined' || !mapEl || !vpcUrl || !sessionToken) return;
    if (mapHandle) {
      mapHandle.resize();
      mapHandle.setView(mapLat, mapLon);
      return;
    }
    mapHandle = await createComposeMap({
      container: mapEl,
      vpcUrl,
      token: sessionToken,
      lat: mapLat || 46.5,
      lon: mapLon || 2.5,
      onPick: (lat, lon) => {
        mapLat = lat;
        mapLon = lon;
        newCoords = `${lat.toFixed(5)},${lon.toFixed(5)}`;
      }
    });
    setTimeout(() => mapHandle?.resize(), 80);
  }

  function syncMapMarker(lat: number, lon: number) {
    mapLat = lat;
    mapLon = lon;
    mapHandle?.setView(lat, lon);
  }

  function destroyMap() {
    mapHandle?.destroy();
    mapHandle = null;
  }

  onMount(() => {
    loadPlaces();
    loadVpcPhone();
    loadGeoStatus();
    const tick = setInterval(() => {
      nowMs = Date.now();
    }, 30_000);
    return () => {
      clearInterval(tick);
      destroyMap();
    };
  });

  async function loadVpcPhone() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/settings?${params}`);
      if (!res.ok) return;
      const payload: any = await res.json();
      if (payload.encrypted_data && payload.iv) {
        const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
        const obj = JSON.parse(plaintext);
        if (obj.self_phone) vpcPhone = String(obj.self_phone);
      }
    } catch {
      /* keep URL fallback */
    }
  }

  $effect(() => {
    const q = placeQ;
    const t = setTimeout(() => {
      loadPlaces(q);
      loadGeoSearch(q);
    }, 220);
    return () => clearTimeout(t);
  });

  // Address field also drives GeoNames search (same pipeline)
  $effect(() => {
    if (!showManage) return;
    const q = newAddress.trim() || newLabel.trim();
    if (q.length < 2) return;
    const t = setTimeout(() => loadGeoSearch(q), 220);
    return () => clearTimeout(t);
  });

  $effect(() => {
    if (showManage) {
      queueMicrotask(() => ensureMap());
      const t = setTimeout(() => mapHandle?.resize(), 120);
      return () => clearTimeout(t);
    }
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
    let { lat, lon } = parseCoords(newCoords);
    if (lat == null || lon == null) {
      lat = mapLat;
      lon = mapLon;
    }
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

  /** Apple-style footer: append at end (not cursor), skip if already present. */
  function insertSignature() {
    const line = signatureLine;
    if (body.includes(line)) {
      msg = 'Signature déjà présente';
      setTimeout(() => (msg = ''), 2000);
      return;
    }
    const trimmed = body.replace(/\s+$/, '');
    body = trimmed ? `${trimmed}\n\n${line}` : line;
    requestAnimationFrame(() => {
      const el = textareaEl;
      if (el) {
        el.focus();
        const pos = body.length;
        el.setSelectionRange(pos, pos);
      }
    });
  }
</script>

<div class="sca">
  <div class="sca__row">
    <span class="sca__label">Jour</span>
    <div class="sca__chips">
      {#each dayOptions as d (d.offset)}
        <button
          type="button"
          class="sca__chip"
          class:sca__chip--on={dayOffset === d.offset}
          onclick={() => (dayOffset = d.offset)}
        >
          {d.label}
        </button>
      {/each}
    </div>
  </div>

  <div class="sca__row">
    <span class="sca__label">Heure</span>
    <div class="sca__chips">
      {#each smartSlots as s (s.key)}
        <button type="button" class="sca__chip sca__chip--time" onclick={() => insertSmartSlot(s)}>
          {s.label}
        </button>
      {:else}
        <span class="sca__empty">plus de créneau ce soir</span>
      {/each}
    </div>
    <div class="sca__mini">
      <input class="sca__num" type="text" inputmode="numeric" maxlength="2" placeholder="HH" bind:value={customHH} />
      <span>:</span>
      <input class="sca__num" type="text" inputmode="numeric" maxlength="2" placeholder="MM" bind:value={customMM} />
      <button type="button" class="sca__mini-btn" onclick={insertCustomClock} title="Heure libre + jour sélectionné">+</button>
    </div>
  </div>

  <div class="sca__row">
    <span class="sca__label">Lieu</span>
    <input
      class="sca__search"
      type="search"
      placeholder="Chercher ville / lieu…"
      bind:value={placeQ}
    />
    <div class="sca__chips">
      {#each places.slice(0, 8) as p (p.id)}
        <button type="button" class="sca__chip sca__chip--place" onclick={() => onPlaceChip(p)}>
          {p.label}
        </button>
      {/each}
      {#each geoHits.slice(0, 6) as h (`${h.lat},${h.lon},${h.name}`)}
        <button
          type="button"
          class="sca__chip sca__chip--geo"
          title={h.display}
          onclick={() => insertGeoChip(h)}
          oncontextmenu={(e) => {
            e.preventDefault();
            applyGeoHit(h);
          }}
        >
          {h.name}
        </button>
      {/each}
      {#if places.length === 0 && geoHits.length === 0}
        <span class="sca__empty">carnet ou GeoNames…</span>
      {/if}
    </div>
    <button
      type="button"
      class="sca__sig"
      onclick={insertSignature}
      title={signatureLine}
    >
      Signature
    </button>
    <button type="button" class="sca__manage" onclick={() => (showManage = !showManage)}>
      {showManage ? 'Fermer' : 'Gérer'}
    </button>
  </div>

  {#if showManage}
    <div class="sca__panel">
      <p class="sca__panel-title">
        Carnet + GeoNames détail (FR·BE·CH…)
        {#if geoStatus}
          <span class="sca__geo-meta">
            {#if geoStatus.importing}
              seed…
            {:else if geoStatus.imported}
              {geoStatus.count?.toLocaleString('fr-FR')} lieux
              {#if geoStatus.countries}
                · {geoStatus.countries} pays
              {/if}
              {#if geoStatus.source}
                · offline
              {/if}
              {#if !geoStatus.bundle}
                · ⚠ bundle manquant
              {/if}
            {:else}
              base vide
              <button type="button" class="sca__link" onclick={triggerGeoImport}>Seed offline</button>
            {/if}
          </span>
        {/if}
      </p>
      {#if geoHits.length}
        <ul class="sca__geo-list">
          {#each geoHits as h (`g-${h.lat}-${h.lon}-${h.name}`)}
            <li>
              <button type="button" class="sca__geo-pick" onclick={() => applyGeoHit(h)}>
                <strong>{h.name}</strong>
                <span class="muted">{h.display}</span>
                <span class="mono">{h.lat.toFixed(4)}, {h.lon.toFixed(4)}</span>
              </button>
            </li>
          {/each}
        </ul>
      {/if}
      <div class="sca__map-wrap">
        <div class="sca__map" bind:this={mapEl}></div>
        <span class="sca__map-attr">MapLibre · Protomaps PMTiles VPC · 0 CDN</span>
      </div>
      <p class="sca__hint">Tape une ville (haut ou adresse) → choisir un résultat. Clic / drag sur la carte pour affiner.</p>
      <div class="sca__form">
        <input type="text" placeholder="Nom (ex. Café de la Gare)" bind:value={newLabel} />
        <input
          type="text"
          placeholder="Adresse / ville (recherche GeoNames)"
          bind:value={newAddress}
          autocomplete="off"
        />
        <input type="text" placeholder="lat,lon" bind:value={newCoords} />
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
  .sca__chip--on {
    background: #202124;
    border-color: #202124;
    color: #fff;
  }
  .sca__chip--on:hover {
    background: #3c4043;
    border-color: #3c4043;
    color: #fff;
  }
  .sca__chip--time {
    font-variant-numeric: tabular-nums;
    font-weight: 600;
  }
  .sca__chip--place {
    background: #e8f0fe;
    border-color: #aecbfa;
    color: #174ea6;
  }
  .sca__chip--geo {
    background: #e6f4ea;
    border-color: #a8dab5;
    color: #137333;
  }
  .sca__map-wrap {
    position: relative;
    width: 100%;
    margin: 8px 0;
    border: 1px solid #dadce0;
    border-radius: 8px;
    overflow: hidden;
    background: #a8c4d8;
  }
  .sca__map {
    width: 100%;
    height: 360px;
    z-index: 0;
  }
  .sca__map-attr {
    position: absolute;
    right: 6px;
    bottom: 4px;
    z-index: 500;
    font-size: 10px;
    color: #3c4043;
    background: rgba(255, 255, 255, 0.85);
    padding: 2px 6px;
    border-radius: 4px;
    pointer-events: none;
  }
  :global(.sca-city-label) {
    background: rgba(20, 24, 28, 0.72);
    border: none;
    color: #f5f0e6;
    font-size: 10px;
    font-weight: 600;
    padding: 1px 5px;
    border-radius: 3px;
    box-shadow: none;
  }
  :global(.sca-city-label::before) {
    display: none;
  }
  .sca__geo-list {
    list-style: none;
    margin: 0 0 8px;
    padding: 0;
    max-height: 140px;
    overflow-y: auto;
    border: 1px solid #e8eaed;
    border-radius: 8px;
  }
  .sca__geo-list li {
    margin: 0;
    border-bottom: 1px solid #eee;
  }
  .sca__geo-pick {
    width: 100%;
    text-align: left;
    border: none;
    background: transparent;
    padding: 6px 4px;
    cursor: pointer;
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-size: 12px;
  }
  .sca__geo-pick:hover {
    background: #f1f3f4;
  }
  .sca__geo-meta {
    font-weight: 400;
    font-size: 11px;
    color: #80868b;
    margin-left: 8px;
  }
  .sca__link {
    border: none;
    background: none;
    color: #174ea6;
    text-decoration: underline;
    cursor: pointer;
    font-size: 11px;
    padding: 0;
  }
  :global(.sca-ml-pin) {
    width: 18px;
    height: 18px;
  }
  :global(.sca-ml-pin__dot) {
    display: block;
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: #202124;
    border: 2px solid #fff;
    box-shadow: 0 1px 4px rgba(0, 0, 0, 0.35);
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
  .sca__sig {
    border: 1px solid #dadce0;
    background: #fff;
    font-size: 12px;
    padding: 4px 10px;
    border-radius: 6px;
    cursor: pointer;
    color: #8e8e93;
    font-style: italic;
  }
  .sca__sig:hover {
    color: #202124;
    border-color: #bdc1c6;
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
