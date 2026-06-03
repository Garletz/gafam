import type { PageServerLoad } from './$types';

export const load: PageServerLoad = async ({ params, platform }) => {
	const phone = params.phone;
	let savedVpcUrl = null;
	let sessionToken = null;

	if (platform?.env?.DB) {
		const { results } = await platform.env.DB.prepare(
			'SELECT vpc_url, session_token FROM directory WHERE phone_number = ?'
		)
			.bind(phone)
			.all();

		if (results && results.length > 0) {
			savedVpcUrl = results[0].vpc_url;
			sessionToken = results[0].session_token;
		}
	}

	return {
		phone,
		savedVpcUrl,
		sessionToken
	};
};
