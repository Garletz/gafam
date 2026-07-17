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

function tok(token: string) {
	return `token=${encodeURIComponent(token)}`;
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'list';
	const id = url.searchParams.get('id') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let path: string;
	if (action === 'world-card') {
		path = `/api/web/mission/world-card?${tok(token)}`;
	} else if (action === 'orchestrator-status') {
		path = `/api/web/orchestrator/status?${tok(token)}`;
	} else if (action === 'get' && id) {
		path = `/api/web/mission/${encodeURIComponent(id)}?${tok(token)}`;
	} else {
		path = `/api/web/mission?${tok(token)}`;
	}

	const result = await vpcRequest(vpcUrl, token, 'GET', path);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'create';
	const id = url.searchParams.get('id') || '';
	const qid = url.searchParams.get('qid') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const body = await request.text();
	let path: string;

	switch (action) {
		case 'create':
			path = `/api/web/mission?${tok(token)}`;
			break;
		case 'orchestrate':
			path = `/api/web/orchestrator/run?${tok(token)}`;
			break;
		case 'claim':
			if (!id || !qid) return json({ error: 'Missing id/qid' }, { status: 400 });
			path = `/api/web/mission/${encodeURIComponent(id)}/quest/${encodeURIComponent(qid)}/claim?${tok(token)}`;
			break;
		case 'run':
			if (!id || !qid) return json({ error: 'Missing id/qid' }, { status: 400 });
			path = `/api/web/mission/${encodeURIComponent(id)}/quest/${encodeURIComponent(qid)}/run?${tok(token)}`;
			break;
		case 'reward':
			if (!id || !qid) return json({ error: 'Missing id/qid' }, { status: 400 });
			path = `/api/web/mission/${encodeURIComponent(id)}/quest/${encodeURIComponent(qid)}/reward?${tok(token)}`;
			break;
		case 'add-quest':
			if (!id) return json({ error: 'Missing id' }, { status: 400 });
			path = `/api/web/mission/${encodeURIComponent(id)}/quest?${tok(token)}`;
			break;
		case 'synthesize':
			if (!id) return json({ error: 'Missing id' }, { status: 400 });
			path = `/api/web/mission/${encodeURIComponent(id)}/synthesize?${tok(token)}`;
			break;
		default:
			return json({ error: 'Unknown action' }, { status: 400 });
	}

	const result = await vpcRequest(vpcUrl, token, 'POST', path, body || undefined);
	return json(result.data, { status: result.status });
};

export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const id = url.searchParams.get('id') || '';
	if (!vpcUrl || !token || !id) return json({ error: 'Missing params' }, { status: 400 });

	const path = `/api/web/mission/${encodeURIComponent(id)}?${tok(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'DELETE', path);
	return json(result.data, { status: result.status });
};
