import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ url }) => {
	const vpcUrl = url.searchParams.get('vpcUrl');
	const token = url.searchParams.get('token');
	if (!vpcUrl || !token) return json({ error: 'Missing params' }, { status: 400 });

	try {
		const fetchUrl = `${vpcUrl}/api/web/sms?token=${token}`;
		const response = await fetch(fetchUrl);
		if (!response.ok) {
			return json({ error: 'VPC error' }, { status: response.status });
		}
		const data = await response.json();
		return json(data);
	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
};
