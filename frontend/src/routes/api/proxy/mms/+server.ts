import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

/** GET MMS list: ?vpcUrl&token */
export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const qs = new URLSearchParams({ token });
	const result = await vpcRequest(vpcUrl, token, 'GET', `/api/web/mms?${qs}`);
	return json(result.data, { status: result.status });
};
