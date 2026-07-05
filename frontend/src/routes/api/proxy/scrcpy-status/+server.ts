import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const parsed = new URL(vpcUrl);
		const host = parsed.hostname;
		const port = parseInt(parsed.port) || 5150;

		try {
			// @ts-ignore — cloudflare:sockets is Cloudflare Workers runtime only
			const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
			const socket = connect(`${host}:${port}`);
			const writer = socket.writable.getWriter();
			const encoder = new TextEncoder();

			const httpRequest = [
				`GET /api/scrcpy/status?token=${encodeURIComponent(token)} HTTP/1.1`,
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
			if (bodyStart === -1) return json({ error: 'Malformed response' }, { status: 502 });

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
			// Fallback to fetch for local dev
			try {
				const res = await fetch(`${vpcUrl}/api/scrcpy/status?token=${encodeURIComponent(token)}`, {
					headers: { 'Authorization': `Bearer ${token}` }
				});
				const data = await res.json();
				return json(data, { status: res.status });
			} catch (fetchError: any) {
				return json({ error: 'Both TCP Socket and fetch failed', details: fetchError.message }, { status: 500 });
			}
		}
	} catch (err: any) {
		return json({ error: err.message }, { status: 500 });
	}
};
