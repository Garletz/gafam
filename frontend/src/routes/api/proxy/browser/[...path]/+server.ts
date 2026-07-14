import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

function concatBuffer(a: Uint8Array, b: Uint8Array): Uint8Array {
	const res = new Uint8Array(a.length + b.length);
	res.set(a, 0);
	res.set(b, a.length);
	return res;
}

function findBodyStart(buffer: Uint8Array): number {
	for (let i = 0; i < buffer.length - 3; i++) {
		if (buffer[i] === 13 && buffer[i + 1] === 10 && buffer[i + 2] === 13 && buffer[i + 3] === 10) {
			return i + 4;
		}
	}
	return -1;
}

function decodeVpc(encoded: string): string {
	const padded = encoded.replace(/-/g, '+').replace(/_/g, '/');
	const pad = padded.length % 4 === 0 ? '' : '='.repeat(4 - (padded.length % 4));
	return atob(padded + pad);
}

function injectBaseHref(html: string, baseHref: string): string {
	if (/<base\s/i.test(html)) {
		return html.replace(/<base\s[^>]*>/i, `<base href="${baseHref}">`);
	}
	if (/<head[^>]*>/i.test(html)) {
		return html.replace(/<head([^>]*)>/i, `<head$1><base href="${baseHref}">`);
	}
	return `<base href="${baseHref}">` + html;
}

async function proxyToVpc(
	vpcUrl: string,
	token: string,
	browserPath: string,
	baseHref: string | null
): Promise<Response> {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;
	const upstreamPath = browserPath.startsWith('/') ? browserPath : `/${browserPath}`;

	try {
		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const httpRequest = [
			`GET /browser${upstreamPath} HTTP/1.0`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`,
			'',
			''
		].join('\r\n');

		await writer.write(encoder.encode(httpRequest));
		writer.releaseLock();

		const reader = socket.readable.getReader();
		let buffer = new Uint8Array(0);

		while (true) {
			const { done, value } = await reader.read();
			if (value) {
				// @ts-ignore
				buffer = concatBuffer(buffer, value);
			}

			const bodyStart = findBodyStart(buffer);
			if (bodyStart !== -1) {
				const headerBytes = buffer.slice(0, bodyStart - 4);
				const headerStr = new TextDecoder().decode(headerBytes);
				const lines = headerStr.split('\r\n');
				const status = parseInt(lines[0].split(' ')[1] || '500');

				const responseHeaders = new Headers();
				let contentType = '';
				for (let i = 1; i < lines.length; i++) {
					const sep = lines[i].indexOf(':');
					if (sep === -1) continue;
					const key = lines[i].slice(0, sep).trim();
					const val = lines[i].slice(sep + 1).trim();
					const lower = key.toLowerCase();
					if (lower === 'transfer-encoding' || lower === 'connection' || lower === 'content-length') {
						continue;
					}
					if (lower === 'content-type') contentType = val;
					responseHeaders.append(key, val);
				}

				let body: Uint8Array | ReadableStream = buffer.slice(bodyStart);

				const isHtml = contentType.includes('text/html') || upstreamPath.endsWith('.html');
				if (isHtml && baseHref) {
					let html = new TextDecoder().decode(body as Uint8Array);
					if (!done) {
						while (true) {
							const { done: d, value: v } = await reader.read();
							if (v) html += new TextDecoder().decode(v);
							if (d) break;
						}
					}
					html = injectBaseHref(html, baseHref);
					responseHeaders.set('Content-Type', 'text/html; charset=utf-8');
					responseHeaders.delete('Content-Security-Policy');
					return new Response(html, { status, headers: responseHeaders });
				}

				if (!done) {
					const remainder = body as Uint8Array;
					body = new ReadableStream({
						async start(controller) {
							if (remainder.length > 0) controller.enqueue(remainder);
							while (true) {
								const { done: d, value: v } = await reader.read();
								if (v) controller.enqueue(v);
								if (d) break;
							}
							controller.close();
						}
					});
				}

				return new Response(body, { status, headers: responseHeaders });
			}

			if (done) {
				return json({ error: 'Incomplete HTTP response' }, { status: 502 });
			}
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}/browser${upstreamPath}`, {
				method: 'GET',
				headers: { Authorization: `Bearer ${token}` }
			});
			const headers = new Headers(response.headers);
			const ct = headers.get('content-type') || '';
			if (baseHref && ct.includes('text/html')) {
				let html = await response.text();
				html = injectBaseHref(html, baseHref);
				headers.delete('content-length');
				headers.delete('content-security-policy');
				return new Response(html, { status: response.status, headers });
			}
			return new Response(response.body, { status: response.status, headers });
		} catch {
			return json(
				{ error: 'Proxy failed', details: socketError?.message || String(socketError) },
				{ status: 500 }
			);
		}
	}
}

export const GET: RequestHandler = async ({ params, url }) => {
	const parts = params.path?.split('/').filter(Boolean) || [];
	// Expected: t/<base64url-vpc>/<token>/...asset
	if (parts.length < 3 || parts[0] !== 't') {
		return json(
			{ error: 'Invalid browser tunnel path. Use /api/proxy/browser/t/<vpc>/<token>/...' },
			{ status: 400 }
		);
	}

	const vpcEnc = parts[1];
	const token = parts[2];
	const assetParts = parts.slice(3);
	const assetPath = '/' + (assetParts.length ? assetParts.join('/') : 'vnc.html');

	let vpcUrl: string;
	try {
		vpcUrl = decodeVpc(vpcEnc);
	} catch {
		return json({ error: 'Invalid vpc encoding' }, { status: 400 });
	}

	// Preserve noVNC query args (autoconnect, path for websockify, etc.)
	const qs = url.searchParams.toString();
	const upstream = qs ? `${assetPath}?${qs}` : assetPath;

	const baseHref = `/api/proxy/browser/t/${vpcEnc}/${token}/`;
	return proxyToVpc(vpcUrl, token, upstream, assetPath.endsWith('.html') || assetPath === '/' ? baseHref : null);
};
