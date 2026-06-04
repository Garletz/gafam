import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const POST: RequestHandler = async ({ request, platform }) => {
	try {
		const { phone, session_token, port, cert_fingerprint } = await request.json();

		if (!phone || !session_token || !port) {
			return json({ error: 'Missing parameters' }, { status: 400 });
		}

		// Get the raw VPC IP from Cloudflare's trusted headers (no nip.io needed)
		const vpcIp = request.headers.get('cf-connecting-ip') || request.headers.get('x-forwarded-for') || '127.0.0.1';
		const vpcUrl = `http://${vpcIp}:${port}`;

		if (platform?.env?.DB) {
			await platform.env.DB.prepare(
				`INSERT INTO directory (phone_number, vpc_url, session_token, cert_fingerprint) 
				 VALUES (?, ?, ?, ?) 
				 ON CONFLICT(phone_number) 
				 DO UPDATE SET vpc_url=excluded.vpc_url, session_token=excluded.session_token, cert_fingerprint=excluded.cert_fingerprint, created_at=CURRENT_TIMESTAMP`
			)
				.bind(phone, vpcUrl, session_token, cert_fingerprint || null)
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
			'SELECT vpc_url, session_token, cert_fingerprint FROM directory WHERE phone_number = ?'
		).bind(phone).all();

		if (results && results.length > 0) {
			const vpcUrl = results[0].vpc_url;
			const sessionToken = results[0].session_token;
			const certFingerprint = results[0].cert_fingerprint;

			if (sessionToken) {
				// SECURITY FIX: Ephemeral Token.
				// As soon as the web client reads the token, we delete it from Cloudflare D1.
				// This ensures nobody else can connect by visiting the same domain later.
				await platform.env.DB.prepare(
					'UPDATE directory SET session_token = NULL WHERE phone_number = ?'
				).bind(phone).run();

				return json({ success: true, vpcUrl, sessionToken, certFingerprint });
			} else {
				// Token already consumed — session expired or already opened in another tab
				return json({ success: false, reason: 'token_consumed' }, { status: 404 });
			}
		}

		return json({ success: false }, { status: 404 });
	}

	return json({ error: 'DB not available' }, { status: 503 });
};
