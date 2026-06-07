<script lang="ts">
  import { Handle, Position } from '@xyflow/svelte';
  export let data: any = {};

  // Direction: 'left' or 'right' — determines which side the handle appears
  $: direction = data.direction || 'right';
  $: label = data.label || 'PORTAL';
  $: color = data.color || '#3b82f6';
</script>

<div class="portal-node" style="--portal-color: {color};">
  {#if direction === 'left' || direction === 'both'}
    <Handle type="target" position={Position.Left} id="in" class="portal-handle" />
  {/if}

  <div class="portal-slit">
    <div class="portal-glow"></div>
    <div class="portal-core"></div>
  </div>

  <span class="portal-label">{label}</span>

  {#if direction === 'right' || direction === 'both'}
    <Handle type="source" position={Position.Right} id="out" class="portal-handle" />
  {/if}

  <!-- Also add top/bottom handles for flexibility -->
  <Handle type="target" position={Position.Top} id="top" class="portal-handle" />
  <Handle type="source" position={Position.Bottom} id="bottom" class="portal-handle" />
</div>

<style>
  .portal-node {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    min-width: 60px;
    cursor: grab;
    user-select: none;
  }

  .portal-slit {
    position: relative;
    width: 12px;
    height: 64px;
    border-radius: 6px;
    background: rgba(0, 0, 0, 0.6);
    border: 1px solid var(--portal-color);
    overflow: hidden;
    box-shadow:
      0 0 12px var(--portal-color),
      0 0 24px color-mix(in srgb, var(--portal-color) 40%, transparent),
      inset 0 0 8px var(--portal-color);
  }

  .portal-glow {
    position: absolute;
    inset: -4px;
    border-radius: 10px;
    background: radial-gradient(
      ellipse at center,
      color-mix(in srgb, var(--portal-color) 30%, transparent) 0%,
      transparent 70%
    );
    animation: pulse 2s ease-in-out infinite;
  }

  .portal-core {
    position: absolute;
    top: 10%;
    left: 20%;
    width: 60%;
    height: 80%;
    border-radius: 3px;
    background: linear-gradient(
      to bottom,
      transparent 0%,
      var(--portal-color) 20%,
      white 50%,
      var(--portal-color) 80%,
      transparent 100%
    );
    opacity: 0.8;
    animation: shimmer 1.5s ease-in-out infinite alternate;
  }

  .portal-label {
    font-size: 8px;
    font-weight: 800;
    letter-spacing: 2px;
    text-transform: uppercase;
    color: var(--portal-color);
    text-shadow: 0 0 6px var(--portal-color);
    white-space: nowrap;
  }

  @keyframes pulse {
    0%, 100% { opacity: 0.5; }
    50% { opacity: 1; }
  }

  @keyframes shimmer {
    0% { opacity: 0.5; transform: scaleY(0.95); }
    100% { opacity: 0.9; transform: scaleY(1.05); }
  }

  :global(.portal-handle) {
    width: 6px !important;
    height: 6px !important;
    background: var(--portal-color, #3b82f6) !important;
    border: 1px solid rgba(0,0,0,0.4) !important;
    border-radius: 50% !important;
  }
</style>
