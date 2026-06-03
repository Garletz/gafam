import type { Reroute } from '@sveltejs/kit';

export const reroute: Reroute = ({ url }) => {
	const host = url.hostname;
	
	// Si on est sur un sous-domaine (ex: 0611223344.gafam.cloud)
	// et pas sur le domaine principal ni localhost
	if (host !== 'gafam.cloud' && !host.includes('localhost') && host.endsWith('.gafam.cloud')) {
		const subdomain = host.split('.')[0];
		
		// Si c'est un numéro (ou un alias alphanumérique simple)
		if (subdomain && subdomain !== 'www') {
			// Ne pas réécrire les appels API
			if (url.pathname.startsWith('/api/')) {
				return;
			}
			
			// On réécrit l'URL en interne pour pointer vers la route /[phone]
			if (url.pathname === '/') {
				return `/${subdomain}`;
			} else if (!url.pathname.startsWith(`/${subdomain}`)) {
				return `/${subdomain}${url.pathname}`;
			}
		}
	}
};
