import { json } from '@sveltejs/kit';
import type { RequestHandler } from './$types';

type WorkflowRuns = {
	workflow_runs: Array<{
		head_sha: string;
		updated_at: string;
	}>;
};

const GH_HEADERS = {
	Accept: 'application/vnd.github+json',
	'User-Agent': 'gafam-relay-worker'
};

// Last known good docker-publish on main (fallback when GitHub rate-limits the Worker).
const FALLBACK_SHA = '50660a54064ae70a2dfd3323d72a29d06f1f267d';

async function latestDockerPublishSha(): Promise<{
	git_sha: string;
	published_at: string;
	source: string;
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

		// Never 502 — GH rate limit from Cloudflare is common; use static fallback so Settings UI works.
		return json({
			repo: 'Garletz/gafam',
			branch: 'main',
			git_sha: FALLBACK_SHA,
			git_sha_short: FALLBACK_SHA.slice(0, 7),
			published_at: new Date().toISOString(),
			image: 'ghcr.io/garletz/gafam:latest',
			source: 'rate_limit_fallback'
		});
	} catch {
		return json({
			repo: 'Garletz/gafam',
			branch: 'main',
			git_sha: FALLBACK_SHA,
			git_sha_short: FALLBACK_SHA.slice(0, 7),
			published_at: new Date().toISOString(),
			image: 'ghcr.io/garletz/gafam:latest',
			source: 'error_fallback'
		});
	}
};
