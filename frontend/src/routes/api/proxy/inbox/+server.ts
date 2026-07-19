import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const vpcPath = `/api/web/inbox?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
	return json(result.data, { status: result.status });
};
