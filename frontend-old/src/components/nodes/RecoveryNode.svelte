<script lang="ts">
  import BaseNode from './BaseNode.svelte';

  export let data: any = {};
</script>

<BaseNode
  type="recovery"
  title="Recovery Contact (Alice)"
  statusColor="#f59e0b"
>
  <div class="info-row">
    <span class="label">PHONE:</span>
    <span class="value">+33 6 12 34 56 78</span>
  </div>
  <div class="info-row">
    <span class="label">CODE:</span>
    <span class="value code-highlight">#RECOVER_GAFAM</span>
  </div>
  
  <div class="alert-box">
    Authorized to trigger wipe and account recovery via SMS.
  </div>

  <button class="action-btn">REMOVE CONTACT</button>
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
  .code-highlight { color: #fcd34d !important; text-shadow: 0 0 5px rgba(252,211,77,0.4); font-size: 12px;}

  .alert-box {
    margin-top: 8px;
    padding: 8px;
    background: rgba(245, 158, 11, 0.1);
    border: 1px solid rgba(245, 158, 11, 0.3);
    border-radius: 6px;
    font-size: 10px;
    color: #fbbf24;
    line-height: 1.4;
  }

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
    background: #ef4444;
    color: white;
    border-color: #ef4444;
  }
</style>
