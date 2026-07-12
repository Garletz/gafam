<script lang="ts">
  import { onMount } from 'svelte';

  let { vpcUrl, sessionToken }: { vpcUrl: string, sessionToken: string } = $props();

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
    git_sha: string;
    git_sha_short: string;
    published_at: string;
    image: string;
  };

  let guardians: Array<{ id: number, name: string, phone: string, keyword: string }> = $state([]);
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

  let updateAvailable = $derived.by(() => {
    if (!vpcInfo || !registryInfo) return false;
    if (vpcInfo.git_sha === 'unknown') return false;
    return vpcInfo.git_sha.slice(0, 7) !== registryInfo.git_sha.slice(0, 7);
  });

  function formatUptime(seconds: number) {
    const d = Math.floor(seconds / 86400);
    const h = Math.floor((seconds % 86400) / 3600);
    const m = Math.floor((seconds % 3600) / 60);
    if (d > 0) return `${d}j ${h}h`;
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
  }

  function formatDate(iso: string) {
    if (!iso || iso === 'unknown') return '—';
    try {
      return new Date(iso).toLocaleString();
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
      } else {
        const err = await vpcRes.json().catch(() => ({}));
        vpcInfo = null;
        vpcError = err.error || err.details || 'VPC unreachable';
      }

      if (regRes.ok) {
        registryInfo = await regRes.json();
      }
    } catch (err) {
      vpcError = 'Network error';
      vpcInfo = null;
    } finally {
      vpcLoading = false;
    }
  }

  async function triggerVpcUpdate() {
    if (!vpcUrl || !sessionToken) return;
    updateLoading = true;
    updateMsg = '';
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/vpc-info?${params.toString()}`, { method: 'POST' });
      const data = await res.json();
      if (res.ok) {
        updateMsg = data.message || 'Update triggered. The node will restart in ~30s.';
        setTimeout(fetchVpcStatus, 8000);
      } else {
        updateMsg = data.message || data.error || 'Update failed';
      }
    } catch {
      updateMsg = 'Network error';
    } finally {
      updateLoading = false;
    }
  }

  async function fetchGuardians() {
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/guardians?${params.toString()}`);
      if (res.ok) {
        guardians = await res.json();
      }
    } catch (err) {
      console.error("Failed to fetch guardians", err);
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
        errorMsg = 'Failed to add guardian';
      }
    } catch (err) {
      errorMsg = 'Network error';
    } finally {
      isLoading = false;
    }
  }

  async function deleteGuardian(id: number) {
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken, id: id.toString() });
      await fetch(`/api/proxy/guardians?${params.toString()}`, {
        method: 'DELETE'
      });
      await fetchGuardians();
    } catch (err) {
      console.error("Failed to delete", err);
    }
  }

  onMount(() => {
    fetchGuardians();
    fetchVpcStatus();
  });
</script>

