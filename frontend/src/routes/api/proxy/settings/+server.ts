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
			// @ts-ignore
			const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
			const socket = connect(`${host}:${port}`);
			const writer = socket.writable.getWriter();
			const encoder = new TextEncoder();

			const httpRequest = [
				`GET /api/settings HTTP/1.1`,
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

			const body = rawResponse.slice(bodyStart + 4).trim();
			return json(JSON.parse(body));
		} catch (e) {
			const res = await fetch(`${vpcUrl}/api/settings`, {
				headers: { 'Authorization': `Bearer ${token}` }
			});
			return json(await res.json());
		}
	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const payload = await request.json();
		const payloadStr = JSON.stringify(payload);
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
				`POST /api/settings HTTP/1.1`,
				`Host: ${host}`,
				`Authorization: Bearer ${token}`,
				`Content-Type: application/json`,
				`Content-Length: ${payloadStr.length}`,
				`Connection: close`,
				``,
				payloadStr
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

			const body = rawResponse.slice(bodyStart + 4).trim();
			return json(JSON.parse(body));
		} catch (e) {
			const res = await fetch(`${vpcUrl}/api/settings`, {
				method: 'POST',
				headers: {
					'Authorization': `Bearer ${token}`,
					'Content-Type': 'application/json'
				},
				body: payloadStr
			});
			return json(await res.json());
		}
	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
};
