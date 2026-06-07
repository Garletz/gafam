<script lang="ts">
  import BaseNode from './BaseNode.svelte';

  export let data: any = {};
</script>

<BaseNode
  type="client"
  title="Web Client (Chrome)"
  statusColor="#10b981"
>
  <div class="info-row">
    <span class="label">SESSION:</span>
    <span class="value">ACTIVE</span>
  </div>
  <div class="info-row">
    <span class="label">FINGERPRINT:</span>
    <span class="value truncate">SHA256: 8fC829A...</span>
  </div>
  <div class="info-row">
    <span class="label">LAST SEEN:</span>
    <span class="value">Just now</span>
  </div>

  <button class="action-btn">REVOKE ACCESS</button>
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
  .truncate { max-width: 120px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .action-btn {
    margin-top: 8px;
    padding: 8px;
    border-radius: 6px;
    border: none;
    font-weight: 700;
    font-size: 11px;
    cursor: pointer;
    background: #18181b;
    color: #f4f4f5;
    border: 1px solid rgba(255,255,255,0.1);
  }
  .action-btn:hover {
    background: #f59e0b; /* Orange warning */
    color: white;
    border-color: #f59e0b;
  }
</style>