<div class="settings-panel">
  <section class="settings-section">
    <div class="settings-header">
      <h3>VPS Node</h3>
      <p>Status, version and updates for your relay server. Data and pairing are preserved during updates.</p>
    </div>

    <div class="vpc-card">
      <div class="vpc-card__top">
        <div class="vpc-status">
          <span class="status-dot {vpcInfo ? 'online' : vpcError ? 'offline' : 'unknown'}"></span>
          <span class="status-label">
            {#if vpcLoading}
              Checking…
            {:else if vpcInfo}
              Online
            {:else}
              Offline
            {/if}
          </span>
        </div>
        <button type="button" class="btn-refresh" onclick={fetchVpcStatus} disabled={vpcLoading}>
          Refresh
        </button>
      </div>

      {#if vpcError}
        <p class="vpc-error">{vpcError}</p>
      {/if}

      {#if vpcInfo}
        <dl class="vpc-meta">
          <div><dt>Version</dt><dd>#{vpcInfo.version}</dd></div>
          <div><dt>Build</dt><dd><code>{vpcInfo.git_sha_short}</code></dd></div>
          <div><dt>Built at</dt><dd>{formatDate(vpcInfo.build_time)}</dd></div>
          <div><dt>Uptime</dt><dd>{formatUptime(vpcInfo.uptime_seconds)}</dd></div>
          <div><dt>Image</dt><dd><code>{vpcInfo.image}</code></dd></div>
          <div><dt>Watchtower</dt><dd>{vpcInfo.watchtower ? 'Enabled' : 'Auto only (legacy node)'}</dd></div>
        </dl>

        {#if registryInfo}
          <div class="update-banner {updateAvailable ? 'update-banner--available' : 'update-banner--ok'}">
            {#if updateAvailable}
              <strong>Update available</strong>
              <span>GitHub main: <code>{registryInfo.git_sha_short}</code> · your node: <code>{vpcInfo.git_sha_short}</code></span>
            {:else}
              <strong>Up to date</strong>
              <span>Latest build on GitHub main (<code>{registryInfo.git_sha_short}</code>)</span>
            {/if}
          </div>
        {/if}

        <div class="vpc-actions">
          <button
            type="button"
            class="btn-update"
            onclick={triggerVpcUpdate}
            disabled={updateLoading || !vpcInfo.watchtower}
            title={vpcInfo.watchtower ? 'Pull latest image now' : 'Redeploy deploy-vpc.sh to enable manual update'}
          >
            {updateLoading ? 'Updating…' : 'Update now'}
          </button>
          <span class="vpc-hint">Auto-update every 5 min via Watchtower. Manual update restarts the container (~30s).</span>
        </div>
        {#if updateMsg}
          <p class="update-msg">{updateMsg}</p>
        {/if}
      {/if}

      <details class="vpc-help">
        <summary>Recreate a deleted VPS</summary>
        <ol>
          <li>Open <strong>GAFAM Manager</strong> → create a DigitalOcean droplet (or run <code>deploy-vpc.sh</code> on any VPS).</li>
          <li>Scan the QR code with the APK — pairing, SMS history and settings stay on the phone.</li>
          <li>Re-authorize on <code>{vpcUrl ? new URL(vpcUrl).hostname : 'yourphone.gafam.cloud'}</code> after pairing.</li>
          <li>No need to rebuild the APK for a VPC code update.</li>
        </ol>
      </details>
    </div>
  </section>

  <section class="settings-section">
    <div class="settings-header">
      <h3>Emergency Social Recovery</h3>
      <p>Add trusted friends or family who can generate emergency login codes for you if you lose your device.</p>
    </div>

    <div class="guardians-list">
      {#each guardians as guardian}
        <div class="guardian-card">
          <div class="guardian-info">
            <strong>{guardian.name}</strong>
            <span class="guardian-phone">{guardian.phone}</span>
            <span class="guardian-keyword">Keyword: <code>{guardian.keyword}</code></span>
          </div>
          <button class="btn-delete" onclick={() => deleteGuardian(guardian.id)}>Remove</button>
        </div>
      {/each}
      {#if guardians.length === 0}
        <p class="empty-state">No trusted guardians added yet.</p>
      {/if}
    </div>

    <form class="add-guardian-form" onsubmit={addGuardian}>
      <h4>Add Trusted Guardian</h4>
      {#if errorMsg}<div class="error">{errorMsg}</div>{/if}
      <div class="form-row">
        <input type="text" placeholder="Name" bind:value={newName} required />
        <input type="tel" placeholder="Phone (e.g. +336...)" bind:value={newPhone} required />
      </div>
      <div class="form-row">
        <input type="text" placeholder="Trigger Keyword" bind:value={newKeyword} required />
        <button type="submit" disabled={isLoading}>Add Guardian</button>
      </div>
    </form>
  </section>
</div>

<style>
  .settings-panel {
    padding: 24px;
    height: 100%;
    min-height: 0;
    box-sizing: border-box;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    background: #fafafa;
  }
  .settings-section {
    margin-bottom: 36px;
  }
  .settings-section:last-child {
    margin-bottom: 0;
  }
  .settings-header h3 {
    margin: 0 0 8px 0;
    color: #202124;
  }
  .settings-header p {
    margin: 0 0 16px 0;
    color: #5f6368;
    font-size: 14px;
  }
  .vpc-card {
    background: white;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 16px;
    box-shadow: 0 2px 4px rgba(0,0,0,0.02);
  }
  .vpc-card__top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    margin-bottom: 12px;
  }
  .vpc-status {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    color: #202124;
  }
  .status-dot {
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: #80868b;
  }
  .status-dot.online { background: #1e8e3e; }
  .status-dot.offline { background: #d93025; }
  .btn-refresh {
    padding: 6px 12px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    background: #fff;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
  }
  .btn-refresh:hover { background: #f1f3f4; }
  .vpc-error {
    color: #d93025;
    font-size: 13px;
    margin: 0 0 12px;
  }
  .vpc-meta {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(180px, 1fr));
    gap: 10px 16px;
    margin: 0 0 16px;
  }
  .vpc-meta div {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .vpc-meta dt {
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #80868b;
    font-weight: 600;
  }
  .vpc-meta dd {
    margin: 0;
    font-size: 14px;
    color: #202124;
  }
  .vpc-meta code {
    font-size: 12px;
    background: #f1f3f4;
    padding: 2px 6px;
    border-radius: 4px;
  }
  .update-banner {
    padding: 10px 12px;
    border-radius: 6px;
    font-size: 13px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-bottom: 12px;
  }
  .update-banner--ok {
    background: #e6f4ea;
    color: #137333;
  }
  .update-banner--available {
    background: #fef7e0;
    color: #b06000;
  }
  .update-banner code {
    background: rgba(0,0,0,0.06);
    padding: 1px 4px;
    border-radius: 3px;
  }
  .vpc-actions {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .btn-update {
    align-self: flex-start;
    padding: 10px 18px;
    background: #202124;
    color: white;
    border: none;
    border-radius: 6px;
    font-weight: 600;
    cursor: pointer;
  }
  .btn-update:disabled {
    background: #80868b;
    cursor: not-allowed;
  }
  .vpc-hint {
    font-size: 12px;
    color: #80868b;
    line-height: 1.4;
  }
  .update-msg {
    margin: 10px 0 0;
    font-size: 13px;
    color: #1a73e8;
  }
  .vpc-help {
    margin-top: 16px;
    font-size: 13px;
    color: #5f6368;
  }
  .vpc-help summary {
    cursor: pointer;
    font-weight: 600;
    color: #202124;
  }
  .vpc-help ol {
    margin: 8px 0 0;
    padding-left: 20px;
    line-height: 1.5;
  }
  .guardians-list {
    display: flex;
    flex-direction: column;
    gap: 12px;
    margin-bottom: 32px;
  }
  .guardian-card {
    background: white;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    box-shadow: 0 2px 4px rgba(0,0,0,0.02);
  }
  .guardian-info {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .guardian-phone {
    color: #5f6368;
    font-size: 14px;
  }
  .guardian-keyword {
    font-size: 12px;
    color: #1a73e8;
    background: #e8f0fe;
    padding: 2px 6px;
    border-radius: 4px;
    width: fit-content;
    margin-top: 4px;
  }
  .btn-delete {
    background: #fce8e6;
    color: #d93025;
    border: none;
    padding: 6px 12px;
    border-radius: 4px;
    cursor: pointer;
    font-size: 13px;
    font-weight: 500;
  }
  .btn-delete:hover {
    background: #fad2cf;
  }
  .empty-state {
    color: #80868b;
    font-style: italic;
  }
  .add-guardian-form {
    background: white;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 20px;
  }
  .add-guardian-form h4 {
    margin: 0 0 16px 0;
  }
  .form-row {
    display: flex;
    gap: 12px;
    margin-bottom: 12px;
  }
  .form-row input {
    flex: 1;
    padding: 10px;
    border: 1px solid #dfe1e5;
    border-radius: 6px;
  }
  .form-row button {
    padding: 10px 20px;
    background: #202124;
    color: white;
    border: none;
    border-radius: 6px;
    cursor: pointer;
    font-weight: 500;
  }
  .form-row button:disabled {
    background: #80868b;
  }
  .error {
    color: #d93025;
    font-size: 13px;
    margin-bottom: 12px;
  }
</style>
