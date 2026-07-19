// Shared VPC proxy helper used by /api/proxy/* routes.
// Supports transparent E2E passthrough (X-GAFAM-E2E header).

export interface VpcResult {
	ok: boolean;
	status: number;
	data: any;
}

export async function vpcRequest(
	vpcUrl: string,
	token: string,
	method: 'GET' | 'POST' | 'PUT' | 'DELETE',
	pathAndQuery: string,
	body?: string,
	e2e?: boolean
): Promise<VpcResult> {
	const parsed = new URL(vpcUrl);
	const host = parsed.hostname;
	const port = parseInt(parsed.port) || 5150;

	try {
		const { connect } = await import(/* @vite-ignore */ 'cloudflare:sockets');
		const socket = connect(`${host}:${port}`);
		const writer = socket.writable.getWriter();
		const encoder = new TextEncoder();

		const headers = [
			`${method} ${pathAndQuery} HTTP/1.1`,
			`Host: ${host}`,
			`Authorization: Bearer ${token}`,
			`Connection: close`
		];
		if (e2e) headers.push('X-GAFAM-E2E: 1');
		if (body) {
			const bodyBytes = new TextEncoder().encode(body).length;
			headers.push('Content-Type: application/json');
			headers.push(`Content-Length: ${bodyBytes}`);
		}
		headers.push('', '');

		const requestText = body ? headers.join('\r\n') + body : headers.join('\r\n');
		await writer.write(encoder.encode(requestText));
		writer.releaseLock();

		const reader = socket.readable.getReader();
		const decoder = new TextDecoder();
		let rawResponse = '';

		while (true) {
			const { done, value } = await reader.read();
			if (done) break;
			rawResponse += decoder.decode(value, { stream: true });
		}

		const bodyStart = rawResponse.indexOf('\r\n\r\n');
		if (bodyStart === -1) {
			return { ok: false, status: 502, data: { error: 'Malformed response' } };
		}

		const statusLine = rawResponse.split('\r\n')[0];
		const statusCode = parseInt(statusLine.split(' ')[1] || '500');
		const responseBody = rawResponse.slice(bodyStart + 4).trim();

		try {
			return { ok: statusCode < 400, status: statusCode, data: responseBody ? JSON.parse(responseBody) : {} };
		} catch {
			return { ok: false, status: statusCode, data: { error: responseBody || 'Upstream error' } };
		}
	} catch (socketError: any) {
		try {
			const fetchHeaders: Record<string, string> = { Authorization: `Bearer ${token}` };
			if (e2e) fetchHeaders['X-GAFAM-E2E'] = '1';
			if (body) fetchHeaders['Content-Type'] = 'application/json';

			const response = await fetch(`${vpcUrl}${pathAndQuery}`, { method, headers: fetchHeaders, body });
			const data = await response.json();
			return { ok: response.ok, status: response.status, data };
		} catch {
			return { ok: false, status: 500, data: { error: socketError.message } };
		}
	}
}
