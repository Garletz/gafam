<script lang="ts">
  import { renderMarkdown } from './markdown';

  let { text = '' }: { text: string } = $props();
  let html = $derived(renderMarkdown(text));
</script>

<div class="md">{@html html}</div>

<style>
  .md {
    font-size: 14px;
    line-height: 1.65;
    color: #202124;
    word-wrap: break-word;
  }
  .md :global(h1) { font-size: 20px; margin: 14px 0 8px; color: #000; }
  .md :global(h2) { font-size: 17px; margin: 12px 0 6px; color: #000; }
  .md :global(h3) { font-size: 15px; margin: 10px 0 5px; color: #000; }
  .md :global(h4) { font-size: 14px; margin: 8px 0 4px; color: #000; }
  .md :global(p) { margin: 6px 0; }
  .md :global(strong) { color: #000; }
  .md :global(code) {
    font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
    font-size: 12px;
    background: #f1f3f4;
    padding: 1px 5px;
    border-radius: 4px;
  }
  .md :global(pre) {
    background: #0a0a0a;
    color: #f5f5f5;
    border-radius: 8px;
    padding: 12px;
    overflow-x: auto;
    margin: 8px 0;
  }
  .md :global(pre code) { background: none; padding: 0; color: inherit; }
  .md :global(ul), .md :global(ol) { margin: 6px 0; padding-left: 22px; }
  .md :global(li) { margin: 2px 0; }
  .md :global(table) {
    border-collapse: collapse;
    margin: 8px 0;
    width: 100%;
    font-size: 13px;
  }
  .md :global(th), .md :global(td) {
    border: 1px solid #dfe1e5;
    padding: 6px 10px;
    text-align: left;
  }
  .md :global(th) { background: #f8f9fa; color: #000; font-weight: 700; }
  .md :global(hr) { border: none; border-top: 1px solid #dfe1e5; margin: 12px 0; }
  .md :global(a) { color: #000; text-decoration: underline; }
</style>
