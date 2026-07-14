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
	// Normalize path query before Debian noVNC does: url += '/' + path
	const fixScript = `<script>(function(){try{var u=new URL(location.href);var p=u.searchParams.get('path')||'';if(/^wss?:\\/\\//i.test(p)){var w=new URL(p);p=w.pathname.replace(/^\\/+/, '')+(w.search||'');u.searchParams.set('path',p);history.replaceState(null,'',u);}else if(p.charAt(0)==='/'){u.searchParams.set('path',p.replace(/^\\/+/, ''));history.replaceState(null,'',u);}}catch(e){}})();</script>`;
	const inject = `<base href="${baseHref}">${fixScript}`;
	if (/<base\s/i.test(html)) {
		html = html.replace(/<base\s[^>]*>/i, inject);
	} else if (/<head[^>]*>/i.test(html)) {
		html = html.replace(/<head([^>]*)>/i, `<head$1>${inject}`);
	} else {
		html = inject + html;
	}
	return html;
}

function encodeClientWsFrame(opcode: number, payload: Uint8Array): Uint8Array {
	const mask = crypto.getRandomValues(new Uint8Array(4));
	const len = payload.length;
	let header: number[];
	if (len < 126) {
		header = [0x80 | opcode, 0x80 | len];
	} else if (len < 65536) {
		header = [0x80 | opcode, 0x80 | 126, (len >> 8) & 0xff, len & 0xff];
	} else {
		header = [
			0x80 | opcode,
			0x80 | 127,
			0,
			0,
			0,
			0,
			(len >>> 24) & 0xff,
			(len >>> 16) & 0xff,
			(len >>> 8) & 0xff,
			len & 0xff
		];
	}
	const out = new Uint8Array(header.length + 4 + len);
	out.set(header, 0);
	out.set(mask, header.length);
	for (let i = 0; i < len; i++) {
		out[header.length + 4 + i] = payload[i] ^ mask[i % 4];
	}
	return out;
}

type ParsedFrame = { opcode: number; payload: Uint8Array; fin: boolean };

function parseServerWsFrames(buf: Uint8Array): { frames: ParsedFrame[]; rest: Uint8Array } {
	const frames: ParsedFrame[] = [];
	let offset = 0;
	while (offset + 2 <= buf.length) {
		const b0 = buf[offset];
		const b1 = buf[offset + 1];
		const fin = (b0 & 0x80) !== 0;
		const opcode = b0 & 0x0f;
		const masked = (b1 & 0x80) !== 0;
		let len = b1 & 0x7f;
		let hdr = 2;
		if (len === 126) {
			if (offset + 4 > buf.length) break;
			len = (buf[offset + 2] << 8) | buf[offset + 3];
			hdr = 4;
		} else if (len === 127) {
			if (offset + 10 > buf.length) break;
			len =
				((buf[offset + 6] << 24) | (buf[offset + 7] << 16) | (buf[offset + 8] << 8) | buf[offset + 9]) >>>
				0;
			hdr = 10;
		}
		const maskLen = masked ? 4 : 0;
		if (offset + hdr + maskLen + len > buf.length) break;
		const start = offset + hdr + maskLen;
		const payload = buf.slice(start, start + len);
		if (masked) {
			const mask = buf.slice(offset + hdr, offset + hdr + 4);
			for (let i = 0; i < payload.length; i++) payload[i] ^= mask[i % 4];
		}
		frames.push({ opcode, payload, fin });
		offset = start + len;
	}
	return { frames, rest: buf.slice(offset) };
}

