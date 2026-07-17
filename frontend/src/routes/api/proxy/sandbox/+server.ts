import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST' | 'DELETE',
	pathAndQuery: string,
	body?: string
) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
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
			return { ok: statusCode < 400, status: statusCode, data: responseBody ? JSON.parse(responseBody) : {} };
		} catch {
			return { ok: false, status: statusCode, data: { error: responseBody || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const response = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: { Authorization: `Bearer ${token}`, ...(body ? { 'Content-Type': 'application/json' } : {}) },
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

async function vpcFileGet(vpcUrl: string, token: string, fpath: string) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
		const httpRequest = [
			`GET ${vpcPath} HTTP/1.0`,
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
						if (remainder.length > 0) controller.enqueue(remainder);
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
			if (done) return json({ error: 'Incomplete response' }, { status: 502 });
		}
	} catch (socketError: any) {
		try {
			const vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
			const response = await fetch(`${vpcUrl}${vpcPath}`, {
				headers: { Authorization: `Bearer ${token}` }
			});
			return new Response(response.body, { status: response.status, headers: response.headers });
		} catch {
			return json({ error: socketError.message }, { status: 500 });
		}
	}
}

async function vpcFilePut(vpcUrl: string, token: string, fpath: string, body: ArrayBuffer, contentType: string) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
		const headers = [
			`PUT ${vpcPath} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Content-Type: ${contentType}`,
			`Content-Length: ${body.byteLength}`,
			`Connection: close`,
			'',
			''
		].join('\r\n');

		await writer.write(encoder.encode(headers));
		await writer.write(new Uint8Array(body));
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
		const responseBody = rawResponse.slice(bodyStart + 4).trim();

		try {
			return json(responseBody ? JSON.parse(responseBody) : {}, { status: statusCode });
		} catch {
			return json({ error: responseBody }, { status: statusCode });
		}
	} catch (socketError: any) {
		try {
			const vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
			const response = await fetch(`${vpcUrl}${vpcPath}`, {
				method: 'PUT',
				headers: { Authorization: `Bearer ${token}`, 'Content-Type': contentType },
				body
			});
			return json(await response.json().catch(() => ({})), { status: response.status });
		} catch {
			return json({ error: socketError.message }, { status: 500 });
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'status';
	const fpath = url.searchParams.get('path') || '';
	const download = url.searchParams.get('download') === '1';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'file' && fpath) {
		const res = await vpcFileGet(vpcUrl, token, fpath);
		if (download) {
			const headers = new Headers(res.headers);
			headers.set('Content-Disposition', `attachment; filename="${fpath.split('/').pop()}"`);
			return new Response(res.body, { status: res.status, headers });
		}
		return res;
	}

	let vpcPath: string;
	if (action === 'status') {
		vpcPath = `/api/web/sandbox/status?token=${encodeURIComponent(token)}`;
	} else if (action === 'storage-vpc') {
		vpcPath = `/api/web/sandbox/storage-vpc?token=${encodeURIComponent(token)}`;
	} else if (action === 'files') {
		vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
	} else if (action === 'tree') {
		const depth = url.searchParams.get('depth') || '5';
		const format = url.searchParams.get('format') || 'json';
		const tpath = fpath || '/';
		vpcPath = `/api/web/sandbox/tree?token=${encodeURIComponent(token)}&path=${encodeURIComponent(tpath)}&depth=${encodeURIComponent(depth)}&format=${encodeURIComponent(format)}`;
	} else {
		return json({ error: 'Unknown action' }, { status: 400 });
	}

	const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'wake';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let vpcPath: string;
	let body: string | undefined;
	if (action === 'wake') {
		vpcPath = `/api/web/sandbox/wake?token=${encodeURIComponent(token)}`;
	} else if (action === 'stop') {
		vpcPath = `/api/web/sandbox/stop?token=${encodeURIComponent(token)}`;
	} else if (action === 'exec') {
		vpcPath = `/api/web/sandbox/exec?token=${encodeURIComponent(token)}`;
		body = await request.text();
	} else if (action === 'shell') {
		vpcPath = `/api/web/sandbox/shell/exec?token=${encodeURIComponent(token)}`;
		body = await request.text();
	} else {
		return json({ error: 'Unknown action' }, { status: 400 });
	}

	const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, body);
	return json(result.data, { status: result.status });
};

export const PUT: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const fpath = url.searchParams.get('path') || '';
	if (!vpcUrl || !token || !fpath) return json({ error: 'Missing params' }, { status: 400 });

	const contentType = request.headers.get('Content-Type') || 'application/octet-stream';
	const body = await request.arrayBuffer();
	return vpcFilePut(vpcUrl, token, fpath, body, contentType);
};

export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const fpath = url.searchParams.get('path') || '';
	if (!vpcUrl || !token || !fpath) return json({ error: 'Missing params' }, { status: 400 });

	const vpcPath = `/api/web/sandbox${fpath}?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'DELETE', vpcPath);
	return json(result.data, { status: result.status });
};
