export type LLMProvider = {
	id: string;
	name: string;
	base_url: string;
	api_key?: string;
	key_hint?: string;
	model: string;
	enabled: boolean;
};

export type LLMEngineInfo = {
	engine: string;
	available: Array<{ engine: string; label: string }>;
};

export type LLMChatResult = {
	content?: string;
	engine?: string;
	model?: string;
	latency_ms?: number;
	error?: string;
};

function qs(vpcUrl: string, sessionToken: string, extra: Record<string, string> = {}) {
	return new URLSearchParams({ vpcUrl, token: sessionToken, ...extra });
}

export async function fetchProviders(
	vpcUrl: string,
	sessionToken: string
): Promise<LLMProvider[]> {
	try {
		const res = await fetch(`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'providers' })}`);
		if (!res.ok) return [];
		const data: any = await res.json();
		return data.providers || [];
	} catch {
		return [];
	}
}

export async function saveProvider(
	vpcUrl: string,
	sessionToken: string,
	provider: Partial<LLMProvider>
): Promise<{ ok: boolean; error?: string }> {
	try {
		const res = await fetch(`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'save' })}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify(provider)
		});
		const data: any = await res.json();
		return res.ok ? { ok: true } : { ok: false, error: data.error || 'save failed' };
	} catch (e: any) {
		return { ok: false, error: e.message };
	}
}

export async function deleteProvider(
	vpcUrl: string,
	sessionToken: string,
	id: string
): Promise<{ ok: boolean; error?: string }> {
	try {
		const res = await fetch(
			`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'delete', id })}`,
			{ method: 'POST' }
		);
		const data: any = await res.json();
		return res.ok ? { ok: true } : { ok: false, error: data.error || 'delete failed' };
	} catch (e: any) {
		return { ok: false, error: e.message };
	}
}

export async function fetchEngine(
	vpcUrl: string,
	sessionToken: string
): Promise<LLMEngineInfo | null> {
	try {
		const res = await fetch(`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'engine' })}`);
		if (!res.ok) return null;
		return await res.json();
	} catch {
		return null;
	}
}

export async function setEngine(
	vpcUrl: string,
	sessionToken: string,
	engine: string
): Promise<{ ok: boolean; error?: string }> {
	try {
		const res = await fetch(`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'engine' })}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ engine })
		});
		const data: any = await res.json();
		return res.ok ? { ok: true } : { ok: false, error: data.error || 'set engine failed' };
	} catch (e: any) {
		return { ok: false, error: e.message };
	}
}

export async function testProvider(
	vpcUrl: string,
	sessionToken: string,
	id: string
): Promise<{ ok: boolean; reply?: string; latency_ms?: number; error?: string }> {
	try {
		const res = await fetch(`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'test' })}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ id })
		});
		return await res.json();
	} catch (e: any) {
		return { ok: false, error: e.message };
	}
}

export async function orchestratorChat(
	vpcUrl: string,
	sessionToken: string,
	prompt: string,
	engine?: string
): Promise<LLMChatResult> {
	try {
		const res = await fetch(`/api/proxy/llm?${qs(vpcUrl, sessionToken, { action: 'chat' })}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ prompt, engine, max_tokens: 512 })
		});
		const data: any = await res.json();
		if (!res.ok) return { error: data.error || 'chat failed' };
		return data;
	} catch (e: any) {
		return { error: e.message };
	}
}
