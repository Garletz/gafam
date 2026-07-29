import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST' | 'PUT' | 'DELETE',
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
			const data = await response.json().catch(() => ({}));
			return { ok: response.ok, status: response.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: socketError.message } };
		}
	}
}

function tok(token: string) {
	return `token=${encodeURIComponent(token)}`;
}

function resourcePath(
	kind: string,
	token: string,
	extra: Record<string, string> = {}
): string | null {
	const base =
		kind === 'places'
			? '/api/web/compose/places'
			: kind === 'times'
				? '/api/web/compose/times'
				: null;
	if (!base) return null;
	const qs = new URLSearchParams({ token, ...extra });
	return `${base}?${qs.toString()}`;
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const kind = url.searchParams.get('kind') || 'places';
	const q = url.searchParams.get('q') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const extra: Record<string, string> = {};
	if (q) extra.q = q;
	const path = resourcePath(kind, token, extra);
	if (!path) return json({ error: 'kind must be places|times' }, { status: 400 });

	const result = await vpcRequest(vpcUrl, token, 'GET', path);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const kind = url.searchParams.get('kind') || 'places';
	const action = url.searchParams.get('action') || 'create';
	const id = url.searchParams.get('id') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'use') {
		if (!id) return json({ error: 'id required' }, { status: 400 });
		const path =
			kind === 'places'
				? `/api/web/compose/places/${encodeURIComponent(id)}/use?${tok(token)}`
				: kind === 'times'
					? `/api/web/compose/times/${encodeURIComponent(id)}/use?${tok(token)}`
					: null;
		if (!path) return json({ error: 'kind must be places|times' }, { status: 400 });
		const result = await vpcRequest(vpcUrl, token, 'POST', path);
		return json(result.data, { status: result.status });
	}

	const path = resourcePath(kind, token);
	if (!path) return json({ error: 'kind must be places|times' }, { status: 400 });
	const body = await request.text();
	const result = await vpcRequest(vpcUrl, token, 'POST', path, body);
	return json(result.data, { status: result.status });
};

export const PUT: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const kind = url.searchParams.get('kind') || 'places';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	const path = resourcePath(kind, token);
	if (!path) return json({ error: 'kind must be places|times' }, { status: 400 });
	const body = await request.text();
	const result = await vpcRequest(vpcUrl, token, 'PUT', path, body);
	return json(result.data, { status: result.status });
};

export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const kind = url.searchParams.get('kind') || 'places';
	const id = url.searchParams.get('id') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (!id) return json({ error: 'id required' }, { status: 400 });
	const path = resourcePath(kind, token, { id });
	if (!path) return json({ error: 'kind must be places|times' }, { status: 400 });
	const result = await vpcRequest(vpcUrl, token, 'DELETE', path);
	return json(result.data, { status: result.status });
};
