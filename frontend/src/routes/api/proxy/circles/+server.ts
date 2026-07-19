import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const circle = url.searchParams.get('circle');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const tag = url.searchParams.get('tag');
	if (tag === 'list') {
		const vpcPath = `/api/web/circles?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
		return json(result.data, { status: result.status });
	}

	if (circle) {
		const vpcPath = `/api/web/circles/${encodeURIComponent(circle)}?token=${encodeURIComponent(token)}`;
		const result = await vpcRequest(vpcUrl, token, 'GET', vpcPath);
		return json(result.data, { status: result.status });
	}

	return json({ error: 'Missing circle or tag=list' }, { status: 400 });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	const body = await request.text();
	const vpcPath = `/api/web/circles?token=${encodeURIComponent(token)}`;
	const result = await vpcRequest(vpcUrl, token, 'POST', vpcPath, body);
	return json(result.data, { status: result.status });
};

export const DELETE: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const name = url.searchParams.get('name');
	if (!vpcUrl || !token || !name) return json({ error: 'Missing params' }, { status: 400 });

	const vpcPath = `/api/web/circles?token=${encodeURIComponent(token)}&name=${encodeURIComponent(name)}`;
	const result = await vpcRequest(vpcUrl, token, 'DELETE', vpcPath);
	return json(result.data, { status: result.status });
};
