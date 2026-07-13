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
