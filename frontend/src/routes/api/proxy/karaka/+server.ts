import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST',
	pathAndQuery: string,
	body?: string
) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const headers = [
			`${method} ${pathAndQuery} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`
		];
		if (body) {
			const bodyBytes = encoder.encode(body).length;
			headers.push('Content-Type: application/json');
			headers.push(`Content-Length: ${bodyBytes}`);
		}
		headers.push('', '');
		await writer.write(encoder.encode(headers.join('\r\n') + (body || '')));
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
		const responseBody = rawResponse.slice(bodyStart + 4).trim();
		try {
			return { ok: statusCode < 400, status: statusCode, data: responseBody ? JSON.parse(responseBody) : {} };
		} catch {
			return { ok: false, status: statusCode, data: { error: responseBody || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: {
					Authorization: `Bearer ${token}`,
					...(body ? { 'Content-Type': 'application/json' } : {})
				},
				body
			});
			const data = await response.json().catch(() => ({}));
			return { ok: response.ok, status: response.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: socketError.message } };
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'status';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const path =
		action === 'tools'
			? `/api/web/karaka/tools?token=${encodeURIComponent(token)}`
			: `/api/web/karaka/status?token=${encodeURIComponent(token)}`;

	const result = await vpcRequest(vpcUrl, token, 'GET', path);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'execute';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (action !== 'execute') return json({ error: 'Unknown action' }, { status: 400 });

	const body = await request.text();
	const path = `/api/web/karaka/execute?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'POST', path, body);
	return json(result.data, { status: result.status });
};
