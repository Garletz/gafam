<script lang="ts">
  import BaseNode from './BaseNode.svelte';

  export let data: any = {};
</script>

<BaseNode
  type="hardware"
  title="Hardware Bridge"
  statusColor="#10b981"
>
  <div class="device-icon">
    📱 GALET / APK
  </div>
  
  <div class="info-row">
    <span class="label">BATTERY:</span>
    <span class="value text-green">89%</span>
  </div>
  <div class="info-row">
    <span class="label">GSM SIGNAL:</span>
    <span class="value">Strong</span>
  </div>
  
  <button class="action-btn red">REBOOT HARDWARE</button>
</BaseNode>

<style>
  .device-icon {
    font-size: 24px;
    text-align: center;
    padding: 10px;
    background: rgba(255,255,255,0.03);
    border-radius: 8px;
    margin-bottom: 8px;
  }
  .info-row {
    display: flex;
    justify-content: space-between;
    font-size: 11px;
    border-bottom: 1px solid rgba(255,255,255,0.1);
    padding-bottom: 4px;
  }
  .label { color: #a1a1aa; font-weight: 600; }
  .value { color: #f4f4f5; font-family: 'Courier New', Courier, monospace; font-weight: 700; }
  .text-green { color: #34d399 !important; text-shadow: 0 0 5px rgba(52,211,153,0.4); }
  
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
  .action-btn.red:hover {
    background: #ef4444;
    color: white;
    border-color: #ef4444;
  }
</style>
