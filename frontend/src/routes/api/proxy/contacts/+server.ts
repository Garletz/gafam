import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

export const GET: RequestHandler = async ({ url }) => {
  const vpcUrl = url.searchParams.get('vpcUrl');
  const token = url.searchParams.get('token');
  const fingerprint = url.searchParams.get('certFingerprint');

  if (!vpcUrl || !token) {
    return json({ error: 'Missing vpcUrl or token' }, { status: 400 });
  }

  try {
    const targetUrl = `${vpcUrl.replace(/\/+$/, '')}/api/web/contacts`;
    
    const res = await fetch(targetUrl, {
      method: 'GET',
      headers: {
        'Authorization': `Bearer ${token}`
      }
    });

    if (!res.ok) {
      return json({ error: 'Upstream error' }, { status: res.status });
    }

    const data = await res.json();
    return json(data);
  } catch (err: any) {
    return json({ error: err.message }, { status: 500 });
  }
};
