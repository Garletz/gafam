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

function unavailable(source: string) {
	// No hardcoded SHA — Settings must treat this as "unknown", not "new build".
	return json({
		repo: 'Garletz/gafam',
		branch: 'main',
		git_sha: null,
		git_sha_short: null,
		published_at: null,
		image: 'ghcr.io/garletz/gafam:latest',
		source,
		available: false
	});
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
				source: docker.source,
				available: true
			});
		}
		return unavailable('rate_limit');
	} catch {
		return unavailable('error');
	}
};
