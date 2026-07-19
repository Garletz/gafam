import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const vpcPath = `/api/web/links?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	if (action === 'scan') {
		const phone = url.searchParams.get('phone');
		if (!phone) return json({ error: 'Missing phone' }, { status: 400 });
		const vpcPath = `/api/web/links/${encodeURIComponent(phone)}/scan?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
		return json(result.data, { status: result.status });
	}

	if (action === 'delete') {
		const id = url.searchParams.get('id');
		if (!id) return json({ error: 'Missing id' }, { status: 400 });
		const vpcPath = `/api/web/links?token=${encodeURIComponent(token)}&id=${encodeURIComponent(id)}`;
		const result = await vpcRequest(vpcUrl, token, 'DELETE', vpcPath);
		return json(result.data, { status: result.status });
	}

	// POST: add link
	const body = await request.text();
	const vpcPath = `/api/web/links?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, body);
	return json(result.data, { status: result.status });
};
