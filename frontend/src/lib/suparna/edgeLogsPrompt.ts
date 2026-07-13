/** VPC edge infer rejects prompts longer than 2000 chars. */
export const EDGE_INFER_PROMPT_MAX = 2000;

export type PhoneLogEntry = {
	ts: number;
	source: string;
	level: string;
	tag: string;
	message: string;
};

const NOISE_RE =
	/removeInvalidNode|junk list|HwViewRootImpl|Choreographer|libPerfCtl|OpenGLRenderer|InputTransport|BufferQueue|SurfaceFlinger|gralloc|GraphicBuffer/i;

function isNoise(e: PhoneLogEntry): boolean {
	return NOISE_RE.test(`${e.tag} ${e.message}`);
}

function scoreEntry(e: PhoneLogEntry): number {
	if (isNoise(e)) return -1000;
	let score = 0;
	const tag = e.tag.toLowerCase();
	const msg = e.message.toLowerCase();
	if (tag.includes('gafam') || msg.includes('gafam')) score += 12;
	if (tag.includes('edge') || msg.includes('edge') || msg.includes('infer')) score += 10;
	if (tag.includes('sms') || msg.includes('sms')) score += 10;
	if (e.level === 'E' || e.level === 'W') score += 6;
	if (e.level === 'I') score += 1;
	return score;
}

function formatLine(e: PhoneLogEntry): string {
	const t = new Date(e.ts).toISOString().slice(11, 19);
	const msg = e.message.length > 100 ? `${e.message.slice(0, 97)}…` : e.message;
	return `${t} ${e.tag}: ${msg}`;
}

function buildHeader(day: string): string {
	return (
		`Summarize this phone log (${day}) in 2–3 short bullet points. ` +
		`Focus on GAFAM, edge inference, SMS, and errors. Be factual.\n\n---\n`
	);
}

export type TodayLogsPromptResult = {
	prompt: string;
	displayLabel: string;
	selectedLines: number;
	totalLines: number;
};

/** Pick the most relevant recent log lines and stay under the infer prompt cap. */
export function buildTodayLogsPrompt(
	entries: PhoneLogEntry[],
	day: string,
	totalLines?: number
): TodayLogsPromptResult | null {
	if (entries.length === 0) return null;

	const header = buildHeader(day);
	const bodyBudget = EDGE_INFER_PROMPT_MAX - header.length;
	if (bodyBudget < 200) return null;

	const scored = entries
		.map((e, idx) => ({ e, score: scoreEntry(e), idx }))
		.filter(({ score }) => score > -1000);

	let pool = scored.filter(({ score }) => score > 0);
	if (pool.length < 8) {
		pool = scored.length > 0 ? scored : entries.map((e, idx) => ({ e, score: 0, idx }));
	}

	pool.sort((a, b) => b.score - a.score || b.e.ts - a.e.ts || a.idx - b.idx);

	const picked: PhoneLogEntry[] = [];
	let used = 0;
	for (const { e } of pool) {
		const line = formatLine(e);
		const extra = picked.length > 0 ? 1 : 0;
		if (used + extra + line.length > bodyBudget) continue;
		picked.push(e);
		used += extra + line.length;
		if (picked.length >= 28) break;
	}

	if (picked.length === 0) {
		const fallback = entries.slice(0, 12);
		for (const e of fallback) {
			const line = formatLine(e);
			const extra = picked.length > 0 ? 1 : 0;
			if (used + extra + line.length > bodyBudget) break;
			picked.push(e);
			used += extra + line.length;
		}
	}
	if (picked.length === 0) return null;

	picked.sort((a, b) => b.ts - a.ts);
	const body = picked.map(formatLine).join('\n');
	const prompt = header + body;
	const total = totalLines ?? entries.length;

	return {
		prompt: prompt.length > EDGE_INFER_PROMPT_MAX ? prompt.slice(0, EDGE_INFER_PROMPT_MAX) : prompt,
		displayLabel: `Today logs (${day}, ${picked.length}/${total} lines)`,
		selectedLines: picked.length,
		totalLines: total
	};
}
