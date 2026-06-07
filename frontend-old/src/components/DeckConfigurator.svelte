<script lang="ts">
  export let cameraZoom: number;
  export let cameraX: number;
  export let cameraY: number;
  export let baseRotateX: number;
  export let targetRotateX: number;
  export let basePerspective: number;
  export let centerWidth: number;
  export let maxCenterWidth: number = 60;
  export let isTuningPanelOpen: boolean;

  function resetCamera() {
    cameraZoom = 1.0;
    cameraY = -50;
    cameraX = 0;
    basePerspective = 720;
    baseRotateX = 21;
    targetRotateX = 21;
  }

  function exportConfig() {
    const raw = localStorage.getItem('gafam-deck-state-v2');
    if (raw) {
      navigator.clipboard.writeText(raw).then(() => {
        alert('Config copiée dans le presse-papier ! Tu peux la coller dans le chat.');
      }).catch(err => {
        console.error('Could not copy', err);
        prompt('Copie ceci et envoie-le dans le chat:', raw);
      });
    } else {
      alert('Aucune config sauvegardée pour le moment.');
    }
  }
</script>

{#if isTuningPanelOpen}
  <div class="camera-panel-card">
    <div class="panel-header">
      <h3>GLOBAL DECK SETTINGS</h3>
      <button on:click={() => isTuningPanelOpen = false}>✕</button>
    </div>
    
    <div class="panel-body">
      <div class="slider-group">
        <div class="slider-labels"><span>ZOOM SCALE</span><span>{cameraZoom.toFixed(2)}x</span></div>
        <input type="range" min="0.4" max="2.5" step="0.02" bind:value={cameraZoom} />
      </div>
      <div class="slider-group">
        <div class="slider-labels"><span>PAN X</span><span>{cameraX}px</span></div>
        <input type="range" min="-1000" max="1000" step="10" bind:value={cameraX} />
      </div>
      <div class="slider-group">
        <div class="slider-labels"><span>CAMERA Y POSITION</span><span>{cameraY}px</span></div>
        <input type="range" min="-800" max="800" step="5" bind:value={cameraY} />
      </div>
      <div class="slider-group">
        <div class="slider-labels"><span>DECK TILT</span><span>{baseRotateX}°</span></div>
        <input type="range" min="10" max="90" step="1" bind:value={baseRotateX} on:input={() => { targetRotateX = baseRotateX; }} />
      </div>
      <div class="slider-group">
        <div class="slider-labels"><span>PERSPECTIVE DEPTH</span><span>{basePerspective}px</span></div>
        <input type="range" min="300" max="1400" step="20" bind:value={basePerspective} />
      </div>
      <hr style="border-color: rgba(255,255,255,0.1); margin: 10px 0;">
      <div class="slider-group">
        <div class="slider-labels"><span>CENTER PLATFORM WIDTH</span><span>{centerWidth}vw</span></div>
        <input type="range" min="10" max={maxCenterWidth} step="1" bind:value={centerWidth} />
      </div>
      <hr style="border-color: rgba(255,255,255,0.1); margin: 10px 0;">
      <div class="action-buttons">
        <button type="button" class="reset-btn" on:click={resetCamera}>Reset View</button>
        <button type="button" class="export-btn" on:click={exportConfig}>Export Config</button>
      </div>
    </div>
  </div>
{/if}

<style>
  .camera-panel-card {
    position: absolute; top: 60px; right: 20px; width: 300px; z-index: 9999;
    background: rgba(24, 24, 27, 0.95); border: 1px solid rgba(255, 255, 255, 0.1);
    border-radius: 12px; color: white; backdrop-filter: blur(10px);
  }
  .panel-header { display: flex; justify-content: space-between; padding: 12px 16px; border-bottom: 1px solid rgba(255,255,255,0.1); }
  .panel-header h3 { margin: 0; font-size: 12px; }
  .panel-header button { background: none; border: none; color: white; cursor: pointer; }
  .panel-body { padding: 16px; display: flex; flex-direction: column; gap: 12px; }
  .slider-group input { width: 100%; cursor: pointer; }
  .slider-labels { display: flex; justify-content: space-between; font-size: 10px; color: #a1a1aa; margin-bottom: 4px;}
  .action-buttons { display: flex; gap: 8px; }
  .reset-btn { flex: 1; background: #3f3f46; color: white; border: none; padding: 8px; border-radius: 4px; cursor: pointer; font-size: 11px;}
  .export-btn { flex: 1; background: #10b981; color: white; border: none; padding: 8px; border-radius: 4px; cursor: pointer; font-size: 11px; font-weight: bold; }
  .export-btn:hover { background: #059669; }
</style>
