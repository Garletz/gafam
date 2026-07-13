<script lang="ts">
  import { onMount } from 'svelte';
  import { DEFAULT_SUPARNA_RULES, type SuparnaRules } from './types';

  const STORAGE_KEY = 'gafam_suparna_rules';

  let rules: SuparnaRules = $state({ ...DEFAULT_SUPARNA_RULES });
  let saved = $state(false);

  onMount(() => {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (raw) rules = { ...DEFAULT_SUPARNA_RULES, ...JSON.parse(raw) };
    } catch {
      /* ignore */
    }
  });

  function persist() {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(rules));
    saved = true;
    setTimeout(() => (saved = false), 2000);
  }
</script>

<section class="panel">
  <div class="panel__intro">
    <p>Local preferences (VPC API persistence in Phase 2).</p>
  </div>

  <label class="field">
    <span>Auto-stop after idle (minutes)</span>
    <input type="number" min="1" max="30" bind:value={rules.autoStopMinutes} />
  </label>

  <label class="field checkbox">
    <input type="checkbox" bind:checked={rules.preferVpcForSmallTasks} />
    <span>Prefer VPC for small tasks when phone is online</span>
  </label>

  <label class="field checkbox">
    <input type="checkbox" bind:checked={rules.showTimeline} />
    <span>Show timeline in analysis results</span>
  </label>

  <button type="button" class="btn" onclick={persist}>{saved ? 'Saved' : 'Save rules'}</button>
</section>

<style>
  .panel__intro {
    margin-bottom: 20px;
  }
  .panel__intro p {
    margin: 0;
    font-size: 14px;
    line-height: 1.5;
    color: #5f6368;
  }
  .panel {
    max-width: 520px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 6px;
    margin-bottom: 14px;
    font-size: 13px;
  }
  .field.checkbox {
    flex-direction: row;
    align-items: center;
    gap: 10px;
  }
  .field input[type='number'] {
    width: 80px;
    padding: 6px 8px;
    border: 1px solid #dadce0;
    border-radius: 4px;
  }
  .btn {
    background: #202124;
    color: #fff;
    border: none;
    border-radius: 4px;
    padding: 8px 14px;
    font-size: 13px;
    cursor: pointer;
  }
  .btn:hover {
    background: #3c4043;
  }
</style>
