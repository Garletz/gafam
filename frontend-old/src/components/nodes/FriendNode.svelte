<script lang="ts">
  import BaseNode from './BaseNode.svelte';

  export let data: any = {};
</script>

<BaseNode
  type="friend"
  title="Friend VPC (Bob)"
  statusColor="#8b5cf6"
>
  <div class="info-row">
    <span class="label">P2P LINK:</span>
    <span class="value text-purple">SECURE</span>
  </div>
  <div class="info-row">
    <span class="label">LATENCY:</span>
    <span class="value">42ms</span>
  </div>
  <div class="info-row">
    <span class="label">PUBKEY:</span>
    <span class="value truncate">0x9Bf4...1A2C</span>
  </div>

  <button class="action-btn">MANAGE FEDERATION</button>
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
  .text-purple { color: #a78bfa !important; text-shadow: 0 0 5px rgba(167,139,250,0.4); }

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
    background: #8b5cf6;
    color: white;
    border-color: #8b5cf6;
  }
</style>
