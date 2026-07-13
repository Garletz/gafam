export type LogDay = { day: string; bytes: number; lines: number; updated_at: string };

export type SuparnaReading = {
	day: string;
	summary: string;
	timeline?: Array<{ time: string; app: string; event: string; severity: string }>;
	alerts?: Array<{ type: string; detail: string; codes?: string[] }>;
	confidence?: string;
	engine?: string;
	model_ready?: boolean;
};

export type SuparnaStatus = {
	model_on_disk?: boolean;
	model_path?: string;
	qwen_running?: boolean;
	analyze_running?: boolean;
	qwen_url?: string;
	auto_stop?: boolean;
};

export type SuparnaRules = {
	autoStopMinutes: number;
	preferVpcForSmallTasks: boolean;
	showTimeline: boolean;
};

export const DEFAULT_SUPARNA_RULES: SuparnaRules = {
	autoStopMinutes: 5,
	preferVpcForSmallTasks: true,
	showTimeline: true
};

export type EdgeStatus = {
	phone_reachable: boolean;
	edge_ready: boolean;
	edge_service: string;
	scrcpy_blocking: boolean;
	ram_reserved_mb: number;
	model_on_device: boolean;
	phase?: string;
};

export type EdgeInferResult = {
	content?: string;
	engine?: string;
	tier_used?: string;
	ram_peak_mb?: number;
	latency_ms?: number;
	status?: string;
	error?: string;
	prompt?: string;
};

export type ModelCatalogEntry = {
	id: string;
	name: string;
	file: string;
	sizeMb: number;
	ramMinMb: number;
	context: number;
	tier: 'vpc' | 'phone' | 'both';
	status: 'installed' | 'available' | 'planned';
	notes: string;
};

export const MODEL_CATALOG: ModelCatalogEntry[] = [
	{
		id: 'qwen3-0.6b-q4',
		name: 'Qwen3 0.6B Q4_K_M',
		file: 'Qwen3-0.6B-Q4_K_M.gguf',
		sizeMb: 380,
		ramMinMb: 520,
		context: 2048,
		tier: 'vpc',
		status: 'installed',
		notes: 'VPC 1 RAM mode — micro logs & SMS'
	},
	{
		id: 'qwen3-1.5b-q4',
		name: 'Qwen3 1.5B Q4_K_M',
		file: 'Qwen3-1.5B-Q4_K_M.gguf',
		sizeMb: 980,
		ramMinMb: 1200,
		context: 4096,
		tier: 'phone',
		status: 'planned',
		notes: 'Phone deep tier — full day logs (Phase 3)'
	}
];

export const EDGE_QUICK_PROMPTS = [
	{ label: '1+1=?', prompt: '1+1=?' },
	{ label: '2×3=?', prompt: 'What is 2 times 3?' },
	{ label: 'Bonjour', prompt: 'Say hello in one short sentence.' }
] as const;
