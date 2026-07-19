<script lang="ts">
	import { encryptAESGCM } from '$lib/crypto';

	let { sessionToken, vpcUrl }: { sessionToken: string; vpcUrl: string } = $props();

	let view: 'links' | 'inbox' | 'circles' = $state('links');
	let links: any[] = $state([]);
	let inbox: any[] = $state([]);
	let circles: any[] = $state([]);
	let loading = $state(false);
	let error = $state('');
	let message = $state('');

	let newPhone = $state('');
	let newName = $state('');
	let circleName = $state('');
	let selectedCircle = $state('');
	let circleFeed: any[] = $state([]);

	const proxyParams = () => `vpcUrl=${encodeURIComponent(vpcUrl)}&token=${encodeURIComponent(sessionToken)}`;

	async function loadLinks() {
		loading = true; error = '';
		try {
			const res = await fetch(`/api/proxy/links?${proxyParams()}`);
			const data = await res.json();
			links = data.links || [];
		} catch (e: any) { error = e.message; }
		finally { loading = false; }
	}

	async function addLink() {
		if (!newPhone.trim()) return;
		error = ''; message = '';
		try {
			const res = await fetch(`/api/proxy/links?${proxyParams()}`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ phone: newPhone.trim(), name: newName.trim() })
			});
			const data = await res.json();
			if (data.error) { error = data.error; return; }
			message = `Link added: ${data.name || data.phone}`;
			newPhone = ''; newName = '';
			await loadLinks();
		} catch (e: any) { error = e.message; }
	}

	async function deleteLink(id: string) {
		try {
			await fetch(`/api/proxy/links?${proxyParams()}&action=delete&id=${id}`);
			await loadLinks();
		} catch (e: any) { error = e.message; }
	}

	async function scanLink(phone: string) {
		error = ''; message = '';
		try {
			const res = await fetch(`/api/proxy/links?${proxyParams()}&action=scan&phone=${encodeURIComponent(phone)}`);
			const data = await res.json();
			message = `Scanned ${phone}: ${data.new || 0} new envelopes`;
			await loadInbox();
		} catch (e: any) { error = e.message; }
	}

	async function loadInbox() {
		loading = true; error = '';
		try {
			const res = await fetch(`/api/proxy/inbox?${proxyParams()}`);
			const data = await res.json();
			inbox = data.inbox || [];
		} catch (e: any) { error = e.message; }
		finally { loading = false; }
	}

	async function loadCircles() {
		loading = true; error = '';
		try {
			const res = await fetch(`/api/proxy/circles?${proxyParams()}&tag=list`);
			const data = await res.json();
			circles = data.circles || [];
		} catch (e: any) { error = e.message; }
		finally { loading = false; }
	}

	async function createCircle() {
		if (!circleName.trim()) return;
		error = ''; message = '';
		try {
			const res = await fetch(`/api/proxy/circles?${proxyParams()}`, {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ name: circleName.trim(), phones: [] })
			});
			const data = await res.json();
			if (data.error) { error = data.error; return; }
			message = `Circle "${circleName.trim()}" created`;
			circleName = '';
			await loadCircles();
		} catch (e: any) { error = e.message; }
	}

	async function loadCircleFeed(name: string) {
		selectedCircle = name;
		try {
			const res = await fetch(`/api/proxy/circles?${proxyParams()}&circle=${encodeURIComponent(name)}`);
			const data = await res.json();
			circleFeed = data.inbox || [];
		} catch (e: any) { error = e.message; }
	}

	$effect(() => { loadLinks(); });
</script>

