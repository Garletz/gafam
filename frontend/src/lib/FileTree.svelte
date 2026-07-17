<script lang="ts">
  import FileTreeNode, { type TreeNode } from './FileTreeNode.svelte';

  let {
    vpcUrl = '',
    sessionToken = '',
    running = false,
    selectedPath = '',
    onselect = (_: TreeNode) => {},
    ondownload = (_: TreeNode) => {},
    ondelete = (_: TreeNode) => {},
    ondropfiles = (_: FileList) => {}
  }: {
    vpcUrl: string;
    sessionToken: string;
    running?: boolean;
    selectedPath?: string;
    onselect?: (n: TreeNode) => void;
    ondownload?: (n: TreeNode) => void;
    ondelete?: (n: TreeNode) => void;
    ondropfiles?: (files: FileList) => void;
  } = $props();

  let root: TreeNode | null = $state(null);
  let truncated = $state(false);
  let loading = $state(false);
  let error = $state('');
  let viewMode: 'tree' | 'agent' = $state('tree');
  let agentAscii = $state('');
  let copied = $state(false);
  let dragOver = $state(false);

  export async function refresh() {
    if (!vpcUrl || !sessionToken || !running) return;
    loading = true;
    error = '';
    try {
      const params = new URLSearchParams({
        vpcUrl, token: sessionToken, action: 'tree', path: '/', depth: '6', format: 'json'
      });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      const data: any = await res.json();
      if (res.ok && data.root) {
        root = data.root;
        truncated = !!data.truncated;
      } else {
        error = data.error || 'tree failed';
      }
    } catch (e: any) {
      error = e.message || 'network error';
    } finally {
      loading = false;
    }
  }

  async function loadAgentView() {
    if (!vpcUrl || !sessionToken) return;
    try {
      const params = new URLSearchParams({
        vpcUrl, token: sessionToken, action: 'tree', path: '/', depth: '6', format: 'ascii'
      });
      const res = await fetch(`/api/proxy/sandbox?${params.toString()}`);
      const data: any = await res.json();
      if (res.ok && data.ascii) agentAscii = data.ascii;
    } catch {}
  }

  async function toggleMode() {
    viewMode = viewMode === 'tree' ? 'agent' : 'tree';
    if (viewMode === 'agent' && !agentAscii) await loadAgentView();
    if (viewMode === 'agent') await loadAgentView();
  }

  async function copyAscii() {
    if (!agentAscii) await loadAgentView();
    try {
      await navigator.clipboard.writeText(agentAscii);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch {}
  }

  function handleDrop(e: DragEvent) {
    e.preventDefault();
    dragOver = false;
    if (e.dataTransfer?.files?.length) ondropfiles(e.dataTransfer.files);
  }
</script>

<div class="ftree">
  <div class="ftree__toolbar">
    <span class="ftree__title">Filesystem</span>
    <div class="ftree__tools">
      <button
        class="ftree__tool"
        class:active={viewMode === 'agent'}
        title="Agent view — what an agent sees in one call (sandbox.tree ascii)"
        onclick={toggleMode}
      >ascii</button>
      <button class="ftree__tool" title="Refresh" onclick={refresh} disabled={loading}>
        {loading ? '…' : '↻'}
      </button>
    </div>
  </div>

  {#if error}
    <div class="ftree__error">{error}</div>
  {/if}

  {#if viewMode === 'agent'}
    <div class="ftree__agent">
      <div class="ftree__agent-bar">
        <span>sandbox.tree · format=ascii</span>
        <button class="ftree__tool" onclick={copyAscii}>{copied ? '✓ copied' : 'copy'}</button>
      </div>
      <pre class="ftree__ascii">{agentAscii || 'Loading…'}</pre>
    </div>
  {:else}
    <div
      class="ftree__body"
      class:dragover={dragOver}
      ondragover={(e) => { e.preventDefault(); dragOver = true; }}
      ondragleave={() => { dragOver = false; }}
      ondrop={handleDrop}
      role="tree"
    >
      {#if !root}
        <div class="ftree__empty">{loading ? 'Loading tree…' : running ? 'No tree yet — hit ↻' : 'Wake the sandbox to see files'}</div>
      {:else}
        <div class="ftree__root-name">/</div>
        {#each root.children ?? [] as child, i}
          <FileTreeNode
            node={child}
            prefix=""
            isLast={i === (root.children?.length ?? 1) - 1}
            {selectedPath}
            {onselect}
            {ondownload}
            {ondelete}
          />
        {/each}
        {#if truncated}
          <div class="ftree__trunc">… truncated (2000 entries cap)</div>
        {/if}
      {/if}
    </div>
  {/if}
</div>

<style>
  .ftree { display: flex; flex-direction: column; flex: 1; min-height: 0; }

  .ftree__toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    border-bottom: 1px solid #f1f3f4;
    flex-shrink: 0;
  }
  .ftree__title {
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.04em;
    color: #80868b;
  }
  .ftree__tools { display: flex; gap: 4px; }
  .ftree__tool {
    border: 1px solid #dfe1e5;
    background: #fff;
    border-radius: 3px;
    font-size: 11px;
    font-family: 'SF Mono', Menlo, monospace;
    color: #5f6368;
    cursor: pointer;
    padding: 2px 7px;
  }
  .ftree__tool:hover { background: #f1f3f4; }
  .ftree__tool.active { background: #202124; color: #fff; border-color: #202124; }
  .ftree__tool:disabled { opacity: 0.5; cursor: default; }

  .ftree__error { padding: 4px 12px; font-size: 11px; color: #d93025; }

  .ftree__body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 6px 8px;
    border: 2px dashed transparent;
  }
  .ftree__body.dragover { border-color: #202124; background: #f8f9fa; }

  .ftree__root-name {
    font-family: 'SF Mono', Menlo, monospace;
    font-size: 12px;
    font-weight: 700;
    color: #202124;
    padding: 1px 0 3px;
  }

  .ftree__empty {
    padding: 24px 12px;
    text-align: center;
    color: #9aa0a6;
    font-size: 12px;
  }
  .ftree__trunc {
    padding: 6px 4px;
    font-size: 10px;
    color: #9aa0a6;
    font-style: italic;
  }

  .ftree__agent { flex: 1; min-height: 0; display: flex; flex-direction: column; }
  .ftree__agent-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 12px;
    font-size: 10px;
    font-family: 'SF Mono', Menlo, monospace;
    color: #9aa0a6;
    border-bottom: 1px solid #f1f3f4;
  }
  .ftree__ascii {
    flex: 1;
    min-height: 0;
    overflow: auto;
    margin: 0;
    padding: 8px 12px;
    background: #fafbfc;
    font-family: 'SF Mono', Menlo, monospace;
    font-size: 11.5px;
    line-height: 1.45;
    color: #202124;
    white-space: pre;
  }
</style>
