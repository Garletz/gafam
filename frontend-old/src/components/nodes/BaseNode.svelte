<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte';
  export let type: string;
  export let title: string;
  export let statusColor: string = "#10b981"; // default green
</script>

<!-- The outer div is just the card styling now, Svelte Flow handles the absolute positioning -->
<div class="drag-node">
  <!-- Invisible or subtle handles for connections -->
  <Handle type="target" position={Position.Top} id="top" class="custom-handle" />
  <Handle type="source" position={Position.Bottom} id="bottom" class="custom-handle" />
  <Handle type="target" position={Position.Left} id="left" class="custom-handle" />
  <Handle type="source" position={Position.Right} id="right" class="custom-handle" />

  <div class="node-header custom-drag-handle">
    <span class="status-dot" style="background-color: {statusColor}; box-shadow: 0 0 8px {statusColor};"></span>
    <span class="node-title">{title}</span>
  </div>

  <div class="node-content">
    <slot></slot>
  </div>
</div>

<style>
  .drag-node {
    width: 280px;
    background: rgba(24, 24, 27, 0.95); /* Black card */
    backdrop-filter: blur(16px);
    -webkit-backdrop-filter: blur(16px);
    border-radius: 12px;
    border: 1px solid rgba(255, 255, 255, 0.1);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    cursor: default;
    user-select: none;
    box-shadow:
      0 10px 0 #09090b,
      0 16px 28px rgba(0, 0, 0, 0.5),
      0 0 0 1px rgba(255, 255, 255, 0.05) inset;
    color: #f4f4f5;
  }

  /* When Svelte Flow is dragging this node, it automatically adds a 'dragging' class to the wrapper, 
     but we can also use global styles to adjust our shadow if needed. For now, the default card looks good. */

  .node-header {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 12px 14px;
    background: rgba(39, 39, 42, 0.5);
    border-bottom: 1px solid rgba(255, 255, 255, 0.05);
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    cursor: grab;
    user-select: none;
  }

  .node-header:active {
    cursor: grabbing;
  }

  .status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    display: inline-block;
    animation: statusPulse 2s infinite;
  }

  @keyframes statusPulse {
    0% { opacity: 0.6; }
    50% { opacity: 1; }
    100% { opacity: 0.6; }
  }

  .node-title {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: #e4e4e7;
  }

  .node-content {
    padding: 16px;
    display: flex;
    flex-direction: column;
    gap: 12px;
  }

  :global(.custom-handle) {
    width: 8px !important;
    height: 8px !important;
    background: #3f3f46 !important;
    border: 2px solid #18181b !important;
    border-radius: 50% !important;
    transition: all 0.2s ease;
  }
  
  :global(.custom-handle:hover) {
    background: #a1a1aa !important;
    transform: scale(1.5);
  }
</style>
