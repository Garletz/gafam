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
		// @ts-ignore
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
			const bodyBytes = new TextEncoder().encode(body).length;
			headers.push('Content-Type: application/json');
			headers.push(`Content-Length: ${bodyBytes}`);
		}
		headers.push('', '');
		if (body) {
			await writer.write(encoder.encode(headers.join('\r\n') + body));
		} else {
			await writer.write(encoder.encode(headers.join('\r\n')));
		}
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
			return {
				ok: statusCode < 400,
				status: statusCode,
				data: responseBody ? JSON.parse(responseBody) : {}
			};
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
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const path = `/api/web/edge/status?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'GET', path);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'wake') {
		const path = `/api/web/edge/wake?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'POST', path);
		return json(result.data, { status: result.status });
	}
	if (action === 'stop') {
		const path = `/api/web/edge/stop?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'POST', path);
		return json(result.data, { status: result.status });
	}

	const payload = await request.text();
	const path = `/api/web/edge/infer?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'POST', path, payload);
	return json(result.data, { status: result.status });
};
