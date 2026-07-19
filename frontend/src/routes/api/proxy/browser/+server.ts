import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST',
	pathAndQuery: string,
	body?: string
) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const headers = [
			`${method} ${pathAndQuery} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`
		];
		if (body) {
			const bodyBytes = new TextEncoder().encode(body).length;
			headers.push('Content-Type: application/json');
			headers.push(`Content-Length: ${bodyBytes}`);
		}
		headers.push('', '');
		if (body) {
			await writer.write(encoder.encode(headers.join('\r\n') + body));
		} else {
			await writer.write(encoder.encode(headers.join('\r\n')));
		}
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
			return { ok: false, status: 502, data: { error: 'Malformed response' } };
		}

		const statusLine = rawResponse.split('\r\n')[0];
		const statusCode = parseInt(statusLine.split(' ')[1] || '500');
		const responseBody = rawResponse.slice(bodyStart + 4).trim();

		try {
			return {
				ok: statusCode < 400,
				status: statusCode,
				data: responseBody ? JSON.parse(responseBody) : {}
			};
		} catch {
			return { ok: false, status: statusCode, data: { error: responseBody || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: {
					Authorization: `Bearer ${token}`,
					...(body ? { 'Content-Type': 'application/json' } : {})
				},
				body
			});
			const data = await response.json();
			return { ok: response.ok, status: response.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: socketError.message } };
		}
	}
}

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

async function proxyStream(url: URL, request: Request) {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const path = `/api/web/browser/stream?token=${encodeURIComponent(token)}`;
		const httpRequest = [
			`GET ${path} HTTP/1.0`,
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
				// @ts-ignore - ArrayBufferLike vs ArrayBuffer
				buffer = concatBuffer(buffer, value);
			}

			const bodyStart = findBodyStart(buffer);
			if (bodyStart !== -1) {
				const headerBytes = buffer.slice(0, bodyStart - 4);
				const decoder = new TextDecoder();
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
		try {
			const path = `/api/web/browser/stream?token=${encodeURIComponent(token)}`;
			const response = await fetch(`${vpcUrl}${path}`, {
				method: 'GET',
				headers: { Authorization: `Bearer ${token}` }
			});
			return new Response(response.body, {
				status: response.status,
				headers: response.headers
			});
		} catch {
			return json({ error: 'Both socket and fetch failed' }, { status: 500 });
		}
	}
}

async function proxyInput(url: URL, request: Request) {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;
	const payload = await request.text();

	try {
		// @ts-ignore
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const path = `/api/web/browser/input?token=${encodeURIComponent(token)}`;
		const bodyBytes = new TextEncoder().encode(payload).length;
		const httpRequest = [
			`POST ${path} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Content-Type: application/json`,
			`Content-Length: ${bodyBytes}`,
			`Connection: close`,
			'',
			payload
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
			return json({ error: 'Malformed response' }, { status: 502 });
		}

		const statusLine = rawResponse.split('\r\n')[0];
		const statusCode = parseInt(statusLine.split(' ')[1] || '500');
		const responseBody = rawResponse.slice(bodyStart + 4).trim();

		try {
			return json(responseBody ? JSON.parse(responseBody) : {}, { status: statusCode });
		} catch {
			return json({ error: responseBody }, { status: statusCode });
		}
	} catch (socketError: any) {
		try {
			const path = `/api/web/browser/input?token=${encodeURIComponent(token)}`;
			const response = await fetch(`${vpcUrl}${path}`, {
				method: 'POST',
				headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
				body: payload
			});
			return json(await response.json(), { status: response.status });
		} catch {
			return json({ error: 'Both socket and fetch failed' }, { status: 500 });
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'status' || !action) {
		const vpcPath = `/api/web/browser/status?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
		return json(result.data, { status: result.status });
	}

	if (action === 'stream') {
		return proxyStream(url, new Request(url));
	}

	if (action === 'fetch') {
		const target = url.searchParams.get('url') || '';
		if (!target) return json({ error: 'Missing url param' }, { status: 400 });
		const vpcPath = `/api/web/browser/fetch?token=${encodeURIComponent(token)}&url=${encodeURIComponent(target)}`;
		const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
		return json(result.data, { status: result.status });
	}

	if (action === 'window') {
		const vpcPath = `/api/web/browser/window?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
		return json(result.data, { status: result.status });
	}

	return json({ error: 'Unknown action' }, { status: 400 });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'wake') {
		const mode = url.searchParams.get('mode') || '';
		const engine = url.searchParams.get('engine') || '';
		let vpcPath = `/api/web/browser/wake?token=${encodeURIComponent(token)}`;
		if (mode) vpcPath += `&mode=${encodeURIComponent(mode)}`;
		if (engine) vpcPath += `&engine=${encodeURIComponent(engine)}`;
		const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, '{}');
		return json(result.data, { status: result.status });
	}

	if (action === 'stop') {
		const vpcPath = `/api/web/browser/stop?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath);
		return json(result.data, { status: result.status });
	}

	if (action === 'input') {
		return proxyInput(url, request);
	}

	if (action === 'navigate') {
		const vpcPath = `/api/web/browser/navigate?token=${encodeURIComponent(token)}`;
		const body = await request.text();
		const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, body);
		return json(result.data, { status: result.status });
	}

	return json({ error: 'Unknown action' }, { status: 400 });
};

export const OPTIONS: RequestHandler = async ({ url }) => {
	return new Response(null, {
		status: 200,
		headers: {
			'Access-Control-Allow-Origin': '*',
			'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
			'Access-Control-Allow-Headers': 'Content-Type, Authorization'
		}
	});
};
