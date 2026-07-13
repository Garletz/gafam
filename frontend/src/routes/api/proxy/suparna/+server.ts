import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST',
	pathAndQuery: string
) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const httpRequest = [
			`${method} ${pathAndQuery} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`,
			'',
			''
		].join('\r\n');

		await writer.write(encoder.encode(httpRequest));
		writer.releaseLock();

		const reader = socket.readable.getReader();
		const decoder = new TextDecoder();
		let rawResponse = '';

		while (true) {
			const { done, value } = await reader.read();
			if (done) break;
			rawResponse += decoder.decode(value, { stream: true });
		}

		const bodyStart = rawResponse.indexOf('\r\n\r\n');
		if (bodyStart === -1) {
			return { ok: false, status: 502, data: { error: 'Malformed response' } };
		}

		const statusLine = rawResponse.split('\r\n')[0];
		const statusCode = parseInt(statusLine.split(' ')[1] || '500');
		const body = rawResponse.slice(bodyStart + 4).trim();

		try {
			return { ok: statusCode < 400, status: statusCode, data: body ? JSON.parse(body) : {} };
		} catch {
			return { ok: false, status: statusCode, data: { error: body || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: { Authorization: `Bearer ${token}` }
			});
			const data = await response.json();
			return { ok: response.ok, status: response.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: socketError.message } };
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const day = url.searchParams.get('day');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const path = day
		? `/api/web/logs/suparna/reading?token=${encodeURIComponent(token)}&day=${encodeURIComponent(day)}`
		: `/api/web/suparna/status?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'GET', path);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const day = url.searchParams.get('day');
	const refresh = url.searchParams.get('refresh') || '0';

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (!day) return json({ error: 'Missing day' }, { status: 400 });

	const path = `/api/web/logs/suparna?token=${encodeURIComponent(token)}&day=${encodeURIComponent(day)}&refresh=${refresh}`;
	const result = await vpcRequest(vpcUrl, token, 'POST', path);
	return json(result.data, { status: result.status });
};
