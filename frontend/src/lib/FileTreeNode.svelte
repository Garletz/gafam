<script lang="ts" module>
  export type TreeNode = {
    name: string;
    path: string;
    type: 'dir' | 'file';
    size: number;
    modified: number;
    children?: TreeNode[];
  };
</script>

<script lang="ts">
  import FileTreeNode from './FileTreeNode.svelte';

  let {
    node,
    prefix = '',
    isLast = true,
    isRoot = false,
    selectedPath = '',
    onselect = (_: TreeNode) => {},
    ondownload = (_: TreeNode) => {},
    ondelete = (_: TreeNode) => {}
  }: {
    node: TreeNode;
    prefix?: string;
    isLast?: boolean;
    isRoot?: boolean;
    selectedPath?: string;
    onselect?: (n: TreeNode) => void;
    ondownload?: (n: TreeNode) => void;
    ondelete?: (n: TreeNode) => void;
  } = $props();

  let open = $state(true);

  function icon(n: TreeNode): string {
    if (n.type === 'dir') return open ? '▾' : '▸';
    const ext = n.name.split('.').pop()?.toLowerCase() || '';
    if (['png', 'jpg', 'jpeg', 'gif', 'svg', 'webp'].includes(ext)) return '◈';
    if (['sh', 'py', 'js', 'ts'].includes(ext)) return '⚙';
    if (['zip', 'tar', 'gz', 'tgz'].includes(ext)) return '▣';
    if (['md', 'txt', 'log'].includes(ext)) return '✎';
    return '·';
  }

  function formatSize(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
    return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  }

  const childPrefix = $derived(
    isRoot ? '' : prefix + (isLast ? '    ' : '│   ')
  );
</script>

<div class="tnode">
  <div
    class="tnode__row"
    class:selected={selectedPath === node.path}
    class:dir={node.type === 'dir'}
  >
    <span class="tnode__guides">{isRoot ? '' : prefix + (isLast ? '└── ' : '├── ')}</span>
    <button
      class="tnode__main"
      onclick={() => {
        if (node.type === 'dir') { open = !open; }
        onselect(node);
      }}
    >
      <span class="tnode__icon" class:diricon={node.type === 'dir'}>{icon(node)}</span>
      <span class="tnode__name">{node.name}{node.type === 'dir' ? '/' : ''}</span>
    </button>
    {#if node.type === 'file'}
      <span class="tnode__size">{formatSize(node.size)}</span>
      <button class="tnode__action" title="Download" onclick={() => ondownload(node)}>⬇</button>
      <button class="tnode__action tnode__action--del" title="Delete" onclick={() => ondelete(node)}>✕</button>
    {/if}
  </div>

  {#if node.type === 'dir' && open && node.children}
    {#each node.children as child, i}
      <FileTreeNode
        node={child}
        prefix={childPrefix}
        isLast={i === (node.children?.length ?? 1) - 1}
        {selectedPath}
        {onselect}
        {ondownload}
        {ondelete}
      />
    {/each}
    {#if node.children.length === 0}
      <div class="tnode__empty">
        <span class="tnode__guides">{childPrefix}└── </span><span class="tnode__empty-label">(empty)</span>
      </div>
    {/if}
  {/if}
</div>

<style>
  .tnode { user-select: none; }
  .tnode__row {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 1px 6px 1px 0;
    border-radius: 3px;
    white-space: nowrap;
  }
  .tnode__row:hover { background: #f1f3f4; }
  .tnode__row.selected { background: #e8f0fe; }
  .tnode__row.selected .tnode__name { color: #1a73e8; font-weight: 600; }

  .tnode__guides {
    font-family: 'SF Mono', Menlo, monospace;
    font-size: 12px;
    color: #bdc1c6;
    flex-shrink: 0;
    letter-spacing: 0;
  }

  .tnode__main {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 5px;
    border: none;
    background: transparent;
    cursor: pointer;
    text-align: left;
    padding: 2px 0;
  }
  .tnode__icon {
    flex-shrink: 0;
    font-size: 11px;
    color: #80868b;
    width: 14px;
    text-align: center;
  }
  .tnode__icon.diricon { color: #f9ab00; font-size: 10px; }
  .tnode__name {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: 12px;
    font-family: 'SF Mono', Menlo, monospace;
    color: #202124;
  }
  .tnode__row.dir .tnode__name { color: #5f6368; font-weight: 600; }
  .tnode__row.selected .tnode__name { color: #1a73e8; }

  .tnode__size {
    flex-shrink: 0;
    font-size: 10px;
    color: #9aa0a6;
    font-variant-numeric: tabular-nums;
    font-family: 'SF Mono', Menlo, monospace;
  }
  .tnode__action {
    flex-shrink: 0;
    border: none;
    background: transparent;
    cursor: pointer;
    font-size: 11px;
    color: #5f6368;
    padding: 1px 3px;
    border-radius: 3px;
    opacity: 0;
  }
  .tnode__row:hover .tnode__action { opacity: 1; }
  .tnode__action:hover { background: #e8eaed; }
  .tnode__action--del:hover { color: #d93025; }

  .tnode__empty { display: flex; padding: 1px 0; }
  .tnode__empty-label {
    font-size: 11px;
    color: #bdc1c6;
    font-family: 'SF Mono', Menlo, monospace;
    font-style: italic;
  }
</style>
