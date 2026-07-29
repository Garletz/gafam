import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

async function connectVpc(vpcUrl: string) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;
	const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
	const socket = connect(`${host}:${port}`);
	return { socket, host, port };
}

async function readRaw(socket: { readable: ReadableStream }, maxBytes = 2 << 20): Promise<Uint8Array> {
	const reader = socket.readable.getReader();
	const chunks: Uint8Array[] = [];
	let total = 0;
	while (true) {
		const { done, value } = await reader.read();
		if (done) break;
		if (value) {
			chunks.push(value);
			total += value.length;
			if (total > maxBytes) break;
		}
	}
	const out = new Uint8Array(total);
	let off = 0;
	for (const c of chunks) {
		out.set(c, off);
		off += c.length;
	}
	return out;
}

function splitHttp(raw: Uint8Array): { status: number; headers: Headers; body: Uint8Array } {
	const decoder = new TextDecoder();
	let sep = -1;
	for (let i = 0; i < raw.length - 3; i++) {
		if (raw[i] === 13 && raw[i + 1] === 10 && raw[i + 2] === 13 && raw[i + 3] === 10) {
			sep = i;
			break;
		}
	}
	if (sep < 0) {
		return { status: 502, headers: new Headers(), body: new Uint8Array() };
	}
	const headText = decoder.decode(raw.subarray(0, sep));
	let body = raw.subarray(sep + 4);
	const lines = headText.split('\r\n');
	const status = parseInt(lines[0]?.split(' ')[1] || '500', 10);
	const headers = new Headers();
	for (let i = 1; i < lines.length; i++) {
		const idx = lines[i].indexOf(':');
		if (idx > 0) {
			headers.set(lines[i].slice(0, idx).trim(), lines[i].slice(idx + 1).trim());
		}
	}
	const cl = headers.get('Content-Length');
	if (cl) {
		const n = parseInt(cl, 10);
		if (!Number.isNaN(n) && n >= 0 && n <= body.length) {
			body = body.subarray(0, n);
		}
	}
	return { status, headers, body };
}

async function vpcJson(
	vpcUrl: string,
	token: string,
	method: string,
	pathAndQuery: string,
	body?: string
) {
	try {
		const { socket, host } = await connectVpc(vpcUrl);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();
		const lines = [
			`${method} ${pathAndQuery} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`
		];
		if (body) {
			lines.push('Content-Type: application/json');
			lines.push(`Content-Length: ${encoder.encode(body).length}`);
		}
		lines.push('', '');
		await writer.write(encoder.encode(lines.join('\r\n') + (body || '')));
		writer.releaseLock();

		const raw = await readRaw(socket);
		const { status, body: respBody } = splitHttp(raw);
		const text = new TextDecoder().decode(respBody).trim();
		try {
			return { ok: status < 400, status, data: text ? JSON.parse(text) : {} };
		} catch {
			return { ok: false, status, data: { error: text || 'parse error' } };
		}
	} catch (e: any) {
		try {
			const res = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: {
					Authorization: `Bearer ${token}`,
					...(body ? { 'Content-Type': 'application/json' } : {})
				},
				body
			});
			const data = await res.json().catch(() => ({}));
			return { ok: res.ok, status: res.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: e.message } };
		}
	}
}

async function vpcBinary(
	vpcUrl: string,
	token: string,
	path: string,
	maxBytes: number,
	fallbackType: string
): Promise<Response> {
	const full = path.includes('?')
		? `${path}&token=${encodeURIComponent(token)}`
		: `${path}?token=${encodeURIComponent(token)}`;
	try {
		const { socket, host } = await connectVpc(vpcUrl);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();
		const req =
			`GET ${full} HTTP/1.1\r\n` +
			`Host: ${host}\r\n` +
			`Authorization: Bearer ${token}\r\n` +
			`Connection: close\r\n\r\n`;
		await writer.write(encoder.encode(req));
		writer.releaseLock();

		const raw = await readRaw(socket, maxBytes);
		const { status, headers, body } = splitHttp(raw);
		const ct = headers.get('Content-Type') || fallbackType;
		const cache = headers.get('X-Geo-Cache') || '';
		const layer = headers.get('X-Geo-Layer') || '';
		return new Response(body, {
			status,
			headers: {
				'Content-Type': ct,
				'Cache-Control': 'public, max-age=86400',
				...(cache ? { 'X-Geo-Cache': cache } : {}),
				...(layer ? { 'X-Geo-Layer': layer } : {})
			}
		});
	} catch (e: any) {
		try {
			const res = await fetch(`${vpcUrl}${full}`, {
				headers: { Authorization: `Bearer ${token}` }
			});
			const buf = await res.arrayBuffer();
			return new Response(buf, {
				status: res.status,
				headers: {
					'Content-Type': res.headers.get('Content-Type') || fallbackType,
					'Cache-Control': 'public, max-age=86400'
				}
			});
		} catch {
			return json({ error: e.message }, { status: 500 });
		}
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'search';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'tiles') {
		const z = url.searchParams.get('z') || '';
		const x = url.searchParams.get('x') || '';
		const y = url.searchParams.get('y') || '';
		if (!z || !x || !y) return json({ error: 'z,x,y required' }, { status: 400 });
		return vpcBinary(
			vpcUrl,
			token,
			`/api/web/geo/tiles/${encodeURIComponent(z)}/${encodeURIComponent(x)}/${encodeURIComponent(y)}`,
			1 << 20,
			'image/png'
		);
	}

	if (action === 'basemap') {
		return vpcBinary(vpcUrl, token, '/api/web/geo/basemap', 20 << 20, 'image/jpeg');
	}

	if (action === 'layer') {
		const name = url.searchParams.get('name') || '';
		if (!name) return json({ error: 'name required' }, { status: 400 });
		return vpcBinary(
			vpcUrl,
			token,
			`/api/web/geo/layers/${encodeURIComponent(name)}`,
			12 << 20,
			'application/geo+json'
		);
	}

	if (action === 'layers') {
		const result = await vpcJson(
			vpcUrl,
			token,
			'GET',
			`/api/web/geo/layers?token=${encodeURIComponent(token)}`
		);
		return json(result.data, { status: result.status });
	}

	if (action === 'status') {
		const result = await vpcJson(
			vpcUrl,
			token,
			'GET',
			`/api/web/geo/status?token=${encodeURIComponent(token)}`
		);
		return json(result.data, { status: result.status });
	}

	// search
	const q = url.searchParams.get('q') || '';
	const limit = url.searchParams.get('limit') || '20';
	const qs = new URLSearchParams({ token, q, limit });
	const result = await vpcJson(vpcUrl, token, 'GET', `/api/web/geo/search?${qs}`);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'import';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (action !== 'import') return json({ error: 'unknown action' }, { status: 400 });
	const result = await vpcJson(
		vpcUrl,
		token,
		'POST',
		`/api/web/geo/import?token=${encodeURIComponent(token)}`
	);
	return json(result.data, { status: result.status });
};
