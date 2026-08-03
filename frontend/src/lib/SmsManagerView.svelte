<script lang="ts">
  /**
   * SmsManagerView — dedicated SMS/MMS management section.
   * Stats, full-text filters, bulk delete, CSV/JSON export.
   * Data (smsList / mmsList / contacts) is provided by the parent dashboard.
   */
  interface Props {
    sessionToken: string;
    vpcUrl: string;
    smsList: any[];
    mmsList: any[];
    contacts: Record<string, string>;
    onChanged?: () => void;
  }
  let { sessionToken, vpcUrl, smsList, mmsList, contacts, onChanged }: Props = $props();

  type Row = {
    id: number;
    sender: string;
    body: string;
    timestamp: number;
    direction: string;
    status: string;
    is_mms: boolean;
    media_count: number;
  };

  let search = $state('');
  let dirFilter: 'all' | 'inbound' | 'outbound' = $state('all');
  let statusFilter: 'all' | 'inbox' | 'purgatory' | 'outbound' | 'sent' | 'failed' = $state('all');
  let peerFilter: string = $state('all');
  let typeFilter: 'all' | 'sms' | 'mms' = $state('all');
  let selected = $state<Record<number, true>>({});
  let deleting = $state(false);
  let statusMsg = $state('');
  let visibleCount = $state(200);

  function getContactName(sender: string): string {
    if (contacts[sender]) return contacts[sender];
    if (!sender) return 'Unknown';
    const normSender = sender.replace(/\D/g, '');
    if (normSender.length < 6) return sender;
    const suffixLen = Math.min(normSender.length, 9);
    const senderSuffix = normSender.slice(-suffixLen);
    for (const [phone, name] of Object.entries(contacts)) {
      const normContact = phone.replace(/\D/g, '');
      if (normContact.endsWith(senderSuffix) && normContact.length >= suffixLen) return name;
    }
    return sender;
  }

  let allRows = $derived.by((): Row[] => {
    const rows: Row[] = [];
    for (const s of smsList) {
      rows.push({
        id: s.id,
        sender: s.sender,
        body: s.body || '',
        timestamp: s.timestamp || 0,
        direction: s.direction || (s.status === 'outbound' || s.status === 'sent' ? 'outbound' : 'inbound'),
        status: s.status || 'inbox',
        is_mms: false,
        media_count: 0,
      });
    }
    for (const m of mmsList) {
      const textParts = (m.parts || []).filter((p: any) => p.text).map((p: any) => p.text);
      const mediaCount = (m.parts || []).filter((p: any) => p.has_media).length;
      rows.push({
        id: m.id,
        sender: m.sender,
        body: textParts.join('\n') || (mediaCount > 0 ? '📷 Media' : '(MMS)'),
        timestamp: m.timestamp || 0,
        direction: m.direction || 'inbound',
        status: m.status || 'inbox',
        is_mms: true,
        media_count: mediaCount,
      });
    }
    rows.sort((a, b) => b.timestamp - a.timestamp);
    return rows;
  });

  let peers = $derived.by(() => {
    const set = new Set<string>();
    for (const r of allRows) set.add(r.sender);
    return [...set].sort((a, b) => getContactName(a).localeCompare(getContactName(b)));
  });

  let filtered = $derived.by(() => {
    const q = search.trim().toLowerCase();
    return allRows.filter((r) => {
      if (dirFilter !== 'all' && r.direction !== dirFilter) return false;
      if (statusFilter !== 'all' && r.status !== statusFilter) return false;
      if (typeFilter === 'sms' && r.is_mms) return false;
      if (typeFilter === 'mms' && !r.is_mms) return false;
      if (peerFilter !== 'all' && r.sender !== peerFilter) return false;
      if (!q) return true;
      return (
        r.body.toLowerCase().includes(q) ||
        r.sender.toLowerCase().includes(q) ||
        getContactName(r.sender).toLowerCase().includes(q)
      );
    });
  });

  let visible = $derived(filtered.slice(0, visibleCount));

  let stats = $derived.by(() => {
    const now = new Date();
    const startOfDay = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
    let today = 0, inbound = 0, outbound = 0, failed = 0;
    for (const r of allRows) {
      if (r.timestamp >= startOfDay) today++;
      if (r.direction === 'outbound') outbound++; else inbound++;
      if (r.status === 'failed') failed++;
    }
    return { total: allRows.length, today, inbound, outbound, failed };
  });

  let selectedCount = $derived(Object.keys(selected).length);
  let allVisibleSelected = $derived(
    visible.length > 0 && visible.every((r) => selected[r.id])
  );

  function toggleAll() {
    if (allVisibleSelected) {
      const next = { ...selected };
      for (const r of visible) delete next[r.id];
      selected = next;
    } else {
      const next = { ...selected };
      for (const r of visible) next[r.id] = true;
      selected = next;
    }
  }

  function statusBadge(r: Row): { label: string; cls: string } {
    if (r.is_mms && r.direction === 'outbound') return { label: 'MMS out', cls: 'badge badge--out' };
    if (r.is_mms) return { label: 'MMS', cls: 'badge badge--mms' };
    switch (r.status) {
      case 'sent': return { label: 'Sent', cls: 'badge badge--sent' };
      case 'failed': return { label: 'Failed', cls: 'badge badge--failed' };
      case 'outbound': return { label: 'Queued', cls: 'badge badge--out' };
      case 'purgatory': return { label: 'Spam', cls: 'badge badge--spam' };
      default: return { label: 'Inbox', cls: 'badge badge--in' };
    }
  }

  function fmtTime(ts: number): string {
    if (!ts) return '—';
    return new Date(ts).toLocaleString();
  }

  async function bulkDelete() {
    if (deleting || selectedCount === 0) return;
    deleting = true;
    statusMsg = '';
    try {
      const ids = Object.keys(selected).map(Number);
      // Only SMS rows are deletable via the bulk endpoint (MMS rows share no table)
      const smsIds = ids.filter((id) => allRows.some((r) => r.id === id && !r.is_mms));
      if (smsIds.length > 0) {
        const params = new URLSearchParams({ vpcUrl, token: sessionToken });
        const res = await fetch(`/api/proxy/sms?${params}`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ ids: smsIds }),
        });
        if (!res.ok) {
          const data: any = await res.json().catch(() => ({}));
          statusMsg = data.error || `Delete failed (${res.status})`;
          return;
        }
      }
      selected = {};
      statusMsg = `Deleted ${smsIds.length} message(s).`;
      onChanged?.();
    } catch (e: any) {
      statusMsg = e?.message || 'Delete failed';
    } finally {
      deleting = false;
    }
  }

  function exportRows(format: 'csv' | 'json') {
    const rows = filtered;
    let content: string;
    let mime: string;
    let ext: string;
    if (format === 'json') {
      content = JSON.stringify(rows.map((r) => ({
        ...r,
        contact: getContactName(r.sender),
        date: new Date(r.timestamp).toISOString(),
      })), null, 2);
      mime = 'application/json';
      ext = 'json';
    } else {
      const esc = (v: string) => `"${v.replace(/"/g, '""')}"`;
      const header = 'date,contact,number,direction,status,type,body';
      const lines = rows.map((r) => [
        new Date(r.timestamp).toISOString(),
        esc(getContactName(r.sender)),
        esc(r.sender),
        r.direction,
        r.status,
        r.is_mms ? 'mms' : 'sms',
        esc(r.body),
      ].join(','));
      content = [header, ...lines].join('\n');
      mime = 'text/csv';
      ext = 'csv';
    }
    const blob = new Blob([content], { type: mime });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `gafam-sms-export-${new Date().toISOString().slice(0, 10)}.${ext}`;
    a.click();
    URL.revokeObjectURL(url);
  }