async function proxyWebSocket(
	vpcUrl: string,
	token: string,
	browserPath: string,
	clientProtocols: string | null
): Promise<Response> {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;
	const upstreamPath = browserPath.startsWith('/') ? browserPath : `/${browserPath}`;

	// @ts-ignore
	const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
	const tcp = connect(`${host}:${port}`);
	const writer = tcp.writable.getWriter();
	const reader = tcp.readable.getReader();
	const encoder = new TextEncoder();

	const wsKey = btoa(String.fromCharCode(...crypto.getRandomValues(new Uint8Array(16))));
	const upgradeLines = [
		`GET /browser${upstreamPath} HTTP/1.1`,
		`Host: ${host}`,
		`Authorization: Bearer ${token}`,
		`Upgrade: websocket`,
		`Connection: Upgrade`,
		`Sec-WebSocket-Key: ${wsKey}`,
		`Sec-WebSocket-Version: 13`
	];
	if (clientProtocols) {
		upgradeLines.push(`Sec-WebSocket-Protocol: ${clientProtocols}`);
	}
	upgradeLines.push('', '');
	await writer.write(encoder.encode(upgradeLines.join('\r\n')));

	let buffer: Uint8Array = new Uint8Array(0);
	let upgraded = false;
	let acceptedProtocol = '';
	while (!upgraded) {
		const { done, value } = await reader.read();
		if (done) {
			return json({ error: 'VPC closed before WebSocket upgrade' }, { status: 502 });
		}
		buffer = concatBuffer(buffer, new Uint8Array(value));
		const bodyStart = findBodyStart(buffer);
		if (bodyStart === -1) continue;
		const headerStr = new TextDecoder().decode(buffer.subarray(0, bodyStart - 4));
		const status = parseInt(headerStr.split('\r\n')[0].split(' ')[1] || '500');
		if (status !== 101) {
			return json(
				{ error: 'WebSocket upgrade rejected by VPC', status, detail: headerStr.slice(0, 300) },
				{ status: 502 }
			);
		}
		const protoMatch = headerStr.match(/Sec-WebSocket-Protocol:\s*(.+)/i);
		if (protoMatch) acceptedProtocol = protoMatch[1].trim();
		buffer = new Uint8Array(buffer.subarray(bodyStart));
		upgraded = true;
	}

	const pair = new WebSocketPair();
	const client = pair[0];
	const server = pair[1];
	server.accept();

	let closed = false;
	const closeBoth = () => {
		if (closed) return;
		closed = true;
		try {
			server.close();
		} catch {
			/* ignore */
		}
		try {
			writer.releaseLock();
			tcp.close?.();
		} catch {
			/* ignore */
		}
	};

	server.addEventListener('message', async (event: MessageEvent) => {
		if (closed) return;
		try {
			let payload: Uint8Array;
			let opcode = 0x2;
			if (typeof event.data === 'string') {
				payload = encoder.encode(event.data);
				opcode = 0x1;
			} else if (event.data instanceof ArrayBuffer) {
				payload = new Uint8Array(event.data);
			} else if (ArrayBuffer.isView(event.data)) {
				payload = new Uint8Array(event.data.buffer, event.data.byteOffset, event.data.byteLength);
			} else {
				return;
			}
			await writer.write(encodeClientWsFrame(opcode, payload));
		} catch {
			closeBoth();
		}
	});

	server.addEventListener('close', closeBoth);
	server.addEventListener('error', closeBoth);

	(async () => {
		try {
			while (!closed) {
				const { done, value } = await reader.read();
				if (done) break;
				if (value) buffer = concatBuffer(buffer, new Uint8Array(value));
				const { frames, rest } = parseServerWsFrames(buffer);
				buffer = new Uint8Array(rest);
				for (const frame of frames) {
					if (frame.opcode === 0x8) {
						closeBoth();
						return;
					}
					if (frame.opcode === 0x9) {
						await writer.write(encodeClientWsFrame(0xa, frame.payload));
						continue;
					}
					if (frame.opcode === 0xa) continue;
					if (frame.opcode === 0x1) {
						server.send(new TextDecoder().decode(frame.payload));
					} else {
						server.send(frame.payload);
					}
				}
			}
		} catch {
			/* ignore */
		} finally {
			closeBoth();
		}
	})();

	const headers = new Headers();
	if (acceptedProtocol) headers.set('Sec-WebSocket-Protocol', acceptedProtocol);
	return new Response(null, { status: 101, webSocket: client, headers });
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
				buffer = concatBuffer(buffer, new Uint8Array(value));
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

				return new Response(body as BodyInit, { status, headers: responseHeaders });
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

function parseTunnel(params: { path?: string }) {
	const parts = params.path?.split('/').filter(Boolean) || [];
	if (parts.length < 3 || parts[0] !== 't') return null;
	const vpcEnc = parts[1];
	const token = parts[2];
	const assetParts = parts.slice(3);
	const assetPath = '/' + (assetParts.length ? assetParts.join('/') : 'vnc.html');
	let vpcUrl: string;
	try {
		vpcUrl = decodeVpc(vpcEnc);
	} catch {
		return null;
	}
	return { vpcEnc, token, assetPath, vpcUrl };
}

export const GET: RequestHandler = async ({ params, url, request }) => {
	const tunnel = parseTunnel(params);
	if (!tunnel) {
		return json(
			{ error: 'Invalid browser tunnel path. Use /api/proxy/browser/t/<vpc>/<token>/...' },
			{ status: 400 }
		);
	}

	const { vpcEnc, token, assetPath, vpcUrl } = tunnel;

	if (request.headers.get('Upgrade')?.toLowerCase() === 'websocket') {
		return proxyWebSocket(
			vpcUrl,
			token,
			assetPath,
			request.headers.get('Sec-WebSocket-Protocol')
		);
	}

	const qs = url.searchParams.toString();
	const upstream = qs ? `${assetPath}?${qs}` : assetPath;
	const baseHref = `/api/proxy/browser/t/${vpcEnc}/${token}/`;
	return proxyToVpc(vpcUrl, token, upstream, assetPath.endsWith('.html') || assetPath === '/' ? baseHref : null);
};
