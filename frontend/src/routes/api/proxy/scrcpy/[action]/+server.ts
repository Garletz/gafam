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
		if (buffer[i] === 13 && buffer[i+1] === 10 && buffer[i+2] === 13 && buffer[i+3] === 10) {
			return i + 4;
		}
	}
	return -1;
}

async function handleProxy(url: URL, request: Request, action: string) {
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

			// Use HTTP/1.0 to disable chunked transfer encoding! 
			// This ensures the response body is raw unadulterated bytes until connection closes.
			let httpRequest = `${request.method} /api/scrcpy/${action}?token=${encodeURIComponent(token)} HTTP/1.0\r\n` +
				`Host: ${host}\r\n` +
				`Connection: close\r\n`;

			if (request.method === 'POST') {
				const body = await request.arrayBuffer();
				httpRequest += `Content-Length: ${body.byteLength}\r\n\r\n`;
				await writer.write(encoder.encode(httpRequest));
				await writer.write(new Uint8Array(body));
			} else {
				httpRequest += `\r\n`;
				await writer.write(encoder.encode(httpRequest));
			}
			writer.releaseLock();

			const reader = socket.readable.getReader();
			const decoder = new TextDecoder();
			let buffer = new Uint8Array(0);

			// Read headers
			while (true) {
				const { done, value } = await reader.read();
				if (value) {
					buffer = concatBuffer(buffer, value);
				}
				
				const bodyStart = findBodyStart(buffer);
				if (bodyStart !== -1) {
					// We found the end of headers!
					const headerBytes = buffer.slice(0, bodyStart - 4);
					const headerStr = decoder.decode(headerBytes);
					const lines = headerStr.split('\r\n');
					
					const statusLine = lines[0];
					const status = parseInt(statusLine.split(' ')[1] || '500');
					
					const responseHeaders = new Headers();
					for (let i = 1; i < lines.length; i++) {
						const sep = lines[i].indexOf(':');
						if (sep !== -1) {
							const key = lines[i].slice(0, sep).trim();
							const val = lines[i].slice(sep + 1).trim();
							if (key.toLowerCase() !== 'transfer-encoding' && key.toLowerCase() !== 'connection') {
								responseHeaders.append(key, val);
							}
						}
					}

					const remainder = buffer.slice(bodyStart);

					// Stream the rest of the body!
					const stream = new ReadableStream({
						async start(controller) {
							if (remainder.length > 0) {
								controller.enqueue(remainder);
							}
							if (!done) {
								while (true) {
									const { done: d, value: v } = await reader.read();
									if (v) controller.enqueue(v);
									if (d) break;
								}
							}
							controller.close();
						}
					});

					return new Response(stream, { status, headers: responseHeaders });
				}

				if (done) {
					return json({ error: 'Incomplete HTTP response' }, { status: 502 });
				}
			}
		} catch (socketError: any) {
			// Fallback to fetch for local development
			try {
				const response = await fetch(`${vpcUrl}/api/scrcpy/${action}?token=${encodeURIComponent(token)}`, {
					method: request.method,
					headers: request.headers,
					body: request.method === 'POST' ? await request.arrayBuffer() : undefined,
					// @ts-ignore
					...(typeof process !== 'undefined' ? {} : {})
				});
				
				// Return the response directly to pipe the stream
				return new Response(response.body, {
					status: response.status,
					headers: response.headers
				});
			} catch (fetchError: any) {
				return json({ error: 'Both socket and fetch failed', socketErr: socketError.message, fetchErr: fetchError.message }, { status: 500 });
			}
		}
	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
}

export const GET: RequestHandler = async ({ url, request, params }) => {
	return handleProxy(url, request, params.action);
};

export const POST: RequestHandler = async ({ url, request, params }) => {
	return handleProxy(url, request, params.action);
};
export const OPTIONS: RequestHandler = async ({ url, request, params }) => {
	return handleProxy(url, request, params.action);
};
