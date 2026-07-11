import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcGet(vpcUrl: string, token: string, pathAndQuery: string) {
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
			`GET ${pathAndQuery} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`,
			``,
			``
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
		if (bodyStart === -1) return json({ error: 'Malformed response', raw: rawResponse }, { status: 502 });

		const statusLine = rawResponse.split('\r\n')[0];
		const statusCode = parseInt(statusLine.split(' ')[1] || '500');
		const body = rawResponse.slice(bodyStart + 4).trim();

		if (statusCode >= 400) {
			try {
				return json(JSON.parse(body), { status: statusCode });
			} catch {
				return json({ error: body || 'Upstream error' }, { status: statusCode });
			}
		}

		try {
			return json(JSON.parse(body));
		} catch {
			return json({ error: 'Failed to parse JSON', raw: body }, { status: 500 });
		}
	} catch (socketError: any) {
		// Local dev fallback
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				headers: { Authorization: `Bearer ${token}` }
			});
			const data = await response.json();
			return json(data, { status: response.status });
		} catch (fetchError: any) {
			return json({ error: 'TCP Socket failed', details: socketError.message, fetch: fetchError.message }, { status: 500 });
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const day = url.searchParams.get('day');
	const offset = url.searchParams.get('offset') || '0';
	const limit = url.searchParams.get('limit') || '500';

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		let path = `/api/web/logs?token=${encodeURIComponent(token)}`;
		if (day) {
			path += `&day=${encodeURIComponent(day)}&offset=${encodeURIComponent(offset)}&limit=${encodeURIComponent(limit)}`;
		}
		return await vpcGet(vpcUrl, token, path);
	} catch (err: any) {
		return json({ error: err.message }, { status: 500 });
	}
};
