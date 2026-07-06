import { json } from '@sveltejs/kit';

export const GET = async ({ request }) => {
    try {
        const res = await fetch('http://165-245-249-166.nip.io:5150/api/_ping');
        return json({ success: true, status: res.status });
    } catch (e: any) {
        return json({ success: false, error: e.message });
    }
};
