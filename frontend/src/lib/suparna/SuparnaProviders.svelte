<script lang="ts">
  import { onMount } from 'svelte';
  import {
    fetchProviders,
    saveProvider,
    deleteProvider,
    fetchEngine,
    setEngine,
    testProvider,
    orchestratorChat,
    type LLMProvider,
    type LLMEngineInfo,
    type LLMChatResult
  } from './providerApi';

  let { vpcUrl, sessionToken }: { vpcUrl: string; sessionToken: string } = $props();

  const PRESETS = [
    { label: 'Moonshot / Kimi', base_url: 'https://api.moonshot.ai/v1', model: 'kimi-k2-0711-preview' },
    { label: 'OpenAI', base_url: 'https://api.openai.com/v1', model: 'gpt-4o-mini' },
    { label: 'OpenRouter', base_url: 'https://openrouter.ai/api/v1', model: '' },
    { label: 'Custom', base_url: '', model: '' }
  ];

  let providers: LLMProvider[] = $state([]);
  let engineInfo: LLMEngineInfo | null = $state(null);
  let busy = $state(false);
  let msg = $state('');
  let msgError = $state(false);

  // Add-provider form
  let preset = $state(PRESETS[0].label);
  let formName = $state('');
  let formBaseUrl = $state(PRESETS[0].base_url);
  let formModel = $state(PRESETS[0].model);
  let formKey = $state('');

  // Per-provider test state
  let testResults: Record<string, string> = $state({});
  let testingId = $state('');

  // Quick chat
  let chatPrompt = $state('');
  let chatRunning = $state(false);
  let chatResult: LLMChatResult | null = $state(null);

  function flash(text: string, isError = false) {
    msg = text;
    msgError = isError;
    setTimeout(() => { if (msg === text) msg = ''; }, 5000);
  }

  async function refresh() {
    const [p, e] = await Promise.all([
      fetchProviders(vpcUrl, sessionToken),
      fetchEngine(vpcUrl, sessionToken)
    ]);
    providers = p;
    engineInfo = e;
  }

  function applyPreset(label: string) {
    preset = label;
    const p = PRESETS.find((x) => x.label === label);
    if (p) {
      if (p.base_url) formBaseUrl = p.base_url;
      if (p.model) formModel = p.model;
      if (label !== 'Custom' && !formName) formName = label;
    }
  }

  async function addProvider() {
    if (busy) return;
    busy = true;
    const r = await saveProvider(vpcUrl, sessionToken, {
      name: formName.trim() || preset,
      base_url: formBaseUrl.trim(),
      model: formModel.trim(),
      api_key: formKey.trim(),
      enabled: true
    });
    busy = false;
    if (!r.ok) { flash(r.error || 'Save failed', true); return; }
    formKey = '';
    formName = '';
    flash('Provider added');
    await refresh();
  }

  async function toggleEnabled(p: LLMProvider) {
    const r = await saveProvider(vpcUrl, sessionToken, { ...p, enabled: !p.enabled });
    if (!r.ok) { flash(r.error || 'Update failed', true); return; }
    await refresh();
  }

  async function removeProvider(p: LLMProvider) {
    if (!confirm(`Delete provider "${p.name}"?`)) return;
    const r = await deleteProvider(vpcUrl, sessionToken, p.id);
    if (!r.ok) { flash(r.error || 'Delete failed', true); return; }
    flash('Provider deleted');
    await refresh();
  }

  async function runTest(p: LLMProvider) {
    testingId = p.id;
    testResults = { ...testResults, [p.id]: '…' };
    const r = await testProvider(vpcUrl, sessionToken, p.id);
    testingId = '';
    testResults = {
      ...testResults,
      [p.id]: r.ok ? `✓ ${r.reply} (${r.latency_ms} ms)` : `✗ ${r.error}`
    };
  }

  async function pickEngine(engine: string) {
    if (engineInfo?.engine === engine) return;
    const r = await setEngine(vpcUrl, sessionToken, engine);
    if (!r.ok) { flash(r.error || 'Set engine failed', true); return; }
    flash('Orchestration engine updated');
    await refresh();
  }

  async function runChat() {
    if (!chatPrompt.trim() || chatRunning) return;
    chatRunning = true;
    chatResult = null;
    const r = await orchestratorChat(vpcUrl, sessionToken, chatPrompt.trim());
    chatRunning = false;
    chatResult = r;
  }

  onMount(refresh);
</script>

