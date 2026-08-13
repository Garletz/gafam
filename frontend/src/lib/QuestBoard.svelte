<script lang="ts">
  import { onDestroy } from 'svelte';

  let {
    vpcUrl = '',
    sessionToken = ''
  }: {
    vpcUrl: string;
    sessionToken: string;
  } = $props();

  type Reward = { verdict: string; score: number; reason: string };
  type Quest = {
    id: string;
    title: string;
    organ_hint: string;
    tool: string;
    params?: Record<string, unknown>;
    depends_on: string[];
    status: string;
    claim: string;
    eta: number;
    result?: unknown;
    error?: string;
    reward?: Reward | null;
  };
  type Mission = {
    id: string;
    instruction: string;
    quests: Quest[];
    status: string;
    mode?: string;
    world_card: string;
    summary?: string;
    created_at?: string;
    updated_at?: string;
  };
  type KarakaInfo = { id: string; name: string; tier: string; status: string };

  let instruction = $state('');
  let mission = $state<Mission | null>(null);
  let karakas = $state<KarakaInfo[]>([]);
  let busy = $state(false);
  let errorMsg = $state('');
  let showWorld = $state(false);
  let worldCard = $state('');
  let addTitle = $state('');
  let addTool = $state('sandbox.storage');
  let addOrgan = $state('suparna_vpc');
  let rewardReason = $state<Record<string, string>>({});
  let pollTimer: ReturnType<typeof setInterval> | null = null;
  let missionList: Mission[] = $state([]);
  let listPoll: ReturnType<typeof setInterval> | null = null;
  let listError = $state('');

  onDestroy(() => { stopPoll(); if (listPoll) clearInterval(listPoll); });

  function q(params: Record<string, string>) {
    return new URLSearchParams({ vpcUrl, token: sessionToken, ...params }).toString();
  }

  function stopPoll() {
    if (pollTimer) { clearInterval(pollTimer); pollTimer = null; }
  }

  async function loadMissionList() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const res = await fetch(`/api/proxy/mission?${q({})}`);
      if (res.ok) { const data: any = await res.json(); missionList = data.missions || []; listError = ''; }
      else { listError = 'Session expired — re-authenticate'; }
    } catch (e: any) { listError = e.message || 'Network error'; }
  }

  function startPoll(id: string) {
    stopPoll();
    pollTimer = setInterval(() => refreshMission(id), 2000);
  }

  async function loadKarakas() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const res = await fetch(`/api/proxy/karaka?${q({ action: 'status' })}`);
      if (res.ok) {
        const data: any = await res.json();
        karakas = data.karakas || [];
      }
    } catch {}
  }

  async function loadWorldCard() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const res = await fetch(`/api/proxy/mission?${q({ action: 'world-card' })}`);
      if (res.ok) {
        const data: any = await res.json();
        worldCard = data.world_card || '';
      }
    } catch {}
  }

  async function refreshMission(id: string) {
    if (!vpcUrl || !sessionToken || !id) return;
    try {
      const res = await fetch(`/api/proxy/mission?${q({ action: 'get', id })}`);
      if (res.ok) {
        mission = await res.json();
        if (mission && (mission.status === 'done' || mission.status === 'cancelled')) stopPoll();
      }
    } catch {}
  }

  async function poseBoard() {
    if (!instruction.trim() || busy) return;
    busy = true;
    errorMsg = '';
    await loadKarakas();
    await loadWorldCard();
    try {
      const res = await fetch(`/api/proxy/mission?${q({ action: 'create' })}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ instruction: instruction.trim() })
      });
      const data: any = await res.json();
      if (!res.ok) {
        errorMsg = data.error || 'Failed to pose board';
        return;
      }
      mission = data;
      if (data.world_card) worldCard = data.world_card;
      startPoll(data.id);
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  // ─── Saṃyojaka: autonomous kāraka run (plan → execute → synthesize) ───
  let autoRunning = $state(false);
  let requireApproval = $state(false);

  async function autoRun(mode: 'action' | 'research' = 'action') {
    if (!instruction.trim() || busy || autoRunning) return;
    autoRunning = true;
    errorMsg = '';
    await loadKarakas();
    await loadWorldCard();
    try {
      const res = await fetch(`/api/proxy/mission?${q({ action: 'orchestrate' })}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          instruction: instruction.trim(),
          karaka_id: 'suparna_vpc',
          mode,
          require_approval: requireApproval
        })
      });
      const data: any = await res.json();
      if (!res.ok) {
        errorMsg = data.error || 'Orchestrator failed to start';
        if (res.status === 409 && data.mission_id) {
          errorMsg = `Saṃyojaka already running on ${data.mission_id}`;
        }
        return;
      }
      mission = null;
      refreshMission(data.mission_id);
      startPoll(data.mission_id);
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      autoRunning = false;
    }
  }

  // Resume a paused mission (e.g. after approving parked quests).
  async function resumeMission(id: string) {
    if (busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(`/api/proxy/mission?${q({ action: 'orchestrate' })}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ mission_id: id, karaka_id: 'suparna_vpc' })
      });
      const data: any = await res.json();
      if (!res.ok) {
        errorMsg = data.error || 'Resume failed';
        return;
      }
      startPoll(id);
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  // Human decision on a parked quest (permission "ask").
  async function approveQuest(qid: string, approve: boolean) {
    if (!mission || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(
        `/api/proxy/mission?${q({ action: 'approve', id: mission.id, qid })}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ approve, reason: rewardReason[qid] || '' })
        }
      );
      const data: any = await res.json();
      if (!res.ok) {
        errorMsg = data.error || 'Approve failed';
        return;
      }
      mission = data;
      // If quests remain pending after the decision and nothing is running
      // server-side, resume the mission so approved work executes.
      if (approve && data.status === 'active') {
        await resumeMission(data.id);
      }
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  // ─── Cron: scheduled missions ───
  type CronJob = {
    id: string;
    name: string;
    instruction: string;
    mode: string;
    every_minutes: number;
    enabled: boolean;
    notify_phone?: string;
    last_run?: number;
  };
  let cronJobs = $state<CronJob[]>([]);
  let cronInstruction = $state('');
  let cronEvery = $state(1440);
  let cronMode = $state<'action' | 'research'>('action');
  let cronNotify = $state(false);

  async function loadCron() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const res = await fetch(`/api/proxy/cron?${q({})}`);
      if (res.ok) {
        const data: any = await res.json();
        cronJobs = data.jobs || [];
      }
    } catch {}
  }

  async function addCron() {
    if (!cronInstruction.trim() || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(`/api/proxy/cron?${q({})}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          instruction: cronInstruction.trim(),
          mode: cronMode,
          every_minutes: cronEvery,
          enabled: true,
          notify_phone: cronNotify ? 'self' : ''
        })
      });
      const data: any = await res.json();
      if (!res.ok) errorMsg = data.error || 'Cron create failed';
      else {
        cronInstruction = '';
        await loadCron();
      }
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function deleteCron(id: string) {
    try {
      await fetch(`/api/proxy/cron?${q({ id })}`, { method: 'DELETE' });
      await loadCron();
    } catch {}
  }

  async function toggleCron(job: CronJob) {
    try {
      await fetch(`/api/proxy/cron?${q({})}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...job, enabled: !job.enabled })
      });
      await loadCron();
    } catch {}
  }

  function cronEveryLabel(min: number): string {
    if (min < 60) return `${min} min`;
    if (min < 1440) return `${Math.round(min / 60)} h`;
    return `${Math.round(min / 1440)} j`;
  }

  async function claimQuest(qid: string, karakaId?: string) {
    if (!mission || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const quest = mission.quests.find((x) => x.id === qid);
      const kid = karakaId || quest?.organ_hint || 'suparna_vpc';
      const res = await fetch(
        `/api/proxy/mission?${q({ action: 'claim', id: mission.id, qid })}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ karaka_id: kid })
        }
      );
      const data: any = await res.json();
      if (!res.ok) errorMsg = data.error || 'Claim failed';
      else mission = data;
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function runQuest(qid: string) {
    if (!mission || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(`/api/proxy/mission?${q({ action: 'run', id: mission.id, qid })}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: '{}'
      });
      const data: any = await res.json();
      if (!res.ok) errorMsg = data.error || 'Run failed';
      else mission = data;
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function rewardQuest(qid: string, verdict: string, score: number) {
    if (!mission || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(
        `/api/proxy/mission?${q({ action: 'reward', id: mission.id, qid })}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            verdict,
            score,
            reason: rewardReason[qid] || '',
            auto_add: verdict === 'needs_more'
          })
        }
      );
      const data: any = await res.json();
      if (!res.ok) errorMsg = data.error || 'Reward failed';
      else mission = data;
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function addQuest() {
    if (!mission || !addTitle.trim() || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(
        `/api/proxy/mission?${q({ action: 'add-quest', id: mission.id })}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            title: addTitle.trim(),
            organ_hint: addOrgan,
            tool: addTool,
            eta: 10
          })
        }
      );
      const data: any = await res.json();
      if (!res.ok) errorMsg = data.error || 'Add failed';
      else {
        mission = data;
        addTitle = '';
      }
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function synthesize() {
    if (!mission || busy) return;
    busy = true;
    errorMsg = '';
    try {
      const res = await fetch(
        `/api/proxy/mission?${q({ action: 'synthesize', id: mission.id })}`,
        { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }
      );
      const data: any = await res.json();
      if (!res.ok) errorMsg = data.error || 'Synthesize failed';
      else {
        mission = data;
        stopPoll();
      }
    } catch (e: any) {
      errorMsg = e.message || 'Network error';
    } finally {
      busy = false;
    }
  }

  async function cancelMission() {
    if (!mission) return;
    stopPoll();
    try {
      await fetch(`/api/proxy/mission?${q({ id: mission.id })}`, { method: 'DELETE' });
    } catch {}
    mission = null;
  }

  $effect(() => {
    if (vpcUrl && sessionToken) {
      loadKarakas();
      loadWorldCard();
      loadMissionList();
      loadCron();
      if (listPoll) clearInterval(listPoll);
      listPoll = setInterval(loadMissionList, 2000);
    }
  });