</script>

<div class="sms-mgr">
  <header class="sms-mgr__head">
    <div class="sms-mgr__title-block">
      <h2>SMS Manager</h2>
      <p>All messages relayed by your phone — SMS &amp; carrier MMS.</p>
    </div>
    <div class="sms-mgr__stats">
      <div class="stat"><span class="stat__num">{stats.total}</span><span class="stat__lbl">Total</span></div>
      <div class="stat"><span class="stat__num">{stats.today}</span><span class="stat__lbl">Today</span></div>
      <div class="stat"><span class="stat__num">{stats.inbound}</span><span class="stat__lbl">In</span></div>
      <div class="stat"><span class="stat__num">{stats.outbound}</span><span class="stat__lbl">Out</span></div>
      {#if stats.failed > 0}
        <div class="stat stat--warn"><span class="stat__num">{stats.failed}</span><span class="stat__lbl">Failed</span></div>
      {/if}
    </div>
  </header>

  <div class="sms-mgr__toolbar">
    <input class="f-search" type="search" placeholder="Search text, contact, number…" bind:value={search} />
    <select bind:value={peerFilter} title="Conversation">
      <option value="all">All conversations</option>
      {#each peers as p}
        <option value={p}>{getContactName(p)}</option>
      {/each}
    </select>
    <select bind:value={dirFilter} title="Direction">
      <option value="all">In + Out</option>
      <option value="inbound">Inbound</option>
      <option value="outbound">Outbound</option>
    </select>
    <select bind:value={statusFilter} title="Status">
      <option value="all">Any status</option>
      <option value="inbox">Inbox</option>
      <option value="purgatory">Spam</option>
      <option value="outbound">Queued</option>
      <option value="sent">Sent</option>
      <option value="failed">Failed</option>
    </select>
    <select bind:value={typeFilter} title="Type">
      <option value="all">SMS + MMS</option>
      <option value="sms">SMS only</option>
      <option value="mms">MMS only</option>
    </select>
    <div class="sms-mgr__toolbar-spacer"></div>
    <button class="btn" onclick={() => exportRows('csv')}>Export CSV</button>
    <button class="btn" onclick={() => exportRows('json')}>Export JSON</button>
    <button
      class="btn btn--danger"
      disabled={selectedCount === 0 || deleting}
      onclick={bulkDelete}
    >{deleting ? 'Deleting…' : `Delete (${selectedCount})`}</button>
  </div>

  {#if statusMsg}<div class="sms-mgr__status">{statusMsg}</div>{/if}

  <div class="sms-mgr__table-wrap">
    <table class="sms-mgr__table">
      <thead>
        <tr>
          <th class="c-check"><input type="checkbox" checked={allVisibleSelected} onchange={toggleAll} /></th>
          <th class="c-date">Date</th>
          <th class="c-contact">Contact</th>
          <th class="c-dir">Dir</th>
          <th class="c-body">Message</th>
          <th class="c-status">Status</th>
        </tr>
      </thead>
      <tbody>
        {#each visible as r (r.is_mms ? 'm' + r.id : 's' + r.id)}
          {@const b = statusBadge(r)}
          <tr class:row--selected={selected[r.id]}>
            <td class="c-check"><input type="checkbox" checked={!!selected[r.id]} onchange={() => {
              const next = { ...selected };
              if (next[r.id]) delete next[r.id]; else next[r.id] = true;
              selected = next;
            }} /></td>
            <td class="c-date">{fmtTime(r.timestamp)}</td>
            <td class="c-contact">
              <div class="contact-name">{getContactName(r.sender)}</div>
              <div class="contact-num">{r.sender}</div>
            </td>
            <td class="c-dir">{r.direction === 'outbound' ? '↗' : '↙'}</td>
            <td class="c-body">
              {#if r.media_count > 0}<span class="media-chip">📎 {r.media_count}</span>{/if}
              {r.body.length > 120 ? r.body.slice(0, 120) + '…' : r.body}
            </td>
            <td class="c-status"><span class={b.cls}>{b.label}</span></td>
          </tr>
        {:else}
          <tr><td colspan="6" class="empty">No message matches these filters.</td></tr>
        {/each}
      </tbody>
    </table>
    {#if filtered.length > visibleCount}
      <button class="btn btn--more" onclick={() => visibleCount += 300}>
        Show more ({filtered.length - visibleCount} remaining)
      </button>
    {/if}
  </div>
</div>

<style>
  .sms-mgr {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    overflow: hidden;
    background: #fff;
  }
  .sms-mgr__head {
    display: flex;
    align-items: flex-end;
    justify-content: space-between;
    gap: 16px;
    padding: 16px 20px 12px;
    border-bottom: 1px solid #dfe1e5;
    flex-shrink: 0;
  }
  .sms-mgr__title-block h2 { margin: 0; font-size: 18px; color: #202124; }
  .sms-mgr__title-block p { margin: 2px 0 0; font-size: 12px; color: #80868b; }
  .sms-mgr__stats { display: flex; gap: 14px; }
  .stat { display: flex; flex-direction: column; align-items: center; min-width: 44px; }
  .stat__num { font-size: 18px; font-weight: 700; color: #202124; }
  .stat__lbl { font-size: 10px; text-transform: uppercase; letter-spacing: .04em; color: #80868b; }
  .stat--warn .stat__num { color: #d93025; }

  .sms-mgr__toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 20px;
    border-bottom: 1px solid #dfe1e5;
    flex-shrink: 0;
    flex-wrap: wrap;
  }
  .f-search {
    flex: 1 1 220px;
    min-width: 160px;
    padding: 8px 12px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    font-size: 13px;
    background: #f8f9fa;
  }
  select {
    padding: 8px 10px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    font-size: 12px;
    background: #fff;
    color: #202124;
    max-width: 180px;
  }
  .sms-mgr__toolbar-spacer { flex: 1; }
  .btn {
    padding: 8px 12px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    background: #fff;
    font-size: 12px;
    font-weight: 600;
    color: #202124;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn:hover { background: #f1f3f4; }
  .btn:disabled { opacity: .5; cursor: default; }
  .btn--danger { border-color: #d93025; color: #d93025; }
  .btn--danger:hover:not(:disabled) { background: #fce8e6; }
  .btn--more { margin: 12px auto; display: block; }

  .sms-mgr__status {
    padding: 8px 20px;
    font-size: 12px;
    color: #137333;
    background: #e6f4ea;
    border-bottom: 1px solid #ceead6;
    flex-shrink: 0;
  }

  .sms-mgr__table-wrap { flex: 1; overflow-y: auto; min-height: 0; }
  .sms-mgr__table { width: 100%; border-collapse: collapse; font-size: 13px; }
  .sms-mgr__table thead th {
    position: sticky;
    top: 0;
    background: #f8f9fa;
    text-align: left;
    padding: 8px 10px;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: .04em;
    color: #5f6368;
    border-bottom: 1px solid #dfe1e5;
    z-index: 1;
  }
  .sms-mgr__table td {
    padding: 8px 10px;
    border-bottom: 1px solid #f1f3f4;
    vertical-align: top;
    color: #202124;
  }
  .row--selected td { background: #e8f0fe; }
  .c-check { width: 30px; }
  .c-date { width: 150px; white-space: nowrap; font-size: 12px; color: #5f6368; }
  .c-contact { width: 170px; }
  .contact-name { font-weight: 600; font-size: 13px; }
  .contact-num { font-size: 11px; color: #80868b; }
  .c-dir { width: 34px; text-align: center; color: #5f6368; }
  .c-body { word-break: break-word; }
  .c-status { width: 80px; }
  .media-chip {
    display: inline-block;
    margin-right: 6px;
    padding: 1px 6px;
    border-radius: 10px;
    background: #f1f3f4;
    font-size: 11px;
    color: #5f6368;
  }
  .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 10px;
    font-size: 11px;
    font-weight: 600;
  }
  .badge--in { background: #e8f0fe; color: #1a73e8; }
  .badge--out { background: #f1f3f4; color: #5f6368; }
  .badge--sent { background: #e6f4ea; color: #137333; }
  .badge--failed { background: #fce8e6; color: #d93025; }
  .badge--spam { background: #fef7e0; color: #b06000; }
  .badge--mms { background: #f3e8fd; color: #8430ce; }
  .empty { text-align: center; color: #80868b; padding: 40px 0 !important; }
</style>