<section class="panel">
  <div class="panel__intro">
    <p>
      External API providers (Kimi, OpenAI, OpenRouter…) + the <strong>orchestration engine</strong> selector.
      Whatever is selected here is what <strong>Kāraka</strong>, quests (<code>llm.chat</code>) and future lucioles will use to think.
    </p>
  </div>

  <!-- ─── Engine selector ─── -->
  <h3 class="section-title">Orchestration engine</h3>
  <div class="engine-grid">
    {#each engineInfo?.available ?? [{ engine: 'vpc', label: 'VPC Qwen (L1)' }, { engine: 'phone', label: 'Phone Edge (L2)' }] as opt}
      <button
        type="button"
        class="engine-card"
        class:is-active={engineInfo?.engine === opt.engine}
        onclick={() => pickEngine(opt.engine)}
      >
        <span class="engine-card__radio">{engineInfo?.engine === opt.engine ? '●' : '○'}</span>
        <span class="engine-card__label">{opt.label}</span>
        {#if engineInfo?.engine === opt.engine}
          <span class="engine-card__tag">orchestrator</span>
        {/if}
      </button>
    {/each}
  </div>

  <!-- ─── Quick chat tester ─── -->
  <div class="chat-tester">
    <input
      type="text"
      placeholder="Test the active engine — e.g. Say hello in one sentence"
      bind:value={chatPrompt}
      disabled={chatRunning}
      onkeydown={(e) => e.key === 'Enter' && runChat()}
    />
    <button type="button" class="btn btn-primary" disabled={chatRunning || !chatPrompt.trim()} onclick={runChat}>
      {chatRunning ? '…' : 'Run'}
    </button>
  </div>
  {#if chatResult}
    <div class="chat-result" class:is-error={!!chatResult.error}>
      {#if chatResult.error}
        <p class="chat-result__error">{chatResult.error}</p>
      {:else}
        <p class="chat-result__content">{chatResult.content}</p>
        <footer class="chat-result__meta">{chatResult.engine} · {chatResult.model} · {chatResult.latency_ms} ms</footer>
      {/if}
    </div>
  {/if}

  <!-- ─── Providers list ─── -->
  <h3 class="section-title">API providers</h3>
  {#if providers.length === 0}
    <p class="empty">No provider yet — add one below (Moonshot/Kimi, OpenAI, OpenRouter…).</p>
  {:else}
    <div class="provider-list">
      {#each providers as p}
        <div class="provider-card" class:is-off={!p.enabled}>
          <div class="provider-card__main">
            <div class="provider-card__head">
              <span class="provider-card__name">{p.name}</span>
              <span class="provider-card__model">{p.model}</span>
            </div>
            <div class="provider-card__sub">
              <span class="mono">{p.base_url}</span>
              <span class="mono key">{p.key_hint || 'no key'}</span>
            </div>
            {#if testResults[p.id]}
              <div class="provider-card__test" class:is-error={testResults[p.id].startsWith('✗')}>
                {testResults[p.id]}
              </div>
            {/if}
          </div>
          <div class="provider-card__actions">
            <button type="button" class="btn btn-ghost btn-sm" disabled={testingId === p.id} onclick={() => runTest(p)}>
              {testingId === p.id ? '…' : 'Test'}
            </button>
            <button type="button" class="btn btn-ghost btn-sm" onclick={() => toggleEnabled(p)}>
              {p.enabled ? 'Disable' : 'Enable'}
            </button>
            <button type="button" class="btn btn-ghost btn-sm btn-danger" onclick={() => removeProvider(p)}>✕</button>
          </div>
        </div>
      {/each}
    </div>
  {/if}

  <!-- ─── Add form ─── -->
  <div class="add-form">
    <h3 class="section-title">Add provider</h3>
    <div class="preset-row">
      {#each PRESETS as p}
        <button
          type="button"
          class="preset-chip"
          class:is-active={preset === p.label}
          onclick={() => applyPreset(p.label)}
        >{p.label}</button>
      {/each}
    </div>
    <div class="form-grid">
      <label>
        <span>Name</span>
        <input type="text" bind:value={formName} placeholder={preset} />
      </label>
      <label>
        <span>Model</span>
        <input type="text" bind:value={formModel} placeholder="kimi-k2-0711-preview" />
      </label>
      <label class="form-grid__wide">
        <span>Base URL (OpenAI-compatible)</span>
        <input type="text" bind:value={formBaseUrl} placeholder="https://api.moonshot.ai/v1" />
      </label>
      <label class="form-grid__wide">
        <span>API key</span>
        <input type="password" bind:value={formKey} placeholder="sk-…" autocomplete="off" />
      </label>
    </div>
    <button
      type="button"
      class="btn btn-primary"
      disabled={busy || !formBaseUrl.trim() || !formModel.trim() || !formKey.trim()}
      onclick={addProvider}
    >
      {busy ? 'Saving…' : '+ Add provider'}
    </button>
  </div>

  {#if msg}
    <p class="flash" class:is-error={msgError}>{msg}</p>
  {/if}
</section>

<style>
  .panel__intro { margin-bottom: 20px; }
  .panel__intro p { margin: 0; font-size: 14px; line-height: 1.5; color: #5f6368; }
  .panel__intro code { font-size: 12px; background: #f1f3f4; padding: 1px 5px; border-radius: 3px; }

  .section-title {
    margin: 18px 0 10px;
    font-size: 11px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #80868b;
  }

  .engine-grid { display: flex; flex-direction: column; gap: 8px; margin-bottom: 14px; }
  .engine-card {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 10px 14px;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    background: #fff;
    cursor: pointer;
    text-align: left;
    font-size: 13px;
    color: #202124;
  }
  .engine-card:hover { background: #f8f9fa; }
  .engine-card.is-active { border-color: #202124; background: #f8f9fa; }
  .engine-card__radio { color: #202124; font-size: 12px; }
  .engine-card__label { font-weight: 600; flex: 1; }
  .engine-card__tag {
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #188038;
    background: #ceead6;
    padding: 2px 8px;
    border-radius: 4px;
  }

  .chat-tester { display: flex; gap: 8px; margin-bottom: 10px; }
  .chat-tester input {
    flex: 1;
    min-width: 0;
    padding: 8px 12px;
    border: 1px solid #dadce0;
    border-radius: 4px;
    font-size: 13px;
  }
  .chat-result {
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    padding: 12px 14px;
    background: #f8f9fa;
    margin-bottom: 8px;
  }
  .chat-result.is-error { border-color: #f28b82; background: #fce8e6; }
  .chat-result__content { margin: 0 0 8px; font-size: 13px; line-height: 1.5; color: #202124; white-space: pre-wrap; }
  .chat-result__error { margin: 0; font-size: 12px; color: #d93025; }
  .chat-result__meta { font-size: 11px; color: #5f6368; font-family: ui-monospace, monospace; }

  .empty { font-size: 13px; color: #9aa0a6; }

  .provider-list { display: flex; flex-direction: column; gap: 8px; margin-bottom: 8px; }
  .provider-card {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    padding: 10px 14px;
    border: 1px solid #dfe1e5;
    border-radius: 8px;
    background: #fff;
  }
  .provider-card.is-off { opacity: 0.55; }
  .provider-card__main { flex: 1; min-width: 0; }
  .provider-card__head { display: flex; align-items: baseline; gap: 8px; flex-wrap: wrap; }
  .provider-card__name { font-size: 13px; font-weight: 600; color: #202124; }
  .provider-card__model { font-size: 11px; color: #5f6368; font-family: ui-monospace, monospace; }
  .provider-card__sub { display: flex; gap: 12px; flex-wrap: wrap; margin-top: 2px; }
  .mono { font-size: 11px; color: #9aa0a6; font-family: ui-monospace, monospace; }
  .mono.key { color: #80868b; }
  .provider-card__test { margin-top: 6px; font-size: 11px; color: #188038; font-family: ui-monospace, monospace; }
  .provider-card__test.is-error { color: #d93025; }
  .provider-card__actions { display: flex; gap: 6px; flex-shrink: 0; }

  .add-form { border-top: 1px solid #f1f3f4; padding-top: 4px; }
  .preset-row { display: flex; gap: 6px; flex-wrap: wrap; margin-bottom: 12px; }
  .preset-chip {
    padding: 5px 12px;
    border: 1px solid #dfe1e5;
    border-radius: 20px;
    background: #fff;
    font-size: 12px;
    font-weight: 600;
    color: #5f6368;
    cursor: pointer;
  }
  .preset-chip.is-active { background: #202124; color: #fff; border-color: #202124; }
  .form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 10px; margin-bottom: 12px; }
  .form-grid__wide { grid-column: 1 / -1; }
  .form-grid label span {
    display: block;
    font-size: 10px;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: #80868b;
    margin-bottom: 4px;
  }
  .form-grid input {
    width: 100%;
    box-sizing: border-box;
    padding: 8px 12px;
    border: 1px solid #dadce0;
    border-radius: 4px;
    font-size: 13px;
    font-family: ui-monospace, monospace;
  }

  .btn { border-radius: 4px; padding: 8px 14px; font-size: 13px; font-weight: 500; cursor: pointer; border: 1px solid #dadce0; background: #fff; color: #202124; }
  .btn:disabled { opacity: 0.5; cursor: not-allowed; }
  .btn-primary { background: #202124; color: #fff; border-color: #202124; }
  .btn-ghost:hover { background: #f1f3f4; }
  .btn-sm { padding: 4px 10px; font-size: 12px; }
  .btn-danger:hover { color: #d93025; border-color: #d93025; }

  .flash { margin: 14px 0 0; font-size: 12px; color: #188038; font-family: ui-monospace, monospace; }
  .flash.is-error { color: #d93025; }
</style>
