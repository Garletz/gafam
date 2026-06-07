<script lang="ts">
  import { SvelteFlow, SvelteFlowProvider, Background, BackgroundVariant } from '@xyflow/svelte';
  import '@xyflow/svelte/dist/style.css';
  import Moveable from 'svelte-moveable';
  import { createEventDispatcher, onDestroy } from 'svelte';

  const dispatch = createEventDispatcher();

  export let platform: any;
  export let currentRotateX: number;
  export let nodes: any[] = [];
  export let edges: any[] = [];
  export let nodeTypes: any = {};
  
  export let translateX = 0;
  export let translateY = 0;
  export let minX = -Infinity;
  export let maxX = Infinity;
  export let width = 20;

  export let viewport = { x: 0, y: 0, zoom: 1 };

  $: isCenter = platform.id === 'center';

  let targetRef: HTMLElement;
  let dragHandle: HTMLElement;
  
  function handleDrag({ detail }: any) {
    if (isCenter) return;
    let x = Math.max(minX, Math.min(maxX, detail.beforeTranslate[0]));
    let y = detail.beforeTranslate[1];
    translateX = x;
    translateY = y;
    targetRef.style.transform = `translate(${translateX}px, ${translateY}px)`;
    dispatch('platformchange');
  }
</script>

<div 
  class="platform"
  bind:this={targetRef}
  style="left: 50%; margin-left: {platform.left}vw; width: {width}vw; transform-style: preserve-3d;"
