import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const vpcPath = `/api/web/feed?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const body = await request.text();
	const vpcPath = `/api/web/feed/publish?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, body);
	return json(result.data, { status: result.status });
};
