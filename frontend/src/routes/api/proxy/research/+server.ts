import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(vpcUrl: string, token: string, pathAndQuery: string) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const httpRequest = [
			`GET ${pathAndQuery} HTTP/1.1`,
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
		if (bodyStart === -1) return { status: 502, data: { error: 'Malformed response' } };
		const statusCode = parseInt(rawResponse.split('\r\n')[0].split(' ')[1] || '500');
		const responseBody = rawResponse.slice(bodyStart + 4).trim();
		try {
			return { status: statusCode, data: responseBody ? JSON.parse(responseBody) : {} };
		} catch {
			return { status: statusCode, data: { error: responseBody || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				headers: { Authorization: `Bearer ${token}` }
			});
			const data = await response.json();
			return { status: response.status, data };
		} catch {
			return { status: 500, data: { error: socketError.message } };
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'notes';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let vpcPath: string;
	if (action === 'search') {
		const query = url.searchParams.get('q') || '';
		if (!query) return json({ error: 'Missing q' }, { status: 400 });
		const limit = url.searchParams.get('limit') || '10';
		vpcPath = `/api/web/research/search?token=${encodeURIComponent(token)}&q=${encodeURIComponent(query)}&limit=${encodeURIComponent(limit)}`;
	} else if (action === 'note') {
		const id = url.searchParams.get('id') || '';
		if (!id) return json({ error: 'Missing id' }, { status: 400 });
		vpcPath = `/api/web/research/note?token=${encodeURIComponent(token)}&id=${encodeURIComponent(id)}`;
	} else if (action === 'notes') {
		const limit = url.searchParams.get('limit') || '25';
		vpcPath = `/api/web/research/notes?token=${encodeURIComponent(token)}&limit=${encodeURIComponent(limit)}`;
	} else {
		return json({ error: 'Unknown action' }, { status: 400 });
	}

	const result = await vpcRequest(vpcUrl, token, vpcPath);
	return json(result.data, { status: result.status });
};
