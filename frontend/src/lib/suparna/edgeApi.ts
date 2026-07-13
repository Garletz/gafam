import type { EdgeInferResult, EdgeStatus } from './types';

const RAM_BUDGET_KEY = 'gafam_edge_ram_budget_mb';

export function getRamBudgetMb(): number {
	try {
		const raw = localStorage.getItem(RAM_BUDGET_KEY);
		const n = raw ? parseInt(raw, 10) : 2048;
		return Number.isFinite(n) ? Math.min(4096, Math.max(512, n)) : 2048;
	} catch {
		return 2048;
	}
}

export function setRamBudgetMb(mb: number) {
	localStorage.setItem(RAM_BUDGET_KEY, String(mb));
}

export async function fetchEdgeStatus(
	vpcUrl: string,
	sessionToken: string
): Promise<EdgeStatus | null> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken });
		const res = await fetch(`/api/proxy/edge?${params}`);
		if (!res.ok) return null;
		return await res.json();
	} catch {
		return null;
	}
}

export async function runEdgeInfer(
	vpcUrl: string,
	sessionToken: string,
	prompt: string,
	tier: 'auto' | 'deep' | 'light' = 'deep'
): Promise<{ ok: boolean; result?: EdgeInferResult; error?: string }> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken });
		const res = await fetch(`/api/proxy/edge?${params}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				prompt,
				tier,
				ram_budget_mb: getRamBudgetMb()
			})
		});
		const data: EdgeInferResult = await res.json();
		if (!res.ok) {
			return { ok: false, error: data.error || 'Infer failed', result: data };
		}
		return { ok: true, result: data };
	} catch {
		return { ok: false, error: 'Network error' };
	}
}

export async function edgeWake(
	vpcUrl: string,
	sessionToken: string,
	ramBudgetMb?: number
): Promise<{ ok: boolean; message?: string; error?: string }> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
		const res = await fetch(`/api/proxy/edge?${params}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ ram_budget_mb: ramBudgetMb ?? getRamBudgetMb() })
		});
		const data = await res.json();
		if (!res.ok) return { ok: false, error: data.error || 'Wake failed' };
		return { ok: true, message: data.message };
	} catch {
		return { ok: false, error: 'Network error' };
	}
}

export async function edgeStop(
	vpcUrl: string,
	sessionToken: string
): Promise<{ ok: boolean; message?: string; error?: string }> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'stop' });
		const res = await fetch(`/api/proxy/edge?${params}`, { method: 'POST' });
		const data = await res.json();
		if (!res.ok) return { ok: false, error: data.error || 'Stop failed' };
		return { ok: true, message: data.message };
	} catch {
		return { ok: false, error: 'Network error' };
	}
}