<div class="federation-container">
	<div class="federation-tabs">
		<button class="fed-tab" class:active={view === 'links'} onclick={() => { view = 'links'; loadLinks(); }}>Links</button>
		<button class="fed-tab" class:active={view === 'inbox'} onclick={() => { view = 'inbox'; loadInbox(); }}>Inbox</button>
		<button class="fed-tab" class:active={view === 'circles'} onclick={() => { view = 'circles'; loadCircles(); }}>Circles</button>
	</div>

	{#if error}
		<p class="fed-error">{error}</p>
	{/if}
	{#if message}
		<p class="fed-msg">{message}</p>
	{/if}

	{#if loading}
		<p class="fed-loading">Loading...</p>
	{:else if view === 'links'}
		<div class="fed-section">
			<h3>Add Link</h3>
			<div class="fed-form">
				<input type="text" placeholder="Phone number" bind:value={newPhone} />
				<input type="text" placeholder="Name (optional)" bind:value={newName} />
				<button onclick={addLink}>Add</button>
			</div>
		</div>
		<div class="fed-section">
			<h3>Linked Nodes ({links.length})</h3>
			{#if links.length === 0}
				<p class="fed-empty">No links yet. Add a phone number to start scanning their feed.</p>
			{:else}
				<div class="fed-list">
					{#each links as link}
						<div class="fed-item">
							<span class="fed-name">{link.name || link.phone} <code>{link.phone}</code></span>
							{#if link.vpc_url}<span class="fed-url">{link.vpc_url}</span>{/if}
							<span class="fed-actions">
								<button class="btn-scan" onclick={() => scanLink(link.phone)} title="Scan feed">Scan</button>
								<button class="btn-del" onclick={() => deleteLink(link.id)} title="Remove">×</button>
							</span>
						</div>
					{/each}
				</div>
			{/if}
		</div>

	{:else if view === 'inbox'}
		<div class="fed-section">
			<h3>Inbox ({inbox.length})</h3>
			{#if inbox.length === 0}
				<p class="fed-empty">Empty. Scan a link to fetch envelopes from their feed.</p>
			{:else}
				<div class="fed-list">
					{#each inbox as entry}
						<div class="fed-item fed-envelope">
							<span class="fed-author">{entry.author_phone}</span>
							<span class="fed-date">{entry.fetched_at}</span>
							<p class="fed-content">{entry.content}</p>
						</div>
					{/each}
				</div>
			{/if}
		</div>

	{:else if view === 'circles'}
		<div class="fed-section">
			<h3>Create Circle</h3>
			<div class="fed-form">
				<input type="text" placeholder="Circle name" bind:value={circleName} />
				<button onclick={createCircle}>Create</button>
			</div>
		</div>
		<div class="fed-section">
			<h3>Circles ({circles.length})</h3>
			{#if circles.length === 0}
				<p class="fed-empty">No circles yet. Create one to group links.</p>
			{:else}
				<div class="fed-list">
					{#each circles as circle}
						<div class="fed-item">
							<button class="fed-link" onclick={() => loadCircleFeed(circle.name)}>
								{circle.name} ({circle.phones?.length || 0} members)
							</button>
						</div>
					{/each}
				</div>
			{/if}
		</div>
		{#if selectedCircle && circleFeed.length > 0}
			<div class="fed-section">
				<h3>{selectedCircle} Feed ({circleFeed.length})</h3>
				<div class="fed-list">
					{#each circleFeed as entry}
						<div class="fed-item fed-envelope">
							<span class="fed-author">{entry.author_phone}</span>
							<p class="fed-content">{entry.content}</p>
						</div>
					{/each}
				</div>
			</div>
		{/if}
	{/if}
</div>

<style>
	.federation-container { padding: 0; }
	.federation-tabs {
		display: flex; gap: 4px; margin-bottom: 12px;
		border-bottom: 1px solid var(--c-border, #eee); padding-bottom: 8px;
	}
	.fed-tab {
		background: none; border: none; padding: 6px 14px; cursor: pointer;
		font-size: 13px; border-radius: 4px; color: var(--c-muted, #5f6368);
	}
	.fed-tab.active { background: #e8eaed; color: #202124; font-weight: 600; }
	.fed-error { color: #5f6368; font-size: 12px; margin: 4px 0; }
	.fed-msg { color: #202124; font-size: 12px; margin: 4px 0; }
	.fed-loading { color: var(--c-muted, #5f6368); font-size: 13px; }
	.fed-section { margin-bottom: 16px; }
	.fed-section h3 { font-size: 13px; margin: 0 0 6px; color: #202124; }
	.fed-form { display: flex; gap: 6px; }
	.fed-form input {
		flex: 1; padding: 5px 8px; border: 1px solid #dadce0;
		border-radius: 4px; font-size: 13px; background: #fff; color: #202124;
	}
	.fed-form button {
		padding: 5px 12px; border: none; border-radius: 4px;
		background: #202124; color: #fff; cursor: pointer; font-size: 12px;
	}
	.fed-empty { color: #5f6368; font-size: 12px; }
	.fed-list { display: flex; flex-direction: column; gap: 6px; }
	.fed-item {
		display: flex; align-items: center; gap: 8px; padding: 6px 8px;
		border-radius: 4px; background: #f1f3f4;
		font-size: 13px; color: #202124;
	}
	.fed-item.fed-envelope {
		flex-direction: column; align-items: flex-start; gap: 2px;
	}
	.fed-name { flex: 1; color: #202124; }
	.fed-name code { font-size: 11px; color: #5f6368; }
	.fed-url { font-size: 11px; color: #80868b; }
	.fed-actions { display: flex; gap: 4px; }
	.fed-author { font-weight: 600; font-size: 12px; color: #202124; }
	.fed-date { font-size: 10px; color: #80868b; }
	.fed-content { font-size: 12px; margin: 0; color: #3c4043; }
	.btn-scan {
		padding: 3px 8px; font-size: 11px; border: 1px solid #dadce0;
		border-radius: 3px; background: transparent; color: #202124; cursor: pointer;
	}
	.btn-del {
		padding: 2px 6px; font-size: 11px; border: none; background: transparent;
		color: #5f6368; cursor: pointer;
	}
	.fed-link {
		background: none; border: none; cursor: pointer; color: #202124;
		font-size: 13px; padding: 0; text-align: left;
	}
	.fed-link:hover { text-decoration: underline; }
</style>
