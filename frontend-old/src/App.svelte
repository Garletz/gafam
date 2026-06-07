<script lang="ts">
  import { onMount } from 'svelte';
  import DeckConfigurator from './components/DeckConfigurator.svelte';
  import PlatformBlock from './components/PlatformBlock.svelte';
  import VpcNode from './components/nodes/VpcNode.svelte';
  import HardwareNode from './components/nodes/HardwareNode.svelte';
  import WebClientNode from './components/nodes/WebClientNode.svelte';
  import FriendNode from './components/nodes/FriendNode.svelte';
  import RecoveryNode from './components/nodes/RecoveryNode.svelte';
  import PortalNode from './components/nodes/PortalNode.svelte';

  const nodeTypes = {
    vpc: VpcNode,
    hardware: HardwareNode,
    client: WebClientNode,
    friend: FriendNode,
    recovery: RecoveryNode,
    portal: PortalNode
  };

  // Platform sizing and perspective values
  let basePerspective = 720;
  let baseRotateX = 21;
  let currentRotateX = 21;
  let targetRotateX = 21;
  let parallaxStrength = 0.08;

  let cameraZoom = 1.0;
  let cameraY = -50; 
  let cameraX = 0;
  let centerWidth = 26;
  let maxCenterWidth = 50;

  let isTuningPanelOpen = false;

  let innerWidth = 1920; // Default fallback

  // --- LocalStorage persistence ---
  const STORAGE_KEY = 'gafam-deck-state-v3';

  let lastSaveTime = 'Never';

  function saveState() {
    try {
      const state = {
        platforms: platforms.map((p: any) => ({
          id: p.id,
          translateX: p.translateX,
          translateY: p.translateY,
          viewport: p.viewport,
          width: p.width,
          nodes: p.nodes.map((n: any) => ({ 
            id: n.id, 
            position: { x: n.position.x, y: n.position.y },
            data: n.data 
          }))
        })),
        cameraZoom: cameraZoom,
        cameraX: cameraX,
        cameraY: cameraY,
        baseRotateX: baseRotateX,
        basePerspective: basePerspective,
        centerWidth: centerWidth
      };
      localStorage.setItem(STORAGE_KEY, JSON.stringify(state));
      lastSaveTime = new Date().toLocaleTimeString();
    } catch (e) { /* silently fail */ }
  }

  function loadState() {
    try {
      const raw = localStorage.getItem(STORAGE_KEY);
      if (!raw) return;
      const state = JSON.parse(raw);
      
      if (state.platforms) {
        for (const saved of state.platforms) {
          const p = platforms.find((pl: any) => pl.id === saved.id);
          if (!p) continue;
          if (saved.translateX != null) p.translateX = saved.translateX;
          if (saved.translateY != null) p.translateY = saved.translateY;
          if (saved.viewport) p.viewport = saved.viewport;
          if (saved.width != null) p.width = saved.width;
          
          if (saved.nodes) {
            p.nodes = p.nodes.map((n: any) => {
              const sn = saved.nodes.find((s: any) => s.id === n.id);
              if (sn && sn.position) {
                return { ...n, position: { x: sn.position.x, y: sn.position.y } };
              }
              return n;
            });
          }
        }
      }
      if (state.cameraZoom !== undefined) cameraZoom = state.cameraZoom;
      if (state.cameraX !== undefined) cameraX = state.cameraX;
      if (state.cameraY !== undefined) cameraY = state.cameraY;
      if (state.baseRotateX !== undefined) {
        baseRotateX = state.baseRotateX;
        targetRotateX = state.baseRotateX;
      }
      if (state.basePerspective !== undefined) basePerspective = state.basePerspective;
      if (state.centerWidth !== undefined) centerWidth = state.centerWidth;
      
    } catch (e) {
      console.warn('Failed to load state:', e);
    }
  }

  let saveTimer: any;
  let isInitialLoadDone = false;
  
  function debouncedSave() {
    if (!isInitialLoadDone) return;
    clearTimeout(saveTimer);
    saveTimer = setTimeout(saveState, 500);
  }

  // Trigger save when any of these global settings change
  $: if (isInitialLoadDone) {
    cameraZoom;
    cameraX;
    cameraY;
    baseRotateX;
    basePerspective;
    centerWidth;
    debouncedSave();
  }

  // 3 Platforms state with Svelte Flow nodes and edges
  let platforms = [
    { 
      id: 'left', 
      name: 'HARDWARE & RECOVERY', 
      left: -100, 
      width: 85, 
      translateX: -59.08,
      translateY: 5,
      viewport: { x: 599.06, y: 293.68, zoom: 0.69 },
      colorTop: 'linear-gradient(155deg, #ffffff 0%, #f4f4f5 40%, #e4e4e7 100%)',
      nodes: [
        { id: 'hw-1', type: 'hardware', position: { x: 993.05, y: 1754.30 }, data: {} },
        { id: 'rec-1', type: 'recovery', position: { x: 1008.71, y: 2135.51 }, data: {} },
        { id: 'portal-left-out', type: 'portal', position: { x: 1402.75, y: 1980.39 }, data: { direction: 'left', label: '→ VPC', color: '#3b82f6' } }
      ],
      edges: [
        { id: 'e-left', source: 'hw-1', sourceHandle: 'bottom', target: 'rec-1', targetHandle: 'top', animated: true, style: 'stroke: #10b981; stroke-width: 2px;' },
        { id: 'e-hw-portal', source: 'hw-1', sourceHandle: 'right', target: 'portal-left-out', targetHandle: 'in', animated: true, style: 'stroke: #3b82f6; stroke-width: 2px;' }
      ]
    },
    { 
      id: 'center', 
      name: 'VPC CLOUD RELAY', 
      left: -13, 
      width: 26, 
      translateX: 0,
      translateY: 0,
      viewport: { x: 108.91, y: 523.77, zoom: 0.67 },
      colorTop: 'linear-gradient(155deg, #ffffff 0%, #f4f4f5 40%, #e4e4e7 100%)',
      nodes: [
        { id: 'portal-center-in-left', type: 'portal', position: { x: -79.95, y: 1753.56 }, data: { direction: 'right', label: 'HW →', color: '#3b82f6' } },
        { id: 'vpc-1', type: 'vpc', position: { x: 67.97, y: 1864.63 }, data: {} },
        { id: 'portal-center-out-right', type: 'portal', position: { x: 449.00, y: 1738.15 }, data: { direction: 'left', label: '→ WEB', color: '#8b5cf6' } }
      ],
      edges: [
        { id: 'e-portal-vpc-in', source: 'portal-center-in-left', sourceHandle: 'out', target: 'vpc-1', targetHandle: 'left', animated: true, style: 'stroke: #3b82f6; stroke-width: 2px;' },
        { id: 'e-vpc-portal-out', source: 'vpc-1', sourceHandle: 'right', target: 'portal-center-out-right', targetHandle: 'in', animated: true, style: 'stroke: #8b5cf6; stroke-width: 2px;' }
      ]
    },
    { 
      id: 'right', 
      name: 'WEB CLIENTS & FEDERATION', 
      left: 15, 
      width: 85, 
      translateX: 30,
      translateY: -7.5,
      viewport: { x: 59.78, y: 61.69, zoom: 0.95 },
      colorTop: 'linear-gradient(155deg, #ffffff 0%, #f4f4f5 40%, #e4e4e7 100%)',
      nodes: [
        { id: 'portal-right-in', type: 'portal', position: { x: -46.12, y: 1652 }, data: { direction: 'right', label: 'VPC →', color: '#8b5cf6' } },
        { id: 'web-1', type: 'client', position: { x: 73.61, y: 1479.77 }, data: {} },
        { id: 'friend-1', type: 'friend', position: { x: 64.44, y: 1743.26 }, data: {} }
      ],
      edges: [
        { id: 'e-portal-web', source: 'portal-right-in', sourceHandle: 'out', target: 'web-1', targetHandle: 'left', animated: true, style: 'stroke: #8b5cf6; stroke-width: 2px;' },
        { id: 'e1', source: 'web-1', sourceHandle: 'bottom', target: 'friend-1', targetHandle: 'top', animated: true, style: 'stroke: #8b5cf6; stroke-width: 2px;' }
      ]
    }
  ];

  // Load state synchronously AFTER platforms array is defined!
  loadState();

  // Reactively calculate max center width to avoid collision
  $: {
    if (innerWidth && platforms.length === 3) {
      const leftRightEdgeVw = platforms[0].left + platforms[0].width + (platforms[0].translateX / innerWidth * 100);
      const rightLeftEdgeVw = platforms[2].left + (platforms[2].translateX / innerWidth * 100);
      const maxHalfWidth = Math.min(-leftRightEdgeVw, rightLeftEdgeVw);
      maxCenterWidth = Math.max(12, Math.floor((maxHalfWidth * 2) - 2));
      
      if (centerWidth > maxCenterWidth) centerWidth = maxCenterWidth;
      
      platforms[1].width = centerWidth;
      platforms[1].left = -(centerWidth / 2);
    }
  }

  $: currentLeftPx = (idx: number) => (platforms[idx].left / 100) * innerWidth + platforms[idx].translateX;
  $: currentRightPx = (idx: number) => currentLeftPx(idx) + (platforms[idx].width / 100) * innerWidth;

  $: getMinX = (idx: number) => {
    let gap = 50; // 50px min gap
    if (idx === 1) return currentRightPx(0) - (platforms[1].left / 100) * innerWidth + gap;
    if (idx === 2) return currentRightPx(1) - (platforms[2].left / 100) * innerWidth + gap;
    return -Infinity;
  };

  $: getMaxX = (idx: number) => {
    let gap = 50;
    if (idx === 0) return currentLeftPx(1) - ((platforms[0].left + platforms[0].width) / 100) * innerWidth - gap;
    if (idx === 1) return currentLeftPx(2) - ((platforms[1].left + platforms[1].width) / 100) * innerWidth - gap;
    return Infinity;
  };

  function handleMouseMove(e: MouseEvent) {
    const ny = (e.clientY / window.innerHeight - 0.5) * 2;
    targetRotateX = baseRotateX - ny * parallaxStrength * 7;
  }

  onMount(() => {
    isInitialLoadDone = true; // allow saving after load
    let rafId: number;
    function tick() {
      currentRotateX += (targetRotateX - currentRotateX) * 0.07;
      rafId = requestAnimationFrame(tick);
    }
    tick();
    return () => cancelAnimationFrame(rafId);
  });
