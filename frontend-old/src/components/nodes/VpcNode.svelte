<script lang="ts">
  import BaseNode from './BaseNode.svelte';

  // Svelte Flow will pass 'data' if we need it
  export let data: any = {};
</script>

<BaseNode
  type="vpc"
  title="VPC Central Relay"
  statusColor="#3b82f6"
>
  <div class="info-row">
    <span class="label">STATUS:</span>
    <span class="value text-blue">ONLINE</span>
  </div>
  <div class="info-row">
    <span class="label">UPTIME:</span>
    <span class="value">99.9%</span>
  </div>
  <div class="info-row">
    <span class="label">IP:</span>
    <span class="value">165.22.45.98</span>
  </div>
  
  <div class="stats-box">
    <div class="stat">
      <span>CLIENTS</span>
      <strong>2</strong>
    </div>
    <div class="stat">
      <span>HARDWARE</span>
      <strong>1</strong>
    </div>
    <div class="stat">
      <span>PEERS</span>
      <strong>1</strong>
    </div>
  </div>
</BaseNode>

<style>
  .info-row {
    display: flex;
    justify-content: space-between;
    font-size: 11px;
    border-bottom: 1px solid rgba(255,255,255,0.1);
    padding-bottom: 4px;
  }
  .label { color: #a1a1aa; font-weight: 600; }
  .value { color: #f4f4f5; font-family: 'Courier New', Courier, monospace; font-weight: 700; }
  .text-blue { color: #60a5fa !important; text-shadow: 0 0 5px rgba(96,165,250,0.4); }
  
  .stats-box {
    display: flex;
    gap: 8px;
    margin-top: 8px;
  }
  .stat {
    flex: 1;
    background: rgba(255,255,255,0.05);
    border-radius: 6px;
    padding: 8px;
    text-align: center;
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .stat span { font-size: 9px; color: #a1a1aa; font-weight: 600; }
  .stat strong { font-size: 14px; color: #3b82f6; font-family: 'Courier New', Courier, monospace; }
</style>
