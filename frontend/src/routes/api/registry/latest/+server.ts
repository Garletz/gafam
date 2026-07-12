import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

type GitHubCommit = {
	sha: string;
	commit: { committer: { date: string } };
};

export const GET: RequestHandler = async () => {
	try {
		const res = await fetch('https://api.github.com/repos/Garletz/gafam/commits/main', {
			headers: {
				Accept: 'application/vnd.github+json',
				'User-Agent': 'gafam-relay-worker'
			}
		});

		if (!res.ok) {
			return json(
				{ error: 'github_unreachable', status: res.status },
				{ status: 502 }
			);
		}

		const data = (await res.json()) as GitHubCommit;
		return json({
			repo: 'Garletz/gafam',
			branch: 'main',
			git_sha: data.sha,
			git_sha_short: data.sha.slice(0, 7),
			published_at: data.commit.committer.date,
			image: 'ghcr.io/garletz/gafam:latest'
		});
	} catch (err: any) {
		return json({ error: err.message }, { status: 500 });
	}
};
