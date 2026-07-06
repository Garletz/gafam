<script lang="ts">
  import { onMount } from 'svelte';

  let { vpcUrl, sessionToken }: { vpcUrl: string, sessionToken: string } = $props();

  let guardians: Array<{ id: number, name: string, phone: string, keyword: string }> = $state([]);
  let newName = $state('');
  let newPhone = $state('');
  let newKeyword = $state('URGENCE_GAFAM');
  let isLoading = $state(false);
  let errorMsg = $state('');

  async function fetchGuardians() {
    try {
      const res = await fetch(`${vpcUrl}/api/settings/guardians?token=${sessionToken}`);
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
      const res = await fetch(`${vpcUrl}/api/settings/guardians?token=${sessionToken}`, {
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
      await fetch(`${vpcUrl}/api/settings/guardians?token=${sessionToken}&id=${id}`, {
        method: 'DELETE'
      });
      await fetchGuardians();
    } catch (err) {
      console.error("Failed to delete", err);
    }
  }

  onMount(() => {
    fetchGuardians();
  });
</script>

<div class="settings-panel">
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
</div>

<style>
  .settings-panel {
    padding: 24px;
    height: 100%;
    overflow-y: auto;
    background: #fafafa;
  }
  .settings-header h3 {
    margin: 0 0 8px 0;
    color: #202124;
  }
  .settings-header p {
    margin: 0 0 24px 0;
    color: #5f6368;
    font-size: 14px;
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
