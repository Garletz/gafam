import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, platform }) => {
	try {
		const { phone, session_token, port } = await request.json();

		if (!phone || !session_token || !port) {
			return json({ error: 'Missing parameters' }, { status: 400 });
		}

		const vpcIp = request.headers.get('cf-connecting-ip') || request.headers.get('x-forwarded-for') || '127.0.0.1';
		const vpcUrl = `http://${vpcIp}:${port}`;

		if (platform?.env?.DB) {
			await platform.env.DB.prepare(
				`INSERT INTO directory (phone_number, vpc_url, session_token) 
				 VALUES (?, ?, ?) 
				 ON CONFLICT(phone_number) 
				 DO UPDATE SET vpc_url=excluded.vpc_url, session_token=excluded.session_token, created_at=CURRENT_TIMESTAMP`
			)
				.bind(phone, vpcUrl, session_token)
				.run();
		} else {
			console.warn('D1 database not found in platform.env');
		}

		return json({ success: true, vpcUrl });
	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
};

export const GET: RequestHandler = async ({ url, platform }) => {
	const phone = url.searchParams.get('phone');
	if (!phone) return json({ error: 'Phone missing' }, { status: 400 });

	if (platform?.env?.DB) {
		const { results } = await platform.env.DB.prepare(
			'SELECT vpc_url, session_token FROM directory WHERE phone_number = ?'
		).bind(phone).all();

		if (results && results.length > 0) {
			return json({ 
				success: true, 
				vpcUrl: results[0].vpc_url,
				sessionToken: results[0].session_token 
			});
		}
	}
	return json({ success: false, error: 'Not found' }, { status: 404 });
};
