import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

type GitHubCommit = {
	sha: string;
	commit: { committer: { date: string } };
};

type WorkflowRuns = {
	workflow_runs: Array<{
		head_sha: string;
		head_branch: string;
		updated_at: string;
		conclusion: string | null;
	}>;
};

const GH_HEADERS = {
	Accept: 'application/vnd.github+json',
	'User-Agent': 'gafam-relay-worker'
};

async function latestDockerPublishSha(): Promise<{
	git_sha: string;
	published_at: string;
	source: 'docker_publish' | 'main_fallback';
} | null> {
	const runsRes = await fetch(
		'https://api.github.com/repos/Garletz/gafam/actions/workflows/docker-publish.yml/runs?status=success&branch=main&per_page=1',
		{ headers: GH_HEADERS }
	);
	if (!runsRes.ok) return null;

	const data = (await runsRes.json()) as WorkflowRuns;
	const run = data.workflow_runs?.[0];
	if (!run?.head_sha) return null;

	return {
		git_sha: run.head_sha,
		published_at: run.updated_at,
		source: 'docker_publish'
	};
}

async function latestMainCommit(): Promise<{ git_sha: string; published_at: string } | null> {
	const res = await fetch('https://api.github.com/repos/Garletz/gafam/commits/main', {
		headers: GH_HEADERS
	});
	if (!res.ok) return null;

	const data = (await res.json()) as GitHubCommit;
	return {
		git_sha: data.sha,
		published_at: data.commit.committer.date
	};
}

export const GET: RequestHandler = async () => {
	try {
		const docker = await latestDockerPublishSha();
		if (docker) {
			return json({
				repo: 'Garletz/gafam',
				branch: 'main',
				git_sha: docker.git_sha,
				git_sha_short: docker.git_sha.slice(0, 7),
				published_at: docker.published_at,
				image: 'ghcr.io/garletz/gafam:latest',
				source: docker.source
			});
		}

		const main = await latestMainCommit();
		if (!main) {
			// Fallback if GitHub rate-limits us (common on Cloudflare Workers without auth)
			return json({
				repo: 'Garletz/gafam',
				branch: 'main',
				git_sha: '0000000000000000000000000000000000000000',
				git_sha_short: '0000000',
				published_at: new Date().toISOString(),
				image: 'ghcr.io/garletz/gafam:latest',
				source: 'rate_limit_fallback'
			});
		}

		return json({
			repo: 'Garletz/gafam',
			branch: 'main',
			git_sha: main.git_sha,
			git_sha_short: main.git_sha.slice(0, 7),
			published_at: main.published_at,
			image: 'ghcr.io/garletz/gafam:latest',
			source: 'main_fallback'
		});
	} catch (err: any) {
		return json({ error: err.message }, { status: 500 });
	}
};
