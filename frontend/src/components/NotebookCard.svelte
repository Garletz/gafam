<script lang="ts">
  export let noteX: number;
  export let noteY: number;
  export let activeDragType: string | null;
  export let startDrag: (e: MouseEvent, type: 'card' | 'note' | 'phone') => void;
  export let noteContent: string;
  export let saveStatus: string;
  export let handleNoteInput: () => void;
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div 
  id="notebook-card" 
  class="notebook-sheet {activeDragType === 'note' ? 'dragging' : ''}"
  style="left: {noteX}%; top: {noteY}%; z-index: {activeDragType === 'note' ? 999 : 10};"
  on:mousedown={(e) => startDrag(e, 'note')}
>
  <!-- Notebook Spiral Binding Header -->
  <div class="notebook-header">
    <div class="notebook-header-left">
      <div class="notebook-spiral-ring"></div>
      <div class="notebook-spiral-ring"></div>
      <div class="notebook-spiral-ring"></div>
      <div class="notebook-spiral-ring"></div>
      <span>GAFAM PERSISTENT NOTES</span>
    </div>
    
    <button 
      type="button" 
      class="note-move-btn" 
      aria-label="Grab Notepad"
      title="Hold and drag note across platform"
    >
      <svg viewBox="0 0 36 36" width="10" height="10" fill="currentColor">
        <rect x="13" y="4" width="10" height="9" rx="2" />
        <rect x="13" y="23" width="10" height="9" rx="2" />
        <rect x="4" y="13" width="9" height="10" rx="2" />
        <rect x="23" y="13" width="9" height="10" rx="2" />
      </svg>
    </button>
  </div>

  <!-- Notebook Lined Body -->
  <div class="notebook-paper-body">
    <textarea 
      class="notebook-textarea"
      placeholder="Take some notes here... autosaves to SQLite."
      bind:value={noteContent}
      on:input={handleNoteInput}
    ></textarea>
  </div>

  <!-- Notebook Footer -->
  <div class="notebook-footer">
    <span>SQLite Sync Active</span>
    <div class="save-status-indicator {saveStatus === 'Saving...' ? 'saving' : 'saved'}">
      <span class="save-dot"></span>
      <span>{saveStatus}</span>
    </div>
  </div>
</div>

<style>
  /* ULTRA-REALISTIC LINED NOTEBOOK SHEET */
  .notebook-sheet {
    position: absolute;
    width: 320px;
    background: #fafaf7; /* soft ivory paper */
    border-radius: 8px;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    cursor: default;
    user-select: none;
    transition: transform 0.15s cubic-bezier(0.1, 0.8, 0.2, 1), box-shadow 0.15s ease;
    
    /* Paper stack depth extrusion and realistic tactile drop shadows */
    box-shadow:
      0 1px 1px rgba(0,0,0,0.08),
      0 10px 0 #e6e5dd, /* stack depth sheet 2 */
      0 11px 1px rgba(0,0,0,0.06),
      0 14px 24px rgba(67, 90, 110, 0.12),
      0 0 0 1px rgba(0, 0, 0, 0.06) inset;
  }

  .notebook-sheet.dragging {
    cursor: grabbing;
    transform: translateY(3px);
    box-shadow:
      0 1px 1px rgba(0,0,0,0.08),
      0 6px 0 #e6e5dd,
      0 7px 1px rgba(0,0,0,0.06),
      0 10px 16px rgba(67, 90, 110, 0.15);
  }

  /* Top spiral binder head */
  .notebook-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 10px 14px;
    background: #eaeae0; /* slightly darker top binder strip */
    border-bottom: 2px solid #dcdcd0;
    font-size: 10px;
    font-weight: 800;
    color: #6d6d63;
    text-transform: uppercase;
    letter-spacing: 0.08em;
  }

  .notebook-header-left {
    display: flex;
    align-items: center;
    gap: 8px;
  }

  /* Steel binder rings loops */
  .notebook-spiral-ring {
    width: 6px;
    height: 14px;
    background: linear-gradient(90deg, #e5e7eb 0%, #9ca3af 50%, #4b5563 100%);
    border-radius: 3px;
    box-shadow: 0 1px 2px rgba(0,0,0,0.22);
  }

  .note-move-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 22px;
    height: 22px;
    padding: 0;
    border-radius: 4px;
    background: #ffffff;
    border: 1px solid rgba(207, 216, 220, 0.8);
    color: #6d6d63;
    cursor: grab;
    transition: all 0.2s;
  }

  .note-move-btn:hover {
    background: #ffffff;
    color: #111;
    border-color: #90a4ae;
  }

  /* Paper lined body container */
  .notebook-paper-body {
    position: relative;
    padding: 0;
    background: #fafaf7;
    display: flex;
    flex-direction: column;
  }

  /* Notebook left red margin line */
  .notebook-paper-body::before {
    content: '';
    position: absolute;
    top: 0;
    bottom: 0;
    left: 42px;
    width: 1px;
    background: rgba(239, 68, 68, 0.4);
    pointer-events: none;
    z-index: 2;
  }

  /* Interactive textarea mapped precisely to paper rules */
  .notebook-textarea {
    font-family: 'Outfit', 'Inter', sans-serif;
    font-size: 13px;
    line-height: 24px;
    padding: 12px 14px 12px 54px; /* offset precisely past red line */
    border: none;
    outline: none;
    resize: none;
    width: 100%;
    height: 264px;
    background: 
      linear-gradient(rgba(176, 190, 197, 0.35) 1px, transparent 1px) 0 0 / 100% 24px,
      #fafaf7;
    color: #2c3e50;
    font-weight: 550;
    box-sizing: border-box;
    pointer-events: auto; /* Active cursor for writing notes */
  }

  .notebook-footer {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 14px;
    background: #fafaf7;
    border-top: 1px solid rgba(226, 232, 240, 0.5);
    font-size: 9px;
    font-weight: 800;
    color: #94a3b8;
  }

  .save-status-indicator {
    display: flex;
    align-items: center;
    gap: 4px;
    text-transform: uppercase;
    font-size: 8px;
    letter-spacing: 0.05em;
  }

  .save-status-indicator.saving {
    color: #f59e0b;
  }

  .save-status-indicator.saved {
    color: #10b981;
  }

  .save-dot {
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background-color: currentColor;
  }
</style>
