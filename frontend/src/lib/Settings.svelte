<script lang="ts">
  import { onMount } from 'svelte';
  import { encryptAESGCM, decryptAESGCM } from '$lib/crypto';
  import { blurContacts, setBlurContacts } from '$lib/privacy';

  let {
    vpcUrl,
    sessionToken,
    section = $bindable<'node' | 'recovery' | 'contacts'>('node'),
    syncContacts = $bindable(true),
    onContactSyncChange
  }: {
    vpcUrl: string;
    sessionToken: string;
    section?: 'node' | 'recovery' | 'contacts';
    syncContacts?: boolean;
    onContactSyncChange?: () => void;
  } = $props();

  type VpcInfo = {
    status: string;
    version: string;
    git_sha: string;
    git_sha_short: string;
    build_time: string;
    image: string;
    uptime_seconds: number;
    started_at: string;
    watchtower: boolean;
  };

  type RegistryInfo = {
    git_sha: string | null;
    git_sha_short: string | null;
    published_at: string | null;
    image: string;
    source?: string;
    available?: boolean;
  };

  let guardians: Array<{ id: number; name: string; phone: string; keyword: string }> = $state([]);
  let newName = $state('');
  let newPhone = $state('');
  let newKeyword = $state('URGENCE_GAFAM');
  let isLoading = $state(false);
  let errorMsg = $state('');

  let vpcInfo: VpcInfo | null = $state(null);
  let registryInfo: RegistryInfo | null = $state(null);
  let vpcLoading = $state(false);
  let vpcError = $state('');
  let updateMsg = $state('');
  let updateLoading = $state(false);
  let updateTriggeredAt = $state(0);

  const UPDATE_POLL_MS = 30_000;
  const ROLLOUT_UPTIME_MAX = 180;

  let updateAvailable = $derived.by(() => {
    if (!vpcInfo || !registryInfo) return false;
    if (vpcInfo.git_sha === 'unknown') return false;
    // Don't invent an update from a stale/hardcoded fallback — only real docker_publish.
    if (!registryInfo.git_sha || registryInfo.source !== 'docker_publish') return false;
    return vpcInfo.git_sha.slice(0, 7) !== registryInfo.git_sha.slice(0, 7);
  });

  type UpdatePhase = 'checking' | 'up_to_date' | 'rolling' | 'available' | 'available_no_wt' | 'ghcr_unknown';

  let updatePhase = $derived.by((): UpdatePhase => {
    if (updateLoading) return 'rolling';
    if (!vpcInfo || !registryInfo) return 'checking';
    if (
      !registryInfo.git_sha ||
      (registryInfo.source && registryInfo.source !== 'docker_publish')
    ) {
      return 'ghcr_unknown';
    }
    if (!updateAvailable) return 'up_to_date';
    if (updateTriggeredAt > 0 && Date.now() - updateTriggeredAt < 120_000) return 'rolling';
    if (vpcInfo.uptime_seconds < ROLLOUT_UPTIME_MAX) return 'rolling';
    if (!vpcInfo.watchtower) return 'available_no_wt';
    return 'available';
  });

  type UpdateStatusInfo = {
    label: string;
    detail: string;
    tone: 'ok' | 'warn' | 'busy' | 'info';
  };

  let updateStatus = $derived.by((): UpdateStatusInfo => {
    switch (updatePhase) {
      case 'checking':
        return { label: 'Checking…', detail: 'Comparing against the latest published Docker image.', tone: 'info' };
      case 'ghcr_unknown':
        return {
          label: 'GHCR check unavailable',
          detail: 'GitHub API rate-limited from Cloudflare. Your node build is shown; retry « Check GHCR » later.',
          tone: 'info'
        };
      case 'up_to_date':
        return {
          label: 'Up to date',
          detail: vpcInfo?.watchtower
            ? 'Watchtower polls GHCR every ~5 minutes.'
            : 'Watchtower unreachable — auto-updates may fail.',
          tone: 'ok'
        };
      case 'rolling':
        return {
          label: 'Update in progress…',
          detail: 'Container restart (~30 s). Pairing and data are kept.',
          tone: 'busy'
        };
      case 'available':
        return {
          label: 'New build available',
          detail: 'Watchtower will apply it within ~5 min, or click to run now.',
          tone: 'warn'
        };
      case 'available_no_wt':
        return {
          label: 'New build available',
          detail: 'Watchtower unreachable from the VPC — wait for auto poll or fix deploy.',
          tone: 'warn'
        };
    }
  });

  let canTriggerUpdate = $derived.by(
    () => vpcInfo?.watchtower && (updatePhase === 'available' || updatePhase === 'up_to_date' || updatePhase === 'ghcr_unknown')
  );

  let updateButtonLabel = $derived.by(() => {
    if (updateLoading || updatePhase === 'rolling') return 'Updating…';
    if (updatePhase === 'available') return 'Update now';
    if (updatePhase === 'ghcr_unknown') return 'Retry GHCR check';
    if (updatePhase === 'up_to_date' && vpcInfo?.watchtower) return 'Check GHCR';
    if (updatePhase === 'available_no_wt') return 'Watchtower unreachable';
    return 'Unavailable';
  });

  function formatUptime(seconds: number) {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}d ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  function formatDate(iso: string) {
    if (!iso || iso === 'unknown') return '—';
    try {
      return new Date(iso).toLocaleString(undefined, {
        dateStyle: 'medium',
        timeStyle: 'short'
      });
    } catch {
      return iso;
    }
  }

  async function fetchVpcStatus() {
    if (!vpcUrl || !sessionToken) return;
    vpcLoading = true;
    vpcError = '';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const [vpcRes, regRes] = await Promise.all([
        fetch(`/api/proxy/vpc-info?${params.toString()}`),
        fetch('/api/registry/latest')
      ]);

      if (vpcRes.ok) {
        vpcInfo = await vpcRes.json();
        if (
          registryInfo &&
          vpcInfo.git_sha !== 'unknown' &&
          vpcInfo.git_sha.slice(0, 7) === registryInfo.git_sha.slice(0, 7)
        ) {
          updateTriggeredAt = 0;
        }
      } else {
        const err = await vpcRes.json().catch(() => ({}));
        vpcInfo = null;
        vpcError = err.error || err.details || 'VPC unreachable';
      }

      if (regRes.ok) {
        registryInfo = await regRes.json();
      } else if (vpcInfo?.git_sha) {
        // GH registry unreachable — don't block Settings on "Checking…"
        registryInfo = {
          git_sha: vpcInfo.git_sha,
          git_sha_short: vpcInfo.git_sha_short,
          published_at: vpcInfo.build_time,
          image: vpcInfo.image,
          source: 'vpc_only'
        };
      }
    } catch {
      vpcError = 'Network error';
      vpcInfo = null;
    } finally {
      vpcLoading = false;
    }
  }

  async function triggerVpcUpdate() {
    if (!vpcUrl || !sessionToken || !canTriggerUpdate) return;
    updateLoading = true;
    updateMsg = '';
    updateTriggeredAt = Date.now();
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/vpc-info?${params.toString()}`, { method: 'POST' });
      const data = await res.json();
      if (res.ok) {
        updateMsg = data.message || 'Update started. Restart in ~30 s.';
        setTimeout(fetchVpcStatus, 8000);
      } else {
        const hint =
          data.error === 'watchtower_unreachable'
            ? ' Re-run deploy-vpc.sh on the server (keep JWT_SECRET).'
            : '';
        updateMsg = (data.message || data.error || 'Update failed') + hint;
        updateTriggeredAt = 0;
      }
    } catch {
      updateMsg = 'Network error';
      updateTriggeredAt = 0;
    } finally {
      updateLoading = false;
    }
  }

  async function fetchGuardians() {
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/guardians?${params.toString()}`);
      if (!res.ok) return;
      const data = await res.json();
      // Proxy / VPC must return an array — never assign a non-array (breaks {#each})
      guardians = Array.isArray(data) ? data : [];
    } catch (err) {
      console.error('Failed to fetch guardians', err);
    }
  }

  async function addGuardian(e: Event) {
    e.preventDefault();
    if (!newName || !newPhone || !newKeyword) return;

    isLoading = true;
    errorMsg = '';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/guardians?${params.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName, phone: newPhone, keyword: newKeyword })
      });
      if (res.ok) {
        newName = '';
        newPhone = '';
        newKeyword = 'URGENCE_GAFAM';
        await fetchGuardians();
      } else {
        const body = await res.json().catch(() => ({}));
        if (res.status === 409 || /unique|constraint|duplicate/i.test(String(body.error || body.message || ''))) {
          errorMsg = 'Ce numéro est déjà enregistré comme gardien.';
        } else {
          errorMsg = body.error || body.message || `Échec ajout (${res.status})`;
        }
      }
    } catch {
      errorMsg = 'Network error';
    } finally {
      isLoading = false;
    }
  }

  async function deleteGuardian(id: number) {
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, id: id.toString() });
      await fetch(`/api/proxy/guardians?${params.toString()}`, { method: 'DELETE' });
      await fetchGuardians();
    } catch (err) {
      console.error('Failed to delete', err);
    }
  }

  // ─── Self phone (SMS → quest remote trigger) ───
  let selfPhone = $state('');
  let selfPhoneLoaded = $state('');
  let selfPhoneMsg = $state('');
  let selfPhoneSaving = $state(false);

  async function loadSelfPhone() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/settings?${params.toString()}`);
      if (!res.ok) return;
      const payload: any = await res.json();
      if (payload.encrypted_data && payload.iv) {
        const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
        const obj = JSON.parse(plaintext);
        if (obj.self_phone) {
          selfPhone = obj.self_phone;
          selfPhoneLoaded = obj.self_phone;
        }
      }
    } catch {}
  }

  async function saveSelfPhone() {
    if (selfPhoneSaving) return;
    selfPhoneSaving = true;
    selfPhoneMsg = '';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const plaintext = JSON.stringify({ key: 'self_phone', value: selfPhone.trim() });
      const encrypted = await encryptAESGCM(plaintext, sessionToken);
      const res = await fetch(`/api/proxy/settings?${params.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(encrypted)
      });
      if (res.ok) {
        selfPhoneLoaded = selfPhone.trim();
        selfPhoneMsg = 'Saved ✓';
      } else {
        selfPhoneMsg = 'Save failed';
      }
    } catch {
      selfPhoneMsg = 'Network error';
    } finally {
      selfPhoneSaving = false;
      setTimeout(() => (selfPhoneMsg = ''), 4000);
    }
  }

  // ─── Agent SMS kill switch (dev phase) ───
  let killSms = $state(false);
  let killSmsSaving = $state(false);

  async function loadKillSms() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/settings?${params.toString()}`);
      if (!res.ok) return;
      const payload: any = await res.json();
      if (payload.encrypted_data && payload.iv) {
        const plaintext = await decryptAESGCM(payload.encrypted_data, payload.iv, sessionToken);
        const obj = JSON.parse(plaintext);
        killSms = obj.agent_kill_sms === '1';
      }
    } catch {}
  }

  async function toggleKillSms() {
    if (killSmsSaving) return;
    killSmsSaving = true;
    const next = !killSms;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const plaintext = JSON.stringify({ key: 'agent_kill_sms', value: next ? '1' : '0' });
      const encrypted = await encryptAESGCM(plaintext, sessionToken);
      const res = await fetch(`/api/proxy/settings?${params.toString()}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(encrypted)
      });
      if (res.ok) killSms = next;
    } catch {} finally {
      killSmsSaving = false;
    }
  }

  onMount(() => {
    fetchGuardians();
    fetchVpcStatus();
    loadSelfPhone();
    loadKillSms();
  });

  $effect(() => {
    if (!updateAvailable || section !== 'node') return;
    const id = setInterval(() => fetchVpcStatus(), UPDATE_POLL_MS);
    return () => clearInterval(id);
  });
