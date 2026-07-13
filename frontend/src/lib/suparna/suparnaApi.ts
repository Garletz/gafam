import type { SuparnaReading, SuparnaStatus } from './types';

export async function fetchSuparnaStatus(
	vpcUrl: string,
	sessionToken: string
): Promise<SuparnaStatus | null> {
	try {
		const params = new URLSearchParams({ vpcUrl, token: sessionToken });
		const res = await fetch(`/api/proxy/suparna?${params}`);
		if (!res.ok) return null;
		return await res.json();
	} catch {
		return null;
	}
}

export async function invokeSuparnaAnalysis(
	vpcUrl: string,
	sessionToken: string,
	day: string,
	refresh: boolean
): Promise<{ ok: boolean; reading?: SuparnaReading; error?: string }> {
	try {
		const baseParams = new URLSearchParams({
			vpcUrl,
			token: sessionToken,
			day,
			refresh: refresh ? '1' : '0'
		});
		const res = await fetch(`/api/proxy/suparna?${baseParams}`, { method: 'POST' });
		const data = await res.json();
		if (res.status === 200 && data.summary) {
			return { ok: true, reading: data };
		}
		if (!res.ok && res.status !== 202) {
			return { ok: false, error: data.error || 'Suparna failed' };
		}

		for (let i = 0; i < 120; i++) {
			await new Promise((r) => setTimeout(r, 3000));
			const pollParams = new URLSearchParams({ vpcUrl, token: sessionToken, day });
			const pollRes = await fetch(`/api/proxy/suparna?${pollParams}`);
			const poll = await pollRes.json();
			if (poll.status === 'done' && poll.reading) {
				return { ok: true, reading: poll.reading };
			}
			if (poll.status === 'error') {
				return { ok: false, error: poll.error || 'Suparna failed' };
			}
		}
		return { ok: false, error: 'Analysis timeout (6 min) — retry in a moment' };
	} catch {
		return { ok: false, error: 'Network error' };
	}
}
