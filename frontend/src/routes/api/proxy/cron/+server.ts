import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

// Cron proxy — scheduled Saṃyojaka missions on the VPC.
// GET    /api/proxy/cron?vpcUrl&token            → list jobs
// POST   /api/proxy/cron?vpcUrl&token            → create/upsert job
// DELETE /api/proxy/cron?vpcUrl&token&id=…       → delete job

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const result = await vpcRequest(vpcUrl, token, 'GET', `/api/web/cron?token=${encodeURIComponent(token)}`);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const body = await request.text();
	const result = await vpcRequest(vpcUrl, token, 'POST', `/api/web/cron?token=${encodeURIComponent(token)}`, body);
	return json(result.data, { status: result.status });
};

export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const id = url.searchParams.get('id') || '';
	if (!vpcUrl || !token || !id) return json({ error: 'Missing params' }, { status: 400 });

	const result = await vpcRequest(
		vpcUrl,
		token,
		'DELETE',
		`/api/web/cron?id=${encodeURIComponent(id)}&token=${encodeURIComponent(token)}`
	);
	return json(result.data, { status: result.status });
};
