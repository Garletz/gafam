import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';
import { vpcRequest } from '$lib/vpcProxy';

// Dakṣiṇā proxy — self-improvement runner (Manifest 27).
// GET    /api/proxy/daksina?vpcUrl&token&action=status|runs|run|settings|sessions|session
// POST   /api/proxy/daksina?vpcUrl&token&action=run|settings|wake|stop|import-session
// DELETE /api/proxy/daksina?vpcUrl&token&action=run&id=…

function basePath(action: string): string | null {
	switch (action) {
		case 'status':
			return '/api/web/daksina/status';
		case 'runs':
			return '/api/web/daksina/runs';
		case 'settings':
			return '/api/web/daksina/settings';
		case 'sessions':
			return '/api/web/daksina/sessions';
		case 'wake':
			return '/api/web/daksina/wake';
		case 'stop':
			return '/api/web/daksina/stop';
		default:
			return null;
	}
}

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || 'status';
	const id = url.searchParams.get('id') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let path = basePath(action);
	if (action === 'run' && id) path = `/api/web/daksina/runs/${encodeURIComponent(id)}`;
	if (action === 'session' && id) path = `/api/web/daksina/sessions/${encodeURIComponent(id)}`;
	if (!path) return json({ error: 'Unknown action' }, { status: 400 });

	const result = await vpcRequest(vpcUrl, token, 'GET', `${path}?token=${encodeURIComponent(token)}`);
	return json(result.data, { status: result.status });
};

export const POST: RequestHandler = async ({ url, request }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	const action = url.searchParams.get('action') || '';
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	let path = basePath(action);
	if (action === 'import-session') path = '/api/web/daksina/sessions';
	if (!path) return json({ error: 'Unknown action' }, { status: 400 });

	const body = await request.text();
	const result = await vpcRequest(
		vpcUrl,
		token,
		'POST',
		`${path}?token=${encodeURIComponent(token)}`,
		body,
		true
	);
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
		`/api/web/daksina/runs/${encodeURIComponent(id)}?token=${encodeURIComponent(token)}`
	);
	return json(result.data, { status: result.status });
};