</script>

<svelte:window bind:innerWidth />

<div style="position: fixed; bottom: 10px; left: 10px; z-index: 100000; background: rgba(0,0,0,0.8); color: #10b981; padding: 10px; border-radius: 8px; font-family: monospace; border: 1px solid #10b981;">
  <b>DEBUG AUTOSAVE</b><br>
  Last saved: {lastSaveTime}<br>
  HW-1 pos X: {Math.round(platforms[0].nodes[0].position.x)}<br>
  HW-1 pos Y: {Math.round(platforms[0].nodes[0].position.y)}<br>
  Left Viewport Y: {Math.round(platforms[0].viewport.y)}
</div>

<main class="viewport" on:mousemove={handleMouseMove}>
  <div class="glow-bg"></div>

  <button class="hud-toggle" on:click={() => isTuningPanelOpen = !isTuningPanelOpen}>⚙️ DECK CONFIG</button>

  <DeckConfigurator
    bind:cameraZoom
    bind:cameraX
    bind:cameraY
    bind:baseRotateX
    bind:targetRotateX
    bind:basePerspective
    bind:centerWidth
    {maxCenterWidth}
    bind:isTuningPanelOpen
  />

  <div id="scene" class="scene" style="perspective: {basePerspective}px;">
    <div 
      id="platform-wrapper" 
      class="platform-wrapper" 
      style="transform: translateX({cameraX}px) translateY({cameraY}px) scale({cameraZoom}); transform-style: preserve-3d;"
    >
      
      {#each platforms as platform, i (platform.id)}
        <PlatformBlock
          {platform}
          {currentRotateX}
          bind:nodes={platform.nodes}
          bind:edges={platform.edges}
          bind:translateX={platform.translateX}
          bind:translateY={platform.translateY}
          bind:viewport={platform.viewport}
          bind:width={platform.width}
          minX={getMinX(i)}
          maxX={getMaxX(i)}
          {nodeTypes}
          on:nodedrag={debouncedSave}
          on:platformchange={debouncedSave}
        />
      {/each}



    </div>
  </div>
</main>

<style>
  :global(*) { box-sizing: border-box; }
  .viewport {
    position: relative; width: 100vw; height: 100vh; overflow: hidden;
    background-color: #050505; display: flex; align-items: center; justify-content: center;
    font-family: 'Inter', sans-serif;
  }
  .glow-bg {
    position: absolute; inset: 0; pointer-events: none; z-index: 1;
    background: radial-gradient(circle 800px at 50% 50%, rgba(255, 255, 255, 0.05) 0%, rgba(0, 0, 0, 0) 100%);
  }

  .hud-toggle {
    position: absolute; top: 20px; right: 20px; z-index: 10000;
    background: #18181b; color: white; border: 1px solid rgba(255,255,255,0.2);
    padding: 8px 12px; border-radius: 6px; cursor: pointer; font-size: 11px; font-weight: bold;
  }

  .scene {
    position: relative; z-index: 5; display: flex; align-items: center; justify-content: center;
    width: 100%; height: 100%;
    perspective-origin: 50% 50%;
    pointer-events: none;
  }
  .platform-wrapper {
    position: relative; display: flex; align-items: center; justify-content: center;
    width: 100%; height: 100%;
    transition: transform 0.1s ease-out;
    pointer-events: none;
  }
</style>

