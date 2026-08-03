import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest as vpcRequestShared } from '$lib/vpcProxy';

async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'DELETE',
	pathAndQuery: string
) {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();
		await writer.write(
			encoder.encode(
				[
					`${method} ${pathAndQuery} HTTP/1.1`,
					`Host: ${host}`,
					`Authorization: Bearer ${token}`,
					`Connection: close`,
					``,
					``
				].join('\r\n')
			)
		);
		writer.releaseLock();

		const reader = socket.readable.getReader();
		const chunks: Uint8Array[] = [];
		while (true) {
			const { done, value } = await reader.read();
			if (done) break;
			if (value) chunks.push(value);
		}
		const total = chunks.reduce((n, c) => n + c.length, 0);
		const raw = new Uint8Array(total);
		let off = 0;
		for (const c of chunks) {
			raw.set(c, off);
			off += c.length;
		}
		const text = new TextDecoder().decode(raw);
		const sep = text.indexOf('\r\n\r\n');
		if (sep < 0) return { ok: false, status: 502, data: { error: 'Malformed response' } };
		const status = parseInt(text.split('\r\n')[0]?.split(' ')[1] || '500', 10);
		const body = text.slice(sep + 4).trim();
		try {
			return { ok: status < 400, status, data: body ? JSON.parse(body) : {} };
		} catch {
			return { ok: false, status, data: { error: body || 'parse error' } };
		}
	} catch (e: any) {
		try {
			const res = await fetch(`${vpcUrl}${pathAndQuery}`, {
				method,
				headers: { Authorization: `Bearer ${token}` }
			});
			const data = await res.json().catch(() => ({}));
			return { ok: res.ok, status: res.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: e.message } };
		}
	}
}

/** DELETE conversation thread: ?vpcUrl&token&peer= */
export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const peer = url.searchParams.get('peer');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (!peer) return json({ error: 'peer required' }, { status: 400 });

	const qs = new URLSearchParams({ token, peer });
	const result = await vpcRequest(
		vpcUrl,
		token,
		'DELETE',
		`/api/web/sms/conversation?${qs}`
	);
	return json(result.data, { status: result.status });
};

/** POST bulk delete by ids: ?vpcUrl&token — body {ids: number[]} */
export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const payload = await request.text();
	const qs = new URLSearchParams({ token });
	const result = await vpcRequestShared(vpcUrl, token, 'POST', `/api/web/sms/delete?${qs}`, payload);
	return json(result.data, { status: result.status });
};
