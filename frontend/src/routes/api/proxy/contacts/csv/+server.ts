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

		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const httpRequest = [
			`GET /api/proxy/contacts/csv HTTP/1.1`,
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

		const body = rawResponse.slice(bodyStart + 4);
		return new Response(body, {
			headers: {
				'Content-Type': 'text/csv; charset=utf-8',
				'Content-Disposition': 'attachment; filename="gafam_contacts.csv"'
			}
		});
	} catch (err: any) {
		return json({ error: err.message }, { status: 500 });
	}
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const csvData = await request.text();
		const parsed = new URL(vpcUrl);
		const host = parsed.hostname;
		const port = parseInt(parsed.port) || 5150;

		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const payloadBytes = encoder.encode(csvData);

		const httpRequest = [
			`POST /api/proxy/contacts/csv HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Content-Type: text/csv`,
			`Content-Length: ${payloadBytes.length}`,
			`Connection: close`,
			``,
			``
		].join('\r\n');

		await writer.write(encoder.encode(httpRequest));
		await writer.write(payloadBytes);
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
		try {
			return json(JSON.parse(body));
		} catch {
			return json({ status: 'imported', raw: body });
		}
	} catch (err: any) {
		return json({ error: err.message }, { status: 500 });
	}
};
