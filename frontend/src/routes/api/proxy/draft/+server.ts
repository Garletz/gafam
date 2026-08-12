import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

/** GET /api/proxy/draft?vpcUrl&token&peer= → GET /api/web/sms/draft?token=&peer= */
export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const peer = url.searchParams.get('peer');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (!peer) return json({ body: '' });

	const qs = new URLSearchParams({ token, peer });
	const result = await vpcRequest(vpcUrl, token, 'GET', `/api/web/sms/draft?${qs}`, undefined, false);
	return json(result.data, { status: result.status });
};

/** PUT /api/proxy/draft?vpcUrl&token → PUT /api/web/sms/draft?token= */
export const PUT: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const body = await request.text();
	const qs = new URLSearchParams({ token });
	const result = await vpcRequest(vpcUrl, token, 'PUT', `/api/web/sms/draft?${qs}`, body, true);
	return json(result.data, { status: result.status });
};
