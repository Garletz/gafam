// Shared AES-GCM helpers (same scheme as the relay session: SHA-256(secret) → AES-GCM).
// Used for the encrypted /api/web/settings channel.

export async function deriveKey(secret: string): Promise<CryptoKey> {
	const enc = new TextEncoder();
	const hashBuffer = await window.crypto.subtle.digest('SHA-256', enc.encode(secret));
	return window.crypto.subtle.importKey('raw', hashBuffer, { name: 'AES-GCM' }, false, [
		'encrypt',
		'decrypt'
	]);
}

function base64ToArrayBuffer(base64: string): ArrayBuffer {
	const binaryString = window.atob(base64);
	const bytes = new Uint8Array(binaryString.length);
	for (let i = 0; i < binaryString.length; i++) {
		bytes[i] = binaryString.charCodeAt(i);
	}
	return bytes.buffer;
}

function arrayBufferToBase64(buffer: ArrayBuffer): string {
	let binary = '';
	const bytes = new Uint8Array(buffer);
	for (let i = 0; i < bytes.byteLength; i++) {
		binary += String.fromCharCode(bytes[i]);
	}
	return window.btoa(binary);
}

export async function decryptAESGCM(
	encryptedBase64: string,
	ivBase64: string,
	secret: string
): Promise<string> {
	const key = await deriveKey(secret);
	const iv = base64ToArrayBuffer(ivBase64);
	const ciphertext = base64ToArrayBuffer(encryptedBase64);
	const decrypted = await window.crypto.subtle.decrypt(
		{ name: 'AES-GCM', iv: new Uint8Array(iv) },
		key,
		ciphertext
	);
	return new TextDecoder().decode(decrypted);
}

export async function encryptAESGCM(
	plaintext: string,
	secret: string
): Promise<{ encrypted_data: string; iv: string }> {
	const key = await deriveKey(secret);
	const iv = window.crypto.getRandomValues(new Uint8Array(12));
	const encoded = new TextEncoder().encode(plaintext);
	const ciphertext = await window.crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, encoded);
	return {
		encrypted_data: arrayBufferToBase64(ciphertext),
		iv: arrayBufferToBase64(iv.buffer as ArrayBuffer)
	};
}
