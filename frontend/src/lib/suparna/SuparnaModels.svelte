<script lang="ts">
  import { onMount } from 'svelte';
  import { MODEL_CATALOG } from './types';
  import { fetchSuparnaStatus } from './suparnaApi';
  import type { SuparnaStatus } from './types';

  let { vpcUrl = '', sessionToken = '' } = $props();

  let status: SuparnaStatus | null = $state(null);

  onMount(async () => {
    if (vpcUrl && sessionToken) {
      status = await fetchSuparnaStatus(vpcUrl, sessionToken);
    }
  });

  function tierLabel(tier: string) {
    if (tier === 'vpc') return 'VPC 1 RAM';
    if (tier === 'phone') return 'Phone deep';
    return 'VPC + Phone';
  }
</script>

<section class="panel">
  <div class="panel__intro">
    <p>GGUF on VPC storage. Phone deployment requires paired APK (Phase 3).</p>
  </div>

  {#if status}
    <div class="status-bar">
      <span>Disk: {status.model_on_disk ? 'model present' : 'missing'}</span>
      <span>RAM: {status.qwen_running ? 'loaded' : 'stopped'}</span>
      {#if status.model_path}
        <span class="path" title={status.model_path}>{status.model_path}</span>
      {/if}
    </div>
  {/if}

  <div class="model-list">
    {#each MODEL_CATALOG as m}
      <article class="model-card status-{m.status}">
        <header>
          <strong>{m.name}</strong>
          <span class="badge">{tierLabel(m.tier)}</span>
        </header>
        <dl>
          <div><dt>File</dt><dd>{m.file}</dd></div>
          <div><dt>Size</dt><dd>~{m.sizeMb} MB</dd></div>
          <div><dt>RAM min</dt><dd>{m.ramMinMb} MB</dd></div>
          <div><dt>Context</dt><dd>{m.context} tokens</dd></div>
          <div><dt>Status</dt><dd>{m.status}</dd></div>
        </dl>
        <p class="notes">{m.notes}</p>
      </article>
    {/each}
  </div>
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
  .status-bar {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    font-size: 11px;
    font-family: ui-monospace, monospace;
    color: #5f6368;
    padding: 8px 10px;
    background: #f8f9fa;
    border: 1px solid #e8eaed;
    border-radius: 4px;
    margin-bottom: 14px;
  }
  .path {
    max-width: 280px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .model-list {
    display: grid;
    gap: 12px;
    max-width: 640px;
  }
  .model-card {
    border: 1px solid #dfe1e5;
    border-radius: 6px;
    padding: 12px 14px;
    background: #fff;
  }
  .model-card header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 8px;
  }
  .badge {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: #5f6368;
    background: #f1f3f4;
    padding: 2px 6px;
    border-radius: 3px;
  }
  .model-card dl {
    display: grid;
    grid-template-columns: 80px 1fr;
    gap: 4px 8px;
    margin: 0 0 8px;
    font-size: 12px;
  }
  .model-card dt {
    color: #80868b;
  }
  .model-card dd {
    margin: 0;
    font-family: ui-monospace, monospace;
  }
  .notes {
    margin: 0;
    font-size: 12px;
    color: #5f6368;
  }
  .status-planned {
    opacity: 0.85;
    border-style: dashed;
  }
</style>
