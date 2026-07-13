<script lang="ts">
  import { onMount } from 'svelte';
  import type { LogDay } from './types';
  import SuparnaVpc from './SuparnaVpc.svelte';
  import SuparnaModels from './SuparnaModels.svelte';
  import SuparnaRules from './SuparnaRules.svelte';

  let {
    vpcUrl,
    sessionToken,
    selectedDay = $bindable<string | null>(null),
    days = $bindable<LogDay[]>([]),
    totalBytes = $bindable(0),
    quotaBytes = $bindable(1 << 30),
    section = $bindable<'vpc' | 'models' | 'rules'>('vpc')
  }: {
    vpcUrl: string;
    sessionToken: string;
    selectedDay?: string | null;
    days?: LogDay[];
    totalBytes?: number;
    quotaBytes?: number;
    section?: 'vpc' | 'models' | 'rules';
  } = $props();

  async function refreshLogDays() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({ vpcUrl, token: sessionToken });
      const res = await fetch(`/api/proxy/logs?${params}`);
      if (!res.ok) return;
      const data = await res.json();
      days = data.days || [];
      totalBytes = data.total_bytes || 0;
      quotaBytes = data.quota_bytes || 1 << 30;
      if (!selectedDay && days.length > 0) selectedDay = days[0].day;
    } catch {
      /* ignore */
    }
  }

  onMount(() => {
    refreshLogDays();
  });
</script>

<div class="suparna">
  <header class="suparna__head">
    <h2 class="suparna__title">Suparna</h2>
    <nav class="suparna__nav" aria-label="Suparna sections">
      <button
        type="button"
        class="suparna__tab"
        class:is-active={section === 'vpc'}
        onclick={() => (section = 'vpc')}
      >
        VPC 1 RAM
      </button>
      <button
        type="button"
        class="suparna__tab"
        class:is-active={section === 'models'}
        onclick={() => (section = 'models')}
      >
        Models
      </button>
      <button
        type="button"
        class="suparna__tab"
        class:is-active={section === 'rules'}
        onclick={() => (section = 'rules')}
      >
        Rules
      </button>
    </nav>
  </header>

  <div class="suparna__body">
    {#if section === 'vpc'}
      <SuparnaVpc {vpcUrl} {sessionToken} {selectedDay} {days} />
    {:else if section === 'models'}
      <SuparnaModels {vpcUrl} {sessionToken} />
    {:else}
      <SuparnaRules />
    {/if}
  </div>
</div>

<style>
  .suparna {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    background: #ffffff;
  }

  .suparna__head {
    flex-shrink: 0;
    padding: 16px 20px 0;
    border-bottom: 1px solid #dfe1e5;
  }

  .suparna__title {
    margin: 0 0 12px;
    font-size: 18px;
    font-weight: 600;
    color: #202124;
  }

  .suparna__nav {
    display: flex;
    gap: 4px;
    margin-bottom: -1px;
  }

  .suparna__tab {
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

  .suparna__tab:hover {
    color: #202124;
    background: #f8f9fa;
  }

  .suparna__tab.is-active {
    color: #202124;
    border-bottom-color: #202124;
  }

  .suparna__body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    -webkit-overflow-scrolling: touch;
    padding: 20px;
  }
</style>
