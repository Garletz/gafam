import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, platform }) => {
	const phone = params.phone;
	let savedVpcUrl = null;

	if (platform?.env?.DB) {
		// NOTE: session_token is deliberately NOT selected — the load function
		// runs for every visitor of /{phone}; serving a bearer token here would
		// bypass the challenge flow entirely. Only the (non-secret) VPC URL is.
		const { results } = await platform.env.DB.prepare(
			'SELECT vpc_url FROM directory WHERE phone_number = ?'
		)
			.bind(phone)
			.all();

		if (results && results.length > 0) {
			savedVpcUrl = results[0].vpc_url;
		}
	}

	return {
		phone,
		savedVpcUrl,
		sessionToken: null
	};
};
