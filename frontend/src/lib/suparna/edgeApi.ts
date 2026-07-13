import type { EdgeInferResult, EdgeStatus } from './types';

const RAM_REQUEST_KEY = 'gafam_edge_ram_request_mb';

export function getRamRequestMb(maxDeliverable?: number): number {
	const ceiling = maxDeliverable && maxDeliverable >= 512 ? maxDeliverable : 4096;
	try {
		const raw = localStorage.getItem(RAM_REQUEST_KEY);
		const n = raw ? parseInt(raw, 10) : Math.min(2048, ceiling);
		return Number.isFinite(n) ? Math.min(ceiling, Math.max(512, n)) : Math.min(2048, ceiling);
	} catch {
		return Math.min(2048, ceiling);
	}
}

export function setRamRequestMb(mb: number, maxDeliverable?: number) {
	const ceiling = maxDeliverable && maxDeliverable >= 512 ? maxDeliverable : 4096;
	const clamped = Math.min(ceiling, Math.max(512, mb));
	localStorage.setItem(RAM_REQUEST_KEY, String(clamped));
}

/** @deprecated use getRamRequestMb */
export function getRamBudgetMb(): number {
	return getRamRequestMb();
}

/** @deprecated use setRamRequestMb */
export function setRamBudgetMb(mb: number) {
	setRamRequestMb(mb);
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
	tier: 'auto' | 'deep' | 'light' = 'deep',
	ramRequestMb?: number
): Promise<{ ok: boolean; result?: EdgeInferResult; error?: string }> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken });
		const res = await fetch(`/api/proxy/edge?${params}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({
				prompt,
				tier,
				ram_request_mb: ramRequestMb ?? getRamRequestMb()
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
	ramRequestMb?: number
): Promise<{ ok: boolean; message?: string; error?: string }> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken, action: 'wake' });
		const res = await fetch(`/api/proxy/edge?${params}`, {
			method: 'POST',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ ram_request_mb: ramRequestMb ?? getRamRequestMb() })
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