>
  <!-- Moveable only for left/right platforms (drag) -->
  {#if !isCenter}
    <Moveable
      target={targetRef}
      draggable={true}
      on:drag={handleDrag}
      snappable={true}
      dragTarget={dragHandle}
    />
  {/if}

  <!-- Top Surface (The inclined plane) -->
  <div 
    class="platform__top {platform.id === 'left' ? 'inner-right-border' : ''} {platform.id === 'right' ? 'inner-left-border' : ''}"
    style="transform: rotateX({currentRotateX}deg); background: {platform.colorTop};"
  >
    <!-- Inner Borders / Walls -->
    {#if platform.id === 'left'}
      <div class="platform__side right-wall"></div>
    {/if}
    {#if platform.id === 'right'}
      <div class="platform__side left-wall"></div>
    {/if}

    <!-- estrade indicator - positioned on the visible portion for wide platforms -->
    <div class="estrade-indicator" style="{platform.id === 'left' ? 'left: auto; right: 2%; width: 25%;' : platform.id === 'right' ? 'left: 2%; right: auto; width: 25%;' : ''}">
       <div class="estrade-glow"></div>
       <span class="estrade-label">:: {platform.name} ::</span>
    </div>

    <!-- Svelte Flow takes up the full platform surface -->
    <div class="flow-wrapper">
      <SvelteFlowProvider>
        <SvelteFlow
          id={platform.id}
          bind:nodes
          bind:edges
          bind:viewport
          onnodedrag={() => dispatch('nodedrag')}
          onnodedragstop={() => dispatch('nodedrag')}
          {nodeTypes}
          fitView={false}
          panOnDrag={true}
          zoomOnScroll={true}
          nodesDraggable={true}
          elementsSelectable={false}
        >
          <Background variant={BackgroundVariant.Dots} gap={20} size={2} color="rgba(0,0,0,0.1)" />
        </SvelteFlow>
      </SvelteFlowProvider>
    </div>
  </div>

  <!-- Front Edge (The cliff) -->
  {#if isCenter}
    <!-- Center: non-interactive front face -->
    <div class="platform__front"></div>
  {:else}
    <!-- Left/Right: entire front face is the drag handle -->
    <div bind:this={dragHandle} class="platform__front draggable-front">
      <div class="platform-dot blue-dot"></div>
    </div>
  {/if}
</div>

<style>
  .platform {
    position: absolute;
    bottom: 0;
    height: 0;
    z-index: 10;
  }

  .platform__top {
    position: absolute;
    bottom: 0;
    left: 0;
    width: 100%;
    height: 250vh;
    z-index: 2;
    transform-origin: center bottom;
    transition: transform 0.45s cubic-bezier(0.23, 1, 0.32, 1);
    box-shadow:
      inset 0 2px 0 rgba(255, 255, 255, 0.8),
      inset 0 -1px 0 rgba(0, 0, 0, 0.04);
    transform-style: preserve-3d;
  }

  .platform__side {
    position: absolute;
    top: 0;
    width: 100px;
    height: 100%;
    background: linear-gradient(to bottom, #d4d4d8 0%, #a1a1aa 15%, #71717a 40%, #27272a 100%);
    box-shadow: inset 0 0 10px rgba(0,0,0,0.5);
  }

  .right-wall {
    right: -1px;
    transform-origin: right;
    transform: rotateY(-90deg);
  }

  .left-wall {
    left: -1px;
    transform-origin: left;
    transform: rotateY(90deg);
  }

  .platform__front {
    position: absolute;
    top: -1px;
    left: 0;
    width: 100%;
    height: 2500px;
    z-index: 1;
    background: linear-gradient(to bottom, #d4d4d8 0%, #a1a1aa 15%, #71717a 40%, #27272a 100%);
    transform: rotateX(-19deg);
    transform-origin: center top;
    box-shadow:
      inset 0 1px 0 rgba(255, 255, 255, 0.5),
      inset 0 3px 6px rgba(0, 0, 0, 0.05);
    pointer-events: none;
  }

  /* Left/right front face: grabbable */
  .draggable-front {
    pointer-events: auto;
    cursor: grab;
  }
  .draggable-front:active {
    cursor: grabbing;
  }

  /* Container for the green resize dot — positioned at the visible center-bottom of the platform */
  .resize-dot-container {
    position: absolute;
    top: 40px; /* slightly below the front face top edge, in the visible cliff area */
    left: 0;
    width: 100%;
    height: 60px;
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 50;
    pointer-events: none;
  }

  .estrade-indicator {
    position: absolute;
    left: 10%;
    top: 5%;
    width: 80%;
    height: 90%;
    border: 2px dashed rgba(255, 255, 255, 0.1);
    border-radius: 20px;
    display: flex;
    align-items: flex-end;
    justify-content: center;
    padding-bottom: 20px;
    z-index: 0;
    pointer-events: none;
    transform: translateZ(1px);
  }

  .estrade-glow {
    position: absolute;
    inset: 0;
    border-radius: 20px;
    background: radial-gradient(circle at 50% 50%, rgba(255, 255, 255, 0.03) 0%, transparent 60%);
    pointer-events: none;
  }

  .estrade-label {
    color: rgba(0, 0, 0, 0.2);
    font-size: 10px;
    font-weight: 800;
    letter-spacing: 4px;
    text-transform: uppercase;
    user-select: none;
  }

  .inner-right-border {
    border-right: 4px solid rgba(255, 255, 255, 0.4);
    box-shadow: inset -5px 0 15px rgba(0,0,0,0.2), inset -2px 0 5px rgba(255,255,255,0.3);
  }

  .inner-left-border {
    border-left: 4px solid rgba(255, 255, 255, 0.4);
    box-shadow: inset 5px 0 15px rgba(0,0,0,0.2), inset 2px 0 5px rgba(255,255,255,0.3);
  }

  .flow-wrapper {
    position: absolute;
    top: 0;
    left: 0;
    width: 100%;
    height: 100%;
    z-index: 5;
    pointer-events: auto;
  }

  /* Hide all Moveable visual chrome */
  :global(.moveable-control-box .moveable-line),
  :global(.moveable-control-box .moveable-direction),
  :global(.moveable-control-box .moveable-origin) {
    display: none !important;
  }

  /* Custom colored control dots */
  .platform-dot {
    width: 20px;
    height: 20px;
    border-radius: 50%;
    pointer-events: auto;
    transition: transform 0.15s ease, box-shadow 0.15s ease;
    border: 2px solid rgba(255,255,255,0.4);
  }
  .platform-dot:hover {
    transform: scale(1.4);
  }

  .blue-dot {
    position: absolute;
    top: 30px;
    left: 50%;
    transform: translateX(-50%);
    background: #3b82f6;
    box-shadow: 0 0 14px #3b82f6, 0 0 6px #3b82f6;
    cursor: grab;
    pointer-events: none; /* The whole front face is the drag target */
  }
</style>
