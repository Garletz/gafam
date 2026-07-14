import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST' | 'DELETE',
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
			return { ok: statusCode < 400, status: statusCode, data: responseBody ? JSON.parse(responseBody) : {} };
		} catch {
			return { ok: false, status: statusCode, data: { error: responseBody || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
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
	const action = url.searchParams.get('action') || 'status';
	const fpath = url.searchParams.get('path') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let vpcPath: string;
	if (action === 'status') {
		vpcPath = `/api/web/sandbox/status?token=${encodeURIComponent(token)}`;
	} else if (action === 'storage-vpc') {
		vpcPath = `/api/web/sandbox/storage-vpc?token=${encodeURIComponent(token)}`;
	} else if (action === 'storage') {
		vpcPath = `/api/web/sandbox/storage?token=${encodeURIComponent(token)}`;
	} else if (action === 'files') {
		vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
	} else {
		return json({ error: 'Unknown action' }, { status: 400 });
	}

	const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'wake';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let vpcPath: string;
	let body: string | undefined;
	if (action === 'wake') {
		vpcPath = `/api/web/sandbox/wake?token=${encodeURIComponent(token)}`;
	} else if (action === 'stop') {
		vpcPath = `/api/web/sandbox/stop?token=${encodeURIComponent(token)}`;
	} else if (action === 'exec') {
		vpcPath = `/api/web/sandbox-exec?token=${encodeURIComponent(token)}`;
		body = await request.text();
	} else {
		return json({ error: 'Unknown action' }, { status: 400 });
	}

	const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, body);
	return json(result.data, { status: result.status });
};

export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const fpath = url.searchParams.get('path') || '';
	if (!vpcUrl || !token || !fpath) return json({ error: 'Missing params' }, { status: 400 });

	const vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'DELETE', vpcPath);
	return json(result.data, { status: result.status });
};