</script>

<div class="qb">
  <div class="qb-intro">
    <p class="qb-tag">Mokṣa · Method 4</p>
    <p class="qb-lead">Pose a quest board from a demand. Organs claim cells. Reward filters the path.</p>
  </div>

  <div class="qb-pose">
    <textarea
      class="qb-input"
      rows="3"
      placeholder="Demand — e.g. check this URL https://example.com and report risk"
      bind:value={instruction}
      disabled={busy}
    ></textarea>
    <div class="qb-pose-actions">
      <button type="button" class="qb-btn primary" onclick={poseBoard} disabled={busy || autoRunning || !instruction.trim()}>
        {busy ? 'Working…' : 'Pose board'}
      </button>
      <button
        type="button"
        class="qb-btn samyojaka"
        onclick={() => autoRun('action')}
        disabled={busy || autoRunning || !instruction.trim()}
        title="Saṃyojaka — plans the quests, runs them, writes the report"
      >
        {autoRunning ? 'Starting saṃyojaka…' : '⚡ Saṃyojaka'}
      </button>
      <button
        type="button"
        class="qb-btn research"
        onclick={() => autoRun('research')}
        disabled={busy || autoRunning || !instruction.trim()}
        title="Research pipeline — decompose → vault+web sweep → digest → draft → critic → patch → archive"
      >
        {autoRunning ? 'Starting…' : '🔬 Research'}
      </button>
      <button type="button" class="qb-btn ghost" onclick={() => (showWorld = !showWorld)}>
        World card
      </button>
      <label class="qb-approval-toggle" title="Permission 'ask' tools (sms.send, feed.publish, sandbox.exec…) pause for your approval">
        <input type="checkbox" bind:checked={requireApproval} />
        Human approval
      </label>
      {#if mission}
        <button type="button" class="qb-btn ghost" onclick={synthesize} disabled={busy}>Synthesize</button>
        <button type="button" class="qb-btn ghost danger" onclick={cancelMission}>Clear</button>
      {/if}
    </div>
  </div>

  {#if showWorld && worldCard}
    <pre class="qb-world">{worldCard}</pre>
  {/if}

  {#if errorMsg}
    <div class="qb-error">{errorMsg}</div>
  {/if}

  <div class="qb-activity">
    <div class="qb-activity-head">
      <span class="qb-activity-title">Activity Monitor</span>
      <span class="qb-activity-count">{missionList.length} missions</span>
    </div>
    {#if listError}
      <div class="qb-activity-empty qb-activity-err">{listError}</div>
    {:else if missionList.length === 0}
      <div class="qb-activity-empty">No missions yet. Pose a demand or send /q by SMS.</div>
    {:else}
    <div class="qb-activity-list">
      {#each missionList as m}
        <button
          class="qb-activity-row"
          class:am-active={m.status === 'planning' || m.status === 'active' || m.status === 'synthesizing'}
          class:am-done={m.status === 'done'}
          class:am-cancelled={m.status === 'cancelled'}
          onclick={() => { mission = m; if (m.status !== 'done' && m.status !== 'cancelled') startPoll(m.id); }}
        >
          <span class="am-status">
            {#if m.status === 'done'}✅{:else if m.status === 'cancelled'}❌{:else if m.status === 'planning'}🧠{:else if m.status === 'synthesizing'}📝{:else if m.status === 'waiting_approval'}🖐{:else}⚡{/if}
          </span>
          <span class="am-id mono">{m.id}</span>
          <span class="am-instr">{m.instruction}</span>
          <span class="am-tools">
            {#each m.quests.slice(0, 3) as q}
              <span class="am-tool-chip" class:am-tool-running={q.status === 'running' || q.status === 'claimed'}>{q.tool}</span>
            {/each}
            {#if m.quests.length > 3}<span class="am-more">+{m.quests.length - 3}</span>{/if}
          </span>
          <span class="am-pill">{m.status}</span>
        </button>
        {#if m.status !== 'done' && m.status !== 'cancelled'}
          <div class="qb-activity-detail">
            {#each m.quests as q}
              <div class="qb-activity-quest">
                <span class="aq-dot" class:aq-running={q.status === 'running'} class:aq-done={q.status === 'done'} class:aq-failed={q.status === 'failed'} class:aq-pending={q.status === 'pending'} class:aq-waiting={q.status === 'waiting_approval'}></span>
                <span class="aq-id mono">{q.id}</span>
                <span class="aq-tool mono">{q.tool}</span>
                <span class="aq-status">{q.status}</span>
              </div>
            {/each}
          </div>
        {/if}
      {/each}
    </div>
    {/if}
  </div>

  <div class="qb-board-wrap">
    <div class="qb-board-title">
      <span>Quest Board</span>
      {#if mission}
        <span class="mono">· {mission.id}</span>
        <span class="pill" class:pill-pulse={mission.status === 'planning' || mission.status === 'synthesizing'}>{mission.status}</span>
        {#if mission.mode === 'research'}
          <span class="pill pill-research">research</span>
        {/if}
        {#if mission.status === 'planning'}
          <span class="samyojaka-working">⚡ saṃyojaka planning quests…</span>
        {:else if mission.status === 'synthesizing'}
          <span class="samyojaka-working">⚡ saṃyojaka writing report…</span>
        {/if}
      {:else}
        <span class="muted">· empty — pose a demand above</span>
      {/if}
    </div>
    {#if mission}
      <p class="qb-demand muted">Demand: {mission.instruction}</p>
    {/if}

    <div class="qb-board" role="table" aria-label="Quest board">
      <div class="qb-row qb-head" role="row">
        <span>Quest</span>
        <span>Organ</span>
        <span>Tool</span>
        <span>ETA</span>
        <span>Status</span>
        <span>Reward</span>
        <span>Actions</span>
      </div>

      {#if mission}
        {#each mission.quests as quest (quest.id)}
          <div class="qb-row" class:is-done={quest.status === 'done'} class:is-failed={quest.status === 'failed'} role="row">
            <div class="qb-cell title">
              <strong>{quest.id}</strong>
              <span>{quest.title}</span>
              {#if quest.error}
                <em class="err">{quest.error}</em>
              {/if}
            </div>
            <div class="qb-cell mono">{quest.claim || quest.organ_hint || '—'}</div>
            <div class="qb-cell mono">{quest.tool || '(judge)'}</div>
            <div class="qb-cell">{quest.eta}s</div>
            <div class="qb-cell"><span class="pill" class:pill-waiting={quest.status === 'waiting_approval'}>{quest.status}</span></div>
            <div class="qb-cell reward">
              {#if quest.reward}
                <span class="pill verdict-{quest.reward.verdict}">{quest.reward.verdict}</span>
                <span class="mono">{quest.reward.score.toFixed(1)}</span>
                {#if quest.reward.reason}<span class="muted">{quest.reward.reason}</span>{/if}
              {:else}
                <span class="muted">—</span>
              {/if}
            </div>
            <div class="qb-cell actions">
              {#if quest.status === 'waiting_approval'}
                <span class="qb-ask-label" title={quest.tool}>⚠️ {quest.tool}</span>
                <button type="button" class="qb-mini ok" onclick={() => approveQuest(quest.id, true)} disabled={busy}>Approve</button>
                <button type="button" class="qb-mini bad" onclick={() => approveQuest(quest.id, false)} disabled={busy}>Reject</button>
              {/if}
              {#if quest.status === 'pending'}
                <button type="button" class="qb-mini" onclick={() => claimQuest(quest.id)} disabled={busy}>Claim</button>
              {/if}
              {#if quest.status === 'claimed' || quest.status === 'failed'}
                {#if quest.tool}
                  <button type="button" class="qb-mini" onclick={() => runQuest(quest.id)} disabled={busy}>Run</button>
                {/if}
              {/if}
              {#if quest.status === 'done' || quest.status === 'failed' || quest.status === 'claimed'}
                <input
                  class="qb-reason"
                  placeholder="reason"
                  bind:value={rewardReason[quest.id]}
                />
                <button type="button" class="qb-mini ok" onclick={() => rewardQuest(quest.id, 'done', 0.9)} disabled={busy}>done</button>
                <button type="button" class="qb-mini bad" onclick={() => rewardQuest(quest.id, 'failed', 0.1)} disabled={busy}>failed</button>
                <button type="button" class="qb-mini more" onclick={() => rewardQuest(quest.id, 'needs_more', 0.4)} disabled={busy}>needs_more</button>
              {/if}
            </div>
          </div>
        {/each}
      {:else}
        {#each [1, 2, 3, 4, 5] as i}
          <div class="qb-row qb-placeholder" role="row">
            <div class="qb-cell title">
              <strong>Q{i}</strong>
              <span class="ghost-line"></span>
            </div>
            <div class="qb-cell"><span class="ghost-chip"></span></div>
            <div class="qb-cell"><span class="ghost-chip"></span></div>
            <div class="qb-cell muted">—</div>
            <div class="qb-cell"><span class="pill ghost">pending</span></div>
            <div class="qb-cell muted">—</div>
            <div class="qb-cell muted">claim · run · reward</div>
          </div>
        {/each}
      {/if}
    </div>
  </div>

  {#if mission}
    <div class="qb-add">
      <input class="qb-add-input" placeholder="Add quest title (supervisor)" bind:value={addTitle} />
      <select bind:value={addOrgan}>
        {#each karakas as k}
          <option value={k.id}>{k.name} ({k.id})</option>
        {:else}
          <option value="suparna_vpc">suparna_vpc</option>
          <option value="edge_l2_phone">edge_l2_phone</option>
        {/each}
      </select>
      <select bind:value={addTool}>
        <option value="sandbox.storage">sandbox.storage</option>
        <option value="sandbox.file_list">sandbox.file_list</option>
        <option value="sandbox.exec">sandbox.exec</option>
        <option value="browser.status">browser.status</option>
        <option value="browser.screenshot">browser.screenshot</option>
        <option value="">(judge / no tool)</option>
      </select>
      <button type="button" class="qb-btn" onclick={addQuest} disabled={busy || !addTitle.trim()}>Add quest</button>
    </div>

    {#if mission.summary}
      <pre class="qb-summary">{mission.summary}</pre>
    {/if}
  {/if}

  <div class="cron">
    <div class="cron-head">
      <span class="cron-title">⏰ Scheduled missions</span>
      <span class="qb-activity-count">{cronJobs.length} jobs</span>
    </div>
    {#if cronJobs.length > 0}
      <div class="cron-list">
        {#each cronJobs as job (job.id)}
          <div class="cron-row" class:cron-off={!job.enabled}>
            <button
              type="button"
              class="qb-mini"
              class:ok={job.enabled}
              onclick={() => toggleCron(job)}
              title={job.enabled ? 'Disable' : 'Enable'}
            >{job.enabled ? 'on' : 'off'}</button>
            <span class="cron-name">{job.name}</span>
            <span class="cron-every mono">every {cronEveryLabel(job.every_minutes)}</span>
            {#if job.mode}<span class="pill pill-research">{job.mode}</span>{/if}
            {#if job.notify_phone}<span class="cron-notify" title="SMS report enabled">📱</span>{/if}
            {#if job.last_run}
              <span class="cron-last muted">{new Date(job.last_run * 1000).toLocaleString()}</span>
            {/if}
            <button type="button" class="qb-mini bad" onclick={() => deleteCron(job.id)}>✕</button>
          </div>
        {/each}
      </div>
    {/if}
    <div class="cron-add">
      <input
        class="qb-add-input"
        placeholder="Recurring instruction — e.g. every morning: sweep the vault, summarize important SMS"
        bind:value={cronInstruction}
      />
      <select bind:value={cronEvery}>
        <option value={30}>30 min</option>
        <option value={60}>1 h</option>
        <option value={360}>6 h</option>
        <option value={720}>12 h</option>
        <option value={1440}>24 h</option>
        <option value={10080}>7 j</option>
      </select>
      <select bind:value={cronMode}>
        <option value="action">action</option>
        <option value="research">research</option>
      </select>
      <label class="qb-approval-toggle" title="Text me the result on my self phone">
        <input type="checkbox" bind:checked={cronNotify} />
        SMS
      </label>
      <button type="button" class="qb-btn" onclick={addCron} disabled={busy || !cronInstruction.trim()}>Add</button>
    </div>
  </div>
</div>

<style>
  .qb {
    padding: 16px 20px 32px;
    overflow: auto;
    height: 100%;
    box-sizing: border-box;
  }
  .qb-tag {
    margin: 0;
    font-size: 11px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: #80868b;
  }
  .qb-lead {
    margin: 4px 0 16px;
    color: #5f6368;
    font-size: 14px;
  }
  .qb-pose {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 12px;
  }
  .qb-input {
    width: 100%;
    box-sizing: border-box;
    border: 1px solid #dadce0;
    border-radius: 8px;
    padding: 12px;
    font: inherit;
    font-size: 14px;
    resize: vertical;
    min-height: 72px;
  }
  .qb-pose-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .qb-btn {
    border: 1px solid #dadce0;
    background: #fff;
    color: #202124;
    border-radius: 6px;
    padding: 8px 12px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }
  .qb-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .qb-btn.primary {
    background: #202124;
    color: #fff;
    border-color: #202124;
  }
  .qb-btn.samyojaka {
    background: #1a73e8;
    color: #fff;
    border-color: #1a73e8;
  }
  .qb-btn.samyojaka:hover:not(:disabled) {
    background: #1765cc;
  }
  .qb-btn.research {
    background: #137333;
    color: #fff;
    border-color: #137333;
  }
  .qb-btn.research:hover:not(:disabled) {
    background: #0d5626;
  }
  .pill.pill-research {
    background: #e6f4ea;
    color: #137333;
  }
  .qb-btn.ghost {
    background: transparent;
  }
  .qb-btn.danger {
    color: #c5221f;
    border-color: #f6c1c0;
  }
  .samyojaka-working {
    font-size: 12px;
    color: #1a73e8;
    font-weight: 600;
    animation: samyojakapulse 1.4s ease-in-out infinite;
  }
  @keyframes samyojakapulse {
    0%, 100% { opacity: 1; }
    50% { opacity: 0.4; }
  }

  /* Activity Monitor */
  .qb-activity {
    margin: 0 0 12px;
    border: 1px solid #e8eaed;
    border-radius: 6px;
    overflow: hidden;
  }
  .qb-activity-head {
    display: flex; justify-content: space-between; align-items: center;
    padding: 6px 10px; background: #f8f9fa;
    border-bottom: 1px solid #e8eaed;
  }
  .qb-activity-title { font-size: 12px; font-weight: 600; color: #202124; }
  .qb-activity-count { font-size: 11px; color: #80868b; }
  .qb-activity-list { display: flex; flex-direction: column; }
  .qb-activity-row {
    display: flex; align-items: center; gap: 8px; padding: 5px 10px;
    border: none; background: none; cursor: pointer; text-align: left;
    font-size: 12px; border-bottom: 1px solid #f1f3f4; width: 100%;
  }
  .qb-activity-row:last-child { border-bottom: none; }
  .qb-activity-row:hover { background: #f8f9fa; }
  .am-active { background: #e8f0fe; }
  .am-active:hover { background: #d2e3fc; }
  .am-done { opacity: 0.7; }
  .am-cancelled { opacity: 0.5; }
  .am-status { font-size: 14px; flex-shrink: 0; }
  .am-id { font-size: 10px; color: #80868b; flex-shrink: 0; min-width: 70px; }
  .am-instr {
    flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
    color: #202124; font-size: 12px;
  }
  .am-tools { display: flex; gap: 3px; flex-shrink: 0; }
  .am-tool-chip {
    padding: 1px 5px; font-size: 9px; border-radius: 3px;
    background: #e8eaed; color: #5f6368; font-family: monospace;
  }
  .am-active .am-tool-chip { background: #c6dafc; color: #174ea6; }
  .am-more { font-size: 9px; color: #80868b; }
  .am-pill {
    padding: 1px 6px; border-radius: 8px; font-size: 10px; font-weight: 600;
    background: #e8eaed; color: #5f6368; flex-shrink: 0;
  }
  .am-active .am-pill { background: #1a73e8; color: #fff; }
  .am-done .am-pill { background: #e6f4ea; color: #137333; }
  .am-cancelled .am-pill { background: #fce8e6; color: #c5221f; }
  .am-tool-running { background: #d2e3fc; color: #174ea6; animation: toolPulse 1.5s ease-in-out infinite; }
  @keyframes toolPulse { 0%, 100% { opacity: 1; } 50% { opacity: 0.5; } }

  /* Quest detail (per mission) */
  .qb-activity-detail {
    padding: 0 10px 6px; display: flex; flex-direction: column; gap: 1px;
    border-bottom: 1px solid #f1f3f4;
  }
  .qb-activity-quest {
    display: flex; align-items: center; gap: 6px; font-size: 11px;
    color: #5f6368;
  }
  .aq-dot { width: 6px; height: 6px; border-radius: 50%; background: #dadce0; flex-shrink: 0; }
  .aq-running { background: #1a73e8; animation: toolPulse 1.5s ease-in-out infinite; }
  .aq-done { background: #137333; }
  .aq-failed { background: #c5221f; }
  .aq-pending { background: #dadce0; }
  .aq-id { font-size: 10px; color: #80868b; }
  .aq-tool { font-size: 10px; }
  .aq-status { font-size: 10px; margin-left: auto; }
  .aq-status.pending { color: #80868b; }

  .qb-activity-empty { padding: 16px; text-align: center; font-size: 12px; color: #80868b; }
  .qb-activity-err { color: #c5221f; font-weight: 600; }

  .qb-world,
  .qb-summary {
    background: #f8f9fa;
    border: 1px solid #e8eaed;
    border-radius: 8px;
    padding: 12px;
    font-size: 12px;
    white-space: pre-wrap;
    overflow: auto;
    max-height: 240px;
    margin: 0 0 12px;
  }
  .qb-error {
    background: #fce8e6;
    color: #c5221f;
    padding: 10px 12px;
    border-radius: 6px;
    margin-bottom: 12px;
    font-size: 13px;
  }
  .qb-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    align-items: center;
    margin-bottom: 12px;
    font-size: 13px;
  }
  .qb-board-wrap {
    margin-top: 4px;
    margin-bottom: 16px;
  }
  .qb-board-title {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 8px;
    font-size: 13px;
    font-weight: 700;
    color: #202124;
  }
  .qb-demand {
    margin: 0 0 10px;
    font-size: 13px;
  }
  .mono {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
  }
  .muted {
    color: #80868b;
  }
  .pill {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 999px;
    background: #e8eaed;
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
  }
  .pill.ghost {
    background: #f1f3f4;
    color: #bdc1c6;
  }
  .pill.pill-pulse {
    background: #e8f0fe;
    color: #1a73e8;
    animation: samyojakapulse 1.4s ease-in-out infinite;
  }
  .verdict-done {
    background: #e6f4ea;
    color: #137333;
  }
  .verdict-failed {
    background: #fce8e6;
    color: #c5221f;
  }
  .verdict-needs_more {
    background: #fef7e0;
    color: #b06000;
  }
  .qb-board {
    border: 1px solid #dadce0;
    border-radius: 8px;
    overflow: hidden;
    background:
      linear-gradient(#e8eaed 1px, transparent 1px) 0 0 / 100% 44px,
      linear-gradient(90deg, #eef0f2 1px, transparent 1px) 0 0 / 14.28% 100%,
      #fff;
  }
  .qb-row {
    display: grid;
    grid-template-columns: 1.6fr 0.9fr 0.9fr 0.4fr 0.7fr 1fr 1.4fr;
    gap: 8px;
    padding: 10px 12px;
    border-top: 1px solid #e8eaed;
    font-size: 12px;
    align-items: start;
    background: rgba(255, 255, 255, 0.92);
  }
  .qb-row.qb-head {
    border-top: none;
    background: #f8f9fa;
    font-weight: 700;
    color: #5f6368;
    text-transform: uppercase;
    letter-spacing: 0.03em;
    font-size: 10px;
  }
  .qb-row.is-done {
    background: rgba(247, 251, 248, 0.95);
  }
  .qb-row.is-failed {
    background: rgba(255, 248, 247, 0.95);
  }
  .qb-row.qb-placeholder {
    min-height: 44px;
    color: #9aa0a6;
  }
  .ghost-line {
    display: block;
    height: 8px;
    width: 70%;
    margin-top: 6px;
    border-radius: 4px;
    background: #e8eaed;
  }
  .ghost-chip {
    display: inline-block;
    height: 10px;
    width: 64px;
    border-radius: 4px;
    background: #e8eaed;
  }
  .qb-cell.title {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .qb-cell.title .err {
    color: #c5221f;
    font-style: normal;
  }
  .qb-cell.actions {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
    align-items: center;
  }
  .qb-cell.reward {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .qb-mini {
    border: 1px solid #dadce0;
    background: #fff;
    border-radius: 4px;
    padding: 3px 6px;
    font-size: 11px;
    font-weight: 600;
    cursor: pointer;
  }
  .qb-mini.ok {
    border-color: #ceead6;
    color: #137333;
  }
  .qb-mini.bad {
    border-color: #f6c1c0;
    color: #c5221f;
  }
  .qb-mini.more {
    border-color: #fde293;
    color: #b06000;
  }
  .qb-reason {
    width: 72px;
    border: 1px solid #dadce0;
    border-radius: 4px;
    padding: 3px 6px;
    font-size: 11px;
  }
  .qb-add {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 16px;
  }
  .qb-add-input {
    flex: 1;
    min-width: 160px;
    border: 1px solid #dadce0;
    border-radius: 6px;
    padding: 8px 10px;
    font-size: 13px;
  }
  .qb-add select {
    border: 1px solid #dadce0;
    border-radius: 6px;
    padding: 8px;
    font-size: 12px;
    background: #fff;
  }
  .qb-empty {
    color: #80868b;
    font-size: 14px;
    padding: 24px 0;
  }
  .qb-approval-toggle {
    display: inline-flex;
    align-items: center;
    gap: 5px;
    font-size: 12px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
    padding: 6px 4px;
  }
  .pill.pill-waiting {
    background: #fef7e0;
    color: #b06000;
    animation: samyojakapulse 1.4s ease-in-out infinite;
  }
  .aq-waiting {
    background: #b06000;
    animation: toolPulse 1.5s ease-in-out infinite;
  }
  .qb-ask-label {
    font-size: 10px;
    color: #b06000;
    font-weight: 600;
  }

  /* Cron panel */
  .cron {
    margin-top: 8px;
    border: 1px solid #e8eaed;
    border-radius: 6px;
    overflow: hidden;
  }
  .cron-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 6px 10px;
    background: #f8f9fa;
    border-bottom: 1px solid #e8eaed;
  }
  .cron-title {
    font-size: 12px;
    font-weight: 600;
    color: #202124;
  }
  .cron-list {
    display: flex;
    flex-direction: column;
  }
  .cron-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 10px;
    font-size: 12px;
    border-bottom: 1px solid #f1f3f4;
  }
  .cron-row:last-child {
    border-bottom: none;
  }
  .cron-row.cron-off {
    opacity: 0.5;
  }
  .cron-name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: #202124;
  }
  .cron-every {
    font-size: 10px;
    color: #80868b;
  }
  .cron-notify {
    font-size: 11px;
  }
  .cron-last {
    font-size: 10px;
  }
  .cron-add {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding: 8px 10px;
    border-top: 1px solid #f1f3f4;
    background: #f8f9fa;
  }
  .cron-add select {
    border: 1px solid #dadce0;
    border-radius: 6px;
    padding: 8px;
    font-size: 12px;
    background: #fff;
  }
  @media (max-width: 900px) {
    .qb-row {
      grid-template-columns: 1fr;
    }
    .qb-row.qb-head {
      display: none;
    }
  }
</style>
