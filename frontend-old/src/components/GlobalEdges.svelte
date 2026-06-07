<script lang="ts">
  export let currentRotateX: number;
  export let platforms: any[];
  export let innerWidth: number;

  const nodeWidth = 280; // approximate width of custom nodes
  const nodeHeight = 150; // approximate height

  // Helper to get absolute center-left or center-right coordinates of a node
  $: getNodeHandlePos = (platformId: string, nodeId: string, handleSide: 'left' | 'right') => {
    const p = platforms.find(p => p.id === platformId);
    if (!p) return null;
    
    // In Svelte 5/4 with reactive arrays, we might need to search the array
    const n = p.nodes.find((n: any) => n.id === nodeId);
    if (!n) return null;

    const platformLeftPx = (0.5 + p.left / 100) * innerWidth + p.translateX;
    const viewport = p.viewport || { x: 0, y: 0, zoom: 1 };
    
    return {
      x: platformLeftPx + (n.position.x * viewport.zoom + viewport.x) + (handleSide === 'right' ? nodeWidth * viewport.zoom : 0),
      y: (n.position.y * viewport.zoom + viewport.y) + (nodeHeight * viewport.zoom) / 2
    };
  };

  // Build the lines dynamically
  $: lines = [
    // VPC to Hardware (Left)
    {
      from: getNodeHandlePos('center', 'vpc-1', 'left'),
      to: getNodeHandlePos('left', 'hw-1', 'right'),
      color: '#3b82f6' // Blue
    },
    // VPC to Web Client (Right)
    {
      from: getNodeHandlePos('center', 'vpc-1', 'right'),
      to: getNodeHandlePos('right', 'web-1', 'left'),
      color: '#8b5cf6' // Purple
    }
  ].filter(l => l.from && l.to);

  // Generate cubic bezier path for a "hanging cable" effect
  function getBezierPath(from: any, to: any) {
    const distance = Math.abs(to.x - from.x);
    // The sag creates a gravity effect, dipping below the shortest path
    const sag = Math.max(80, distance * 0.25);
    return `M ${from.x},${from.y} C ${from.x + distance * 0.2},${from.y + sag} ${to.x - distance * 0.2},${to.y + sag} ${to.x},${to.y}`;
  }
</script>

<div class="global-edges-container" style="transform: rotateX({currentRotateX}deg);">
  <svg class="global-edges-svg">
    <defs>
      <filter id="glow">
        <feGaussianBlur stdDeviation="3" result="coloredBlur"/>
        <feMerge>
          <feMergeNode in="coloredBlur"/>
          <feMergeNode in="SourceGraphic"/>
        </feMerge>
      </filter>
    </defs>

    {#each lines as line}
      <path 
        d={getBezierPath(line.from, line.to)}
        class="global-edge-path"
        style="stroke: {line.color};"
        filter="url(#glow)"
      />
    {/each}
  </svg>
</div>

<style>
  .global-edges-container {
    position: absolute;
    bottom: 0;
    left: 0;
    width: 100vw;
    height: 120vh;
    z-index: 1; /* Below the Svelte Flow nodes (z-index 5) but above the platform base */
    transform-origin: center bottom;
    pointer-events: none;
    transform-style: preserve-3d;
  }
  .global-edges-svg {
    width: 100%;
    height: 100%;
    overflow: visible;
  }
  .global-edge-path {
    fill: none;
    stroke-width: 3px;
    stroke-linecap: round;
    stroke-linejoin: round;
    stroke-dasharray: 4, 12;
    animation: dash 1s linear infinite;
    opacity: 0.9;
    box-shadow: 0 0 10px currentColor; /* will work vaguely as glow if not filtered */
  }
  @keyframes dash {
    to {
      stroke-dashoffset: -16;
    }
  }
</style>
