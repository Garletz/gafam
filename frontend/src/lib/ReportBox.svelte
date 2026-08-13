<script lang="ts">
  import Markdown from './Markdown.svelte';

  let {
    question,
    report,
    verdict = '',
    score = -1,
    initiallyOpen = false
  }: {
    question: string;
    report: string;
    verdict?: string;
    score?: number;
    initiallyOpen?: boolean;
  } = $props();

  let open = $state(initiallyOpen);
</script>

<div class="gift" class:gift--open={open}>
  <button type="button" class="gift__lid" onclick={() => (open = !open)}>
    <span class="gift__icon">{open ? '▾' : '▸'}</span>
    <span class="gift__question">{question}</span>
    {#if verdict}
      <span
        class="gift__badge"
        class:gift__badge--ok={verdict === 'success'}
        class:gift__badge--mid={verdict === 'partial'}
        class:gift__badge--ko={verdict === 'failed'}
      >⚖️ {verdict}{#if score >= 0}&nbsp;{Math.round(score * 100)}%{/if}</span>
    {/if}
    <span class="gift__cta">{open ? 'fermer' : 'ouvrir'}</span>
  </button>
  {#if open}
    <div class="gift__body">
      <Markdown text={report} />
    </div>
  {/if}
</div>

<style>
  .gift {
    border: 2px solid #000;
    border-radius: 10px;
    background: #fff;
    overflow: hidden;
    margin: 0 0 14px;
  }
  .gift__lid {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    padding: 12px 14px;
    border: none;
    background: #000;
    color: #fff;
    cursor: pointer;
    text-align: left;
    font-size: 13px;
  }
  .gift--open .gift__lid {
    border-bottom: 2px solid #000;
  }
  .gift__lid:hover { background: #222; }
  .gift__icon { font-size: 12px; flex-shrink: 0; }
  .gift__question {
    flex: 1;
    min-width: 0;
    font-weight: 600;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .gift__badge {
    flex-shrink: 0;
    font-size: 10px;
    font-weight: 700;
    padding: 2px 8px;
    border-radius: 999px;
    border: 1px solid #fff;
  }
  .gift__badge--ok { background: #fff; color: #000; }
  .gift__badge--mid { background: transparent; color: #fff; }
  .gift__badge--ko { background: transparent; color: #fff; border-style: dashed; }
  .gift__cta {
    flex-shrink: 0;
    font-size: 10px;
    text-transform: uppercase;
    letter-spacing: 0.08em;
    opacity: 0.7;
  }
  .gift__body {
    padding: 14px 16px;
    max-height: 480px;
    overflow-y: auto;
    background: #fff;
  }
</style>
