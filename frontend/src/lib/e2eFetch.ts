// E2E transport encryption for VPC proxy calls.
// Wraps fetch() to add X-GAFAM-E2E: 1 header and transparently encrypt/decrypt
// the request/response body using AES-256-GCM with the session token as key.
//
// Usage: const data = await e2eFetch('/api/proxy/sms?${params}');
//        const data = await e2eFetch('/api/proxy/sms?${params}', { method: 'POST', body: JSON.stringify(payload) });

import { encryptAESGCM, decryptAESGCM } from './crypto';

interface E2EOptions extends RequestInit {
	skipE2E?: boolean;
}

export async function e2eFetch(url: string, sessionToken: string, options: E2EOptions = {}): Promise<any> {
	const useE2E = sessionToken.length > 0 && !options.skipE2E;

	if (useE2E) {
		options.headers = {
			...options.headers,
			'X-GAFAM-E2E': '1',
			'Content-Type': 'application/json'
		};
		if (options.body && typeof options.body === 'string') {
			const envelope = await encryptAESGCM(options.body, sessionToken);
			options.body = JSON.stringify(envelope);
		}
	}

	const res = await fetch(url, options);
	const data = await res.json();

	if (useE2E && data.encrypted_data && data.iv) {
		const plaintext = await decryptAESGCM(data.encrypted_data, data.iv, sessionToken);
		try {
			return JSON.parse(plaintext);
		} catch {
			return plaintext;
		}
	}

	return data;
}