</script>

<div class="settings">
  <header class="settings__head">
    <h2 class="settings__title">Settings</h2>
    <nav class="settings__nav" aria-label="Settings sections">
      <button
        type="button"
        class="settings__tab"
        class:is-active={section === 'node'}
        onclick={() => (section = 'node')}
      >
        VPS Node
      </button>
      <button
        type="button"
        class="settings__tab"
        class:is-active={section === 'recovery'}
        onclick={() => (section = 'recovery')}
      >
        Recovery
      </button>
      <button
        type="button"
        class="settings__tab"
        class:is-active={section === 'contacts'}
        onclick={() => (section = 'contacts')}
      >
        Contacts
      </button>
    </nav>
  </header>

  <div class="settings__body">
    {#if section === 'node'}
      <section class="panel">
        <div class="panel__intro">
          <p>Relay server status and updates. Pairing and data are kept during updates.</p>
        </div>

        <div class="status-bar">
          <div class="status-bar__left">
            <span
              class="status-pill"
              class:is-online={!!vpcInfo}
              class:is-offline={!!vpcError && !vpcLoading}
            ></span>
            <span class="status-bar__label">
              {#if vpcLoading}
                Checking…
              {:else if vpcInfo}
                Online
              {:else}
                Offline
              {/if}
            </span>
          </div>
          <button type="button" class="btn-ghost" onclick={fetchVpcStatus} disabled={vpcLoading}>
            Refresh
          </button>
        </div>

        {#if vpcError}
          <p class="panel__error">{vpcError}</p>
        {/if}

        {#if vpcInfo}
          <div class="stats-grid">
            <div class="stat">
              <span class="stat__label">Version</span>
              <span class="stat__value">#{vpcInfo.version}</span>
            </div>
            <div class="stat">
              <span class="stat__label">Build</span>
              <span class="stat__value mono">{vpcInfo.git_sha_short}</span>
            </div>
            <div class="stat">
              <span class="stat__label">Built</span>
              <span class="stat__value">{formatDate(vpcInfo.build_time)}</span>
            </div>
            <div class="stat">
              <span class="stat__label">Uptime</span>
              <span class="stat__value">{formatUptime(vpcInfo.uptime_seconds)}</span>
            </div>
            <div class="stat stat--wide">
              <span class="stat__label">Image</span>
              <span class="stat__value mono">{vpcInfo.image}</span>
            </div>
            <div class="stat">
              <span class="stat__label">Watchtower</span>
              <span class="stat__value">
                {vpcInfo.watchtower ? 'Reachable' : 'Unreachable'}
              </span>
            </div>
          </div>

          <div
            class="subpanel"
            class:subpanel--highlight={updatePhase === 'available' || updatePhase === 'available_no_wt'}
            class:subpanel--busy={updatePhase === 'rolling'}
          >
            <div class="subpanel__head">
              <h3>Software update</h3>
              {#if registryInfo}
                <span class="subpanel__badge" class:subpanel__badge--ok={updatePhase === 'up_to_date'}>
                  {updateStatus.label}
                </span>
              {/if}
            </div>

            <div class="update-status update-status--{updateStatus.tone}" role="status">
              <span class="update-status__dot"></span>
              <div class="update-status__text">
                <strong>{updateStatus.label}</strong>
                <span>{updateStatus.detail}</span>
              </div>
            </div>

            {#if registryInfo}
              <div class="version-compare">
                <div class="version-row">
                  <span class="version-row__label">This node</span>
                  <code>{vpcInfo.git_sha_short}</code>
                </div>
                <div class="version-row">
                  <span class="version-row__label">GHCR image</span>
                  <code>{registryInfo.git_sha_short ?? 'unavailable'}</code>
                </div>
              </div>
            {/if}

            <div class="subpanel__actions">
              <button
                type="button"
                class="btn-primary"
                class:btn-primary--busy={updatePhase === 'rolling'}
                onclick={triggerVpcUpdate}
                disabled={updateLoading || !canTriggerUpdate}
              >
                {updateButtonLabel}
              </button>
              <button
                type="button"
                class="btn-ghost"
                onclick={fetchVpcStatus}
                disabled={vpcLoading}
              >
                {vpcLoading ? 'Refreshing…' : 'Refresh status'}
              </button>
            </div>

            {#if updateMsg}
              <p class="subpanel__msg">{updateMsg}</p>
            {/if}
          </div>

          <div class="subpanel subpanel--kill" class:subpanel--kill-on={killSms}>
            <div class="subpanel__head">
              <h3>Agent controls</h3>
              <span class="kill-badge" class:kill-badge--on={killSms}>
                {killSms ? 'SMS blocked' : 'SMS free'}
              </span>
            </div>
            <p class="kill-hint">
              <strong>Kill switch (dev phase).</strong> While ON, agents cannot send any SMS
              (<code>sms.send</code> fails) — missions, cron and sub-agents keep working,
              only outbound texting is blocked. Mission reports to your self phone still arrive.
            </p>
            <div class="subpanel__actions">
              <button
                type="button"
                class="btn-primary"
                class:btn-primary--danger={!killSms}
                onclick={toggleKillSms}
                disabled={killSmsSaving}
              >
                {killSmsSaving ? 'Saving…' : killSms ? '🔓 Disable kill switch' : '🔒 Enable kill switch'}
              </button>
            </div>
          </div>

          <div class="subpanel">
            <div class="subpanel__head">
              <h3>Privacy</h3>
              <span class="kill-badge" class:kill-badge--on={$blurContacts}>
                {$blurContacts ? 'Names hidden' : 'Names visible'}
              </span>
            </div>
            <p class="kill-hint">
              <strong>Blur contact names.</strong> When ON, the names in the chat list are
              blurred so a shoulder-surfer can't read who you're talking to. Local to this
              device only — nothing is sent to the VPC.
            </p>
            <div class="subpanel__actions">
              <button
                type="button"
                class="btn-primary"
                class:btn-primary--danger={!$blurContacts}
                onclick={() => setBlurContacts(!$blurContacts)}
              >
                {$blurContacts ? '👁 Show names' : '🙈 Blur names'}
              </button>
            </div>
          </div>

          <details class="help-block">
            <summary>Recreate a deleted VPS</summary>
            <ol>
              <li>Open <strong>GAFAM Manager</strong> and create a droplet, or run <code>deploy-vpc.sh</code>.</li>
              <li>Scan the QR code with the APK — pairing stays on the phone.</li>
              <li>Re-authorize on <code>{vpcUrl ? new URL(vpcUrl).hostname : 'yourphone.gafam.cloud'}</code>.</li>
            </ol>
          </details>
        {/if}
      </section>

    {:else if section === 'recovery'}
      <section class="panel">
        <div class="panel__intro">
          <p>Trusted contacts who can trigger emergency login codes if you lose your device.</p>
        </div>

        <!-- Self phone: the only number allowed to trigger a quest by SMS -->
        <div class="selfphone-card">
          <div class="selfphone-card__head">
            <h3 class="form-card__title">Self phone</h3>
            {#if selfPhoneLoaded}
              <span class="selfphone-card__badge">active · {selfPhoneLoaded}</span>
            {/if}
          </div>
          <p class="selfphone-card__hint">
            Your own number — the <strong>only one</strong> allowed to trigger a quest remotely.
            Send <code>/q your instruction</code> by SMS to the relay phone: Saṃyojaka plans it,
            executes it, and texts you back the result.
          </p>
          <div class="form-row">
            <input type="tel" placeholder="+33 6 12 34 56 78" bind:value={selfPhone} />
            <button type="button" class="btn-primary" onclick={saveSelfPhone} disabled={selfPhoneSaving}>
              {selfPhoneSaving ? 'Saving…' : 'Save'}
            </button>
          </div>
          {#if selfPhoneMsg}<p class="selfphone-card__msg">{selfPhoneMsg}</p>{/if}
        </div>

        <div class="guardian-list">
          <p class="guardian-list__count">{guardians.length} guardian{guardians.length === 1 ? '' : 's'}</p>
          {#each guardians as guardian (guardian.id)}
            <article class="guardian-card">
              <div class="guardian-card__main">
                <strong class="guardian-card__name">{guardian.name}</strong>
                <span class="guardian-card__phone">{guardian.phone}</span>
                <span class="guardian-card__keyword">Keyword <code>{guardian.keyword}</code></span>
              </div>
              <button type="button" class="btn-ghost" onclick={() => deleteGuardian(guardian.id)}>
                Remove
              </button>
            </article>
          {/each}
          {#if guardians.length === 0}
            <p class="empty">No guardians configured.</p>
          {/if}
        </div>

        <form class="form-card" onsubmit={addGuardian}>
          <h3 class="form-card__title">Add guardian</h3>
          {#if errorMsg}<p class="panel__error">{errorMsg}</p>{/if}
          <div class="form-row">
            <input type="text" placeholder="Name" bind:value={newName} required />
            <input type="tel" placeholder="Phone (+33…)" bind:value={newPhone} required />
          </div>
          <div class="form-row">
            <input type="text" placeholder="Trigger keyword" bind:value={newKeyword} required />
            <button type="submit" class="btn-primary" disabled={isLoading}>Add</button>
          </div>
        </form>
      </section>

    {:else}
      <section class="panel">
        <div class="panel__intro">
          <p>Sync contact names from your Android device to the web interface.</p>
        </div>

        <div class="toggle-card">
          <label class="toggle-row">
            <input
              type="checkbox"
              bind:checked={syncContacts}
              onchange={() => onContactSyncChange?.()}
            />
            <span class="toggle-row__text">
              <strong>Sync contacts</strong>
              <span>Pull names from the phone and match them to SMS threads.</span>
            </span>
          </label>
        </div>

        <p class="panel__note">
          When enabled, the relay APK sends your address book to your VPC. Data stays on your server.
        </p>
      </section>
    {/if}
  </div>
</div>

<style>
  .kill-hint {
    font-size: 13px;
    color: #5f6368;
    line-height: 1.5;
    margin: 8px 0 12px;
  }
  .kill-hint code {
    background: #f1f3f4;
    padding: 1px 5px;
    border-radius: 4px;
    font-size: 12px;
  }
  .kill-badge {
    font-size: 11px;
    font-weight: 600;
    padding: 2px 8px;
    border-radius: 999px;
    background: #e6f4ea;
    color: #137333;
  }
  .kill-badge--on {
    background: #fce8e6;
    color: #c5221f;
  }
  .subpanel--kill-on {
    border-color: #f6c1c0;
    background: #fffafa;
  }
  .btn-primary--danger {
    background: #c5221f;
    border-color: #c5221f;
  }
  .settings {
    display: flex;
    flex-direction: column;
    flex: 1;
    height: 100%;
    min-height: 0;
    background: #ffffff;
  }

  .settings__head {
    flex-shrink: 0;
    padding: 16px 20px 0;
    border-bottom: 1px solid #dfe1e5;
  }

  .settings__title {
    margin: 0 0 12px;
    font-size: 18px;
    font-weight: 600;
    color: #202124;
  }

  .settings__nav {
    display: flex;
    gap: 4px;
    margin-bottom: -1px;
  }

  .settings__tab {
    padding: 10px 14px;
    border: none;
    background: transparent;
    font-size: 13px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
    border-bottom: 2px solid transparent;
    margin-bottom: 0;
  }

  .settings__tab:hover {
    color: #202124;
    background: #f8f9fa;
  }

  .settings__tab.is-active {
    color: #202124;
    border-bottom-color: #202124;
  }

  .settings__body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    padding: 20px;
  }

  .panel__intro {
    margin-bottom: 20px;
  }

  .panel__intro p,
  .panel__note {
    margin: 0;
    font-size: 14px;
    line-height: 1.5;
    color: #5f6368;
  }

  .panel__note {
    margin-top: 16px;
  }

  .panel__error {
    margin: 0 0 16px;
    font-size: 13px;
    color: #202124;
    font-weight: 500;
  }

  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 12px 14px;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    background: #f8f9fa;
    margin-bottom: 16px;
  }

  .status-bar__left {
    display: flex;
    align-items: center;
    gap: 10px;
  }

  .status-bar__label {
    font-size: 14px;
    font-weight: 600;
    color: #202124;
  }

  .status-pill {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: #bdc1c6;
    flex-shrink: 0;
  }

  .status-pill.is-online {
    background: #202124;
  }

  .status-pill.is-offline {
    background: transparent;
    border: 2px solid #80868b;
    box-sizing: border-box;
  }

  .stats-grid {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 1px;
    background: #dfe1e5;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    overflow: hidden;
    margin-bottom: 16px;
  }

  .stat {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 12px 14px;
    background: #ffffff;
  }

  .stat--wide {
    grid-column: span 2;
  }

  .stat__label {
    font-size: 10px;
    font-weight: 600;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: #80868b;
  }

  .stat__value {
    font-size: 14px;
    color: #202124;
    word-break: break-all;
  }

  .stat__value.mono,
  .mono {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 13px;
  }

  .stat__value--ok,
  .stat__value--warn {
    color: #202124;
    font-weight: 600;
  }

  .subpanel {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 16px;
    background: #ffffff;
    margin-bottom: 16px;
  }

  .subpanel--highlight {
    border-color: #202124;
    box-shadow: inset 3px 0 0 #202124;
  }

  .subpanel--busy {
    border-color: #202124;
    box-shadow: inset 3px 0 0 #202124;
  }

  .update-status {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 12px 14px;
    border-radius: 8px;
    margin-bottom: 14px;
    border: 1px solid #dfe1e5;
    background: #f8f9fa;
  }

  .update-status--ok,
  .update-status--warn,
  .update-status--busy,
  .update-status--info {
    background: #f8f9fa;
    border-color: #dfe1e5;
  }

  .update-status__dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    margin-top: 5px;
    flex-shrink: 0;
    background: #80868b;
  }

  .update-status--busy .update-status__dot {
    background: #202124;
    animation: update-pulse 1.2s ease-in-out infinite;
  }

  @keyframes update-pulse {
    0%,
    100% {
      opacity: 1;
    }
    50% {
      opacity: 0.35;
    }
  }

  .update-status__text {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: 13px;
    line-height: 1.45;
    color: #5f6368;
  }

  .update-status__text strong {
    font-size: 14px;
    color: #202124;
  }

  .subpanel__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 14px;
  }

  .subpanel__head h3 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
    color: #202124;
  }

  .subpanel__badge {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #5f6368;
    border: 1px solid #dfe1e5;
    padding: 3px 8px;
    border-radius: 4px;
    background: #f8f9fa;
  }

  .subpanel--highlight .subpanel__badge {
    color: #202124;
    border-color: #202124;
    background: #ffffff;
  }

  .subpanel__badge--ok {
    color: #202124;
    border-color: #202124;
    background: #ffffff;
  }

  .subpanel__hint {
    margin: 0 0 14px;
    font-size: 13px;
    line-height: 1.45;
    color: #5f6368;
  }

  .version-compare {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 12px;
    margin-bottom: 16px;
  }

  .version-row {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: 10px 12px;
    background: #f8f9fa;
    border: 1px solid #e8eaed;
    border-radius: 6px;
  }

  .version-row__label {
    font-size: 11px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #80868b;
  }

  .version-row code {
    font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace;
    font-size: 14px;
    color: #202124;
  }

  .subpanel__actions {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 10px;
  }

  .btn-primary--busy {
    background: #202124;
    border-color: #202124;
  }

  .subpanel__msg {
    margin: 12px 0 0;
    font-size: 13px;
    color: #202124;
  }

  .help-block {
    font-size: 13px;
    color: #5f6368;
    border-top: 1px solid #e8eaed;
    padding-top: 14px;
  }

  .help-block summary {
    cursor: pointer;
    font-weight: 600;
    color: #202124;
    list-style: none;
  }

  .help-block summary::-webkit-details-marker {
    display: none;
  }

  .help-block ol {
    margin: 10px 0 0;
    padding-left: 18px;
    line-height: 1.55;
  }

  .help-block code {
    font-size: 12px;
    background: #f1f3f4;
    padding: 1px 5px;
    border-radius: 3px;
  }

  .guardian-list {
    display: flex;
    flex-direction: column;
    gap: 10px;
    margin-bottom: 20px;
  }

  .guardian-list__count {
    margin: 0;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    color: #80868b;
  }

  .guardian-card {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 14px 16px;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    background: #ffffff;
  }

  .guardian-card__main {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
  }

  .guardian-card__name {
    font-size: 15px;
    color: #202124;
  }

  .guardian-card__phone {
    font-size: 13px;
    color: #5f6368;
  }

  .guardian-card__keyword {
    font-size: 12px;
    color: #80868b;
    margin-top: 2px;
  }

  .guardian-card__keyword code {
    font-size: 12px;
    background: #f1f3f4;
    padding: 1px 5px;
    border-radius: 3px;
    color: #202124;
  }

  .empty {
    margin: 0;
    padding: 24px;
    text-align: center;
    font-size: 13px;
    color: #80868b;
    border: 1px dashed #dfe1e5;
    border-radius: 8px;
  }

  .form-card {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 16px;
    background: #f8f9fa;
  }

  .form-card__title {
    margin: 0 0 14px;
    font-size: 14px;
    font-weight: 600;
    color: #202124;
  }

  .selfphone-card {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 14px 16px;
    margin-bottom: 18px;
    background: #ffffff;
  }

  .selfphone-card__head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: 6px;
  }

  .selfphone-card__head .form-card__title {
    margin: 0;
  }

  .selfphone-card__badge {
    font-size: 11px;
    font-weight: 600;
    color: #188038;
    background: #ceead6;
    padding: 2px 8px;
    border-radius: 4px;
    font-family: ui-monospace, monospace;
    white-space: nowrap;
  }

  .selfphone-card__hint {
    margin: 0 0 10px;
    font-size: 12.5px;
    line-height: 1.5;
    color: #5f6368;
  }

  .selfphone-card__hint code {
    background: #f1f3f4;
    padding: 1px 5px;
    border-radius: 3px;
    font-size: 11.5px;
  }

  .selfphone-card__msg {
    margin: 8px 0 0;
    font-size: 12px;
    color: #188038;
    font-family: ui-monospace, monospace;
  }

  .form-row {
    display: flex;
    gap: 10px;
    margin-bottom: 10px;
  }

  .form-row:last-child {
    margin-bottom: 0;
  }

  .form-row input {
    flex: 1;
    min-width: 0;
    padding: 10px 12px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    background: #ffffff;
    font-size: 14px;
    color: #202124;
  }

  .form-row input:focus {
    outline: none;
    border-color: #bdc1c6;
  }

  .toggle-card {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 16px;
    background: #f8f9fa;
  }

  .toggle-row {
    display: flex;
    align-items: flex-start;
    gap: 12px;
    cursor: pointer;
  }

  .toggle-row input {
    margin-top: 3px;
    accent-color: #202124;
    cursor: pointer;
  }

  .toggle-row__text {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .toggle-row__text strong {
    font-size: 14px;
    color: #202124;
  }

  .toggle-row__text span {
    font-size: 13px;
    color: #5f6368;
    line-height: 1.4;
  }

  .btn-primary {
    align-self: flex-start;
    padding: 10px 18px;
    background: #202124;
    color: #ffffff;
    border: none;
    border-radius: 6px;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
  }

  .btn-primary:hover:not(:disabled) {
    background: #3c4043;
  }

  .btn-primary:disabled {
    background: #bdc1c6;
    cursor: not-allowed;
  }

  .btn-ghost {
    padding: 6px 12px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    background: #ffffff;
    color: #202124;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    flex-shrink: 0;
  }

  .btn-ghost:hover:not(:disabled) {
    background: #f1f3f4;
  }

  .btn-ghost:disabled {
    opacity: 0.6;
    cursor: not-allowed;
  }

  @media (max-width: 640px) {
    .stats-grid {
      grid-template-columns: 1fr 1fr;
    }

    .stat--wide {
      grid-column: span 2;
    }

    .version-compare {
      grid-template-columns: 1fr;
    }

    .form-row {
      flex-direction: column;
    }
  }
</style>
