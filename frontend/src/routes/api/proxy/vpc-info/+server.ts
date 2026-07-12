import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcTcpRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST',
	path: string
) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	// @ts-ignore
	const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
	const socket = connect(`${host}:${port}`);
	const writer = socket.writable.getWriter();
	const encoder = new TextEncoder();

	const httpRequest = [
		`${method} ${path} HTTP/1.1`,
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
		return { ok: false, status: 502, data: { error: 'Malformed response', raw: rawResponse } };
	}

	const statusLine = rawResponse.split('\r\n')[0];
	const statusCode = parseInt(statusLine.split(' ')[1] || '500');
	const body = rawResponse.slice(bodyStart + 4).trim();

	try {
		const data = body ? JSON.parse(body) : {};
		return { ok: statusCode < 400, status: statusCode, data };
	} catch {
		return { ok: false, status: statusCode, data: { error: body || 'Upstream error' } };
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const result = await vpcTcpRequest(vpcUrl, token, 'GET', '/api/web/vpc-info');
		return json(result.data, { status: result.status });
	} catch (err: any) {
		return json({ error: 'TCP Socket failed', details: err.message }, { status: 500 });
	}
};

export const POST: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const result = await vpcTcpRequest(vpcUrl, token, 'POST', '/api/web/vpc-update');
		return json(result.data, { status: result.status });
	} catch (err: any) {
		return json({ error: 'TCP Socket failed', details: err.message }, { status: 500 });
	}
};
