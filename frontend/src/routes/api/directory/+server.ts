import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

// --- POST: VPC deposits an encrypted safe ---
export const POST: RequestHandler = async ({ request, platform }) => {
	try {
		const { phone, encrypted_safe, salt, iv, access_time } = await request.json();

		if (!phone || !encrypted_safe || !salt || !iv || !access_time) {
			return json({ error: 'Missing parameters' }, { status: 400 });
		}

		if (platform?.env?.DB) {
			// Create tables if they don't exist (migration-safe)
			await platform.env.DB.prepare(`
				CREATE TABLE IF NOT EXISTS directory_v2 (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					phone_number TEXT NOT NULL,
					encrypted_safe TEXT NOT NULL,
					salt TEXT NOT NULL,
					iv TEXT NOT NULL,
					access_time TEXT NOT NULL,
					consumed INTEGER DEFAULT 0,
					created_at DATETIME DEFAULT CURRENT_TIMESTAMP
				)
			`).run();

			await platform.env.DB.prepare(`
				CREATE TABLE IF NOT EXISTS rate_limits (
					id INTEGER PRIMARY KEY AUTOINCREMENT,
					ip_address TEXT NOT NULL,
					phone_number TEXT NOT NULL,
					attempts INTEGER DEFAULT 0,
					last_attempt_at DATETIME DEFAULT CURRENT_TIMESTAMP,
					UNIQUE(ip_address, phone_number)
				)
			`).run();

			// Insert the safe (real or honeypot — we don't distinguish)
			await platform.env.DB.prepare(
				`INSERT INTO directory_v2 (phone_number, encrypted_safe, salt, iv, access_time) VALUES (?, ?, ?, ?, ?)`
			).bind(phone, encrypted_safe, salt, iv, access_time).run();
		} else {
			console.warn('D1 database not found in platform.env');
		}

		return json({ success: true });
	} catch (e: any) {
		return json({ error: e.message }, { status: 500 });
	}
};

// --- GET: Web Client requests a safe with the correct time ---
export const GET: RequestHandler = async ({ url, platform, request }) => {
	const phone = url.searchParams.get('phone');
	const time = url.searchParams.get('time');

	if (!phone) return json({ error: 'Phone missing' }, { status: 400 });

	// If no time is provided, this is the legacy flow (backward compat during migration)
	// TODO: Remove this legacy block once all clients are updated
	if (!time) {
		return legacyGet(phone, platform);
	}

	if (!platform?.env?.DB) {
		return json({ error: 'DB not available' }, { status: 503 });
	}

	const clientIp = request.headers.get('cf-connecting-ip') || request.headers.get('x-forwarded-for') || 'unknown';

	// --- Rate Limiting ---
	// Ensure table exists
	await platform.env.DB.prepare(`
		CREATE TABLE IF NOT EXISTS rate_limits (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ip_address TEXT NOT NULL,
			phone_number TEXT NOT NULL,
			attempts INTEGER DEFAULT 0,
			last_attempt_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(ip_address, phone_number)
		)
	`).run();

	// Check rate limit
	const rateCheck = await platform.env.DB.prepare(
		`SELECT attempts, last_attempt_at FROM rate_limits WHERE ip_address = ? AND phone_number = ?`
	).bind(clientIp, phone).first<{ attempts: number; last_attempt_at: string }>();

	if (rateCheck) {
		const lastAttempt = new Date(rateCheck.last_attempt_at).getTime();
		const now = Date.now();
		const tenMinutes = 10 * 60 * 1000;

		if (now - lastAttempt < tenMinutes && rateCheck.attempts >= 3) {
			return json({ error: 'Too many attempts. Try again later.' }, { status: 429 });
		}

		// Reset counter if 10 minutes have passed
		if (now - lastAttempt >= tenMinutes) {
			await platform.env.DB.prepare(
				`UPDATE rate_limits SET attempts = 0, last_attempt_at = CURRENT_TIMESTAMP WHERE ip_address = ? AND phone_number = ?`
			).bind(clientIp, phone).run();
		}
	}

	// --- Look for a safe matching the phone + time ---
	const safe = await platform.env.DB.prepare(
		`SELECT id, encrypted_safe, salt, iv FROM directory_v2 WHERE phone_number = ? AND access_time = ? AND consumed = 0 ORDER BY created_at DESC LIMIT 1`
	).bind(phone, time).first<{ id: number; encrypted_safe: string; salt: string; iv: string }>();

	if (!safe) {
		// Wrong time or no safe available — increment rate limit
		await platform.env.DB.prepare(
			`INSERT INTO rate_limits (ip_address, phone_number, attempts, last_attempt_at) VALUES (?, ?, 1, CURRENT_TIMESTAMP)
			 ON CONFLICT(ip_address, phone_number) DO UPDATE SET attempts = attempts + 1, last_attempt_at = CURRENT_TIMESTAMP`
		).bind(clientIp, phone).run();

		return json({ success: false, reason: 'invalid_time' }, { status: 404 });
	}

	// --- Ephemeral: Mark as consumed ---
	await platform.env.DB.prepare(
		`UPDATE directory_v2 SET consumed = 1 WHERE id = ?`
	).bind(safe.id).run();

	return json({
		success: true,
		encrypted_safe: safe.encrypted_safe,
		salt: safe.salt,
		iv: safe.iv
	});
};

// --- Legacy GET (backward compatibility) ---
async function legacyGet(phone: string, platform: any) {
	if (platform?.env?.DB) {
		try {
			const { results } = await platform.env.DB.prepare(
				'SELECT vpc_url, session_token, cert_fingerprint FROM directory WHERE phone_number = ?'
			).bind(phone).all();

			if (results && results.length > 0) {
				const vpcUrl = results[0].vpc_url;
				const sessionToken = results[0].session_token;
				const certFingerprint = results[0].cert_fingerprint;

				if (sessionToken) {
					await platform.env.DB.prepare(
						'UPDATE directory SET session_token = NULL WHERE phone_number = ?'
					).bind(phone).run();

					return json({ success: true, vpcUrl, sessionToken, certFingerprint });
				} else {
					return json({ success: false, reason: 'token_consumed' }, { status: 404 });
				}
			}
		} catch (e) {
			// Old table might not exist, that's fine
		}
	}

	return json({ success: false }, { status: 404 });
}
