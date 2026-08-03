import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

/** GET one MMS media part (base64, encrypted): ?vpcUrl&token&id= */
export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const id = url.searchParams.get('id');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });
	if (!id) return json({ error: 'id required' }, { status: 400 });

	const qs = new URLSearchParams({ token });
	const result = await vpcRequest(vpcUrl, token, 'GET', `/api/web/mms/part/${encodeURIComponent(id)}?${qs}`);
	return json(result.data, { status: result.status });
};
