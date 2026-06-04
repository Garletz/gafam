import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	// certFingerprint is informational — stored in D1 for future mTLS or audit purposes.
	// The Worker uses TCP Sockets which bypass Error 1003 on raw IPv4 addresses.

	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const parsed = new URL(vpcUrl);
		const host = parsed.hostname;
		const port = parseInt(parsed.port) || 5150;

		// Strategy: Use cloudflare:sockets (TCP) to bypass Cloudflare Error 1003
		// which blocks fetch() calls to raw IPv4 addresses.
		// TCP Sockets do NOT have this restriction and can connect to any IP:port.
		// In Cloudflare Workers runtime: uses cloudflare:sockets
		// In local dev (Node.js): falls back to regular fetch()
		try {
			// @ts-ignore — cloudflare:sockets is Cloudflare Workers runtime only
			const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');

			const socket = connect(`${host}:${port}`);
			const writer = socket.writable.getWriter();
			const encoder = new TextEncoder();

			// Manually craft the HTTP request over the raw TCP tunnel
			const httpRequest = [
				`GET /api/web/sms?token=${encodeURIComponent(token)} HTTP/1.1`,
				`Host: ${host}`,
				`Connection: close`,
				``,
				``
			].join('\r\n');

			await writer.write(encoder.encode(httpRequest));
			writer.releaseLock();

			// Read the full raw HTTP response
			const reader = socket.readable.getReader();
			const decoder = new TextDecoder();
			let rawResponse = '';

			while (true) {
				const { done, value } = await reader.read();
				if (done) break;
				rawResponse += decoder.decode(value, { stream: true });
			}

			// Parse HTTP response: split headers from body at \r\n\r\n
			const bodyStart = rawResponse.indexOf('\r\n\r\n');
			if (bodyStart === -1) {
				return json({ error: 'Malformed HTTP response from VPC' }, { status: 502 });
			}

			const statusLine = rawResponse.split('\r\n')[0];
			const statusCode = parseInt(statusLine.split(' ')[1] || '500');
			const body = rawResponse.slice(bodyStart + 4).trim();

			if (statusCode === 403) {
				// Session expired — signal the Web Client to re-authenticate
				return json({ error: 'Session expired' }, { status: 403 });
			}

			if (statusCode !== 200) {
				return json({ error: `VPC returned HTTP ${statusCode}` }, { status: statusCode });
			}

			const data = JSON.parse(body);
			return json(data);

		} catch (socketError: any) {
			// Fallback: regular fetch() for local development (Node.js runtime)
			// In local dev, there is no browser restriction on HTTP from a server process
			try {
				const response = await fetch(`${vpcUrl}/api/web/sms?token=${encodeURIComponent(token)}`, {
					// @ts-ignore
					...(typeof process !== 'undefined' ? {} : {})
				});

				if (!response.ok) {
					return json({ error: 'VPC error via fetch fallback', details: socketError.message }, { status: response.status });
				}

				const data = await response.json();
				return json(data);
			} catch (fetchError: any) {
				return json({ error: 'Both socket and fetch failed', socketErr: socketError.message, fetchErr: fetchError.message }, { status: 500 });
			}
		}

	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
};
