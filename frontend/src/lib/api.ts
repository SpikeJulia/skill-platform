// Skill Platform API client
// 所有操作走 Go hook 自定义路由（同源，不需要 API key）

const BASE = '/api';

export interface Skill {
	name: string;
	description: string;
	path: string;
	hasSkillMD: boolean;
	// v0.2: agents 标识 skill 属于哪些 agent
	//   - ["all"] 表示中央源（所有 agent 都有）
	//   - ["minimax"] 等表示专属
	agents?: string[];
}

export interface SkillDetail {
	name: string;
	content: string;
	path: string;
	hasSkillMD: boolean;
}

export interface SkillMatch {
	name: string;
	reason: string;
}

export interface FindResponse {
	query: string;
	matches: SkillMatch[];
	agent_output?: string;
}

export interface AgentResponse {
	operation: string;
	result: string;
	affected: string[];
	target_path?: string;
}

// v0.4: agent 的模型与提示词配置
export type ApiFormat = 'anthropic' | 'openai_responses';

// ⚠️ 这里没有 api_key 字段——后端从设计上就不回显明文。
// 只有 has_api_key（有没有配）和 api_key_masked（打码）两个字段。
export interface LLMConfigView {
	provider: string;
	api_format: ApiFormat;
	base_url: string;
	has_api_key: boolean;
	api_key_masked: string;
	headers: Record<string, string>;
	model: string;
	context_window: number;
	max_output_tokens: number;
	reasoning_effort: string;
	global_prompt: string;
	default_global_prompt: string;
	supported_formats: ApiFormat[];
}

// PUT 请求体。api_key 空 = 不改；传 '__clear__' = 清除
export interface LLMConfigInput {
	provider?: string;
	api_format?: ApiFormat;
	base_url?: string;
	api_key?: string;
	headers?: Record<string, string>;
	model?: string;
	context_window?: number;
	max_output_tokens?: number;
	reasoning_effort?: string;
	global_prompt?: string;
}

export interface APIFormatInfo {
	id: ApiFormat;
	label: string;
	endpoint: string;
	note: string;
	auth_default: string;
}

// 平台管不到、但确实存在的 skill（由宿主 sync.sh 扫描生成清单）
// kind: "local" = ~/.<agent>/skills/ 里的真目录，可纳管
//       "builtin" = agent 自带的，会自动更新，不该纳管
export interface Unmanaged {
	agent: string;
	name: string;
	kind: 'local' | 'builtin';
	path: string;
	description: string;
}

// v0.2.1: Agent tab（来自 PB agent_tabs collection + 系统固定的 "all"）
export interface Agent {
	id?: string;
	name: string;
	label: string;
	sort_order: number;
	created_at?: string;
}

export interface CreateAgentBody {
	name: string;
	label?: string;
	sort_order?: number;
}

export interface UpdateAgentBody {
	label?: string;
	sort_order?: number;
}

async function request<T>(path: string, options?: RequestInit): Promise<T> {
	const res = await fetch(BASE + path, {
		headers: { 'Content-Type': 'application/json' },
		...options
	});
	if (!res.ok) {
		const text = await res.text();
		throw new Error(`${res.status} ${res.statusText}: ${text}`);
	}
	return res.json();
}

export const api = {
	// ===== Skills CRUD =====
	listSkills: () => request<{ skills: Skill[]; count: number }>('/skills'),
	getSkill: (name: string) => request<SkillDetail>(`/skills/${encodeURIComponent(name)}`),
	deleteSkill: (name: string) =>
		request<{ deleted: string; path: string; note: string }>(`/skills/${encodeURIComponent(name)}`, {
			method: 'DELETE'
		}),

	// ===== 未纳管（平台管不到但存在的 skill）=====
	// 清单不存在时后端返回空列表，不会报错
	listUnmanaged: () => request<{ unmanaged: Unmanaged[]; count: number }>('/unmanaged'),

	// ===== Install =====
	// agent 空 = 装到中央源（所有 agent 共享）；给值 = 只装到该 agent 的专属库。
	// 主页的「安装到 X」按钮会把当前 tab 传过来。
	installContent: (name: string, content: string, agent?: string) =>
		request<{ installed: string; path: string; target: string; sync: string }>('/install/content', {
			method: 'POST',
			body: JSON.stringify({ name, content, agent: agent || undefined })
		}),
	installURL: (url: string, name?: string, agent?: string) =>
		request<{ installed: string; url: string; target: string; agent_output: string }>(
			'/install/url',
			{
				method: 'POST',
				body: JSON.stringify({ url, name, agent: agent || undefined })
			}
		),

	// ===== Find =====
	find: (query: string, topK = 3) =>
		request<FindResponse>('/find', {
			method: 'POST',
			body: JSON.stringify({ query, topK })
		}),

	// ===== Agent ops =====
	compose: (skills: string[], prompt = '') =>
		request<AgentResponse>('/agent/compose', {
			method: 'POST',
			body: JSON.stringify({ skills, prompt })
		}),
	orchestrate: (skills: string[], prompt = '') =>
		request<AgentResponse>('/agent/orchestrate', {
			method: 'POST',
			body: JSON.stringify({ skills, prompt })
		}),
	merge: (skills: string[], target: string, prompt = '') =>
		request<AgentResponse>('/agent/merge', {
			method: 'POST',
			body: JSON.stringify({ skills, target, prompt })
		}),
	evolve: (skill: string, prompt: string) =>
		request<AgentResponse>('/agent/evolve', {
			method: 'POST',
			body: JSON.stringify({ skills: [skill], prompt })
		}),

	// ===== Agents (v0.2.1) =====
	listAgents: () => request<{ agents: Agent[]; count: number }>('/agents'),
	createAgent: (body: CreateAgentBody) =>
		request<Agent>('/agents', {
			method: 'POST',
			body: JSON.stringify(body)
		}),
	updateAgent: (name: string, body: UpdateAgentBody) =>
		request<Agent>(`/agents/${encodeURIComponent(name)}`, {
			method: 'PATCH',
			body: JSON.stringify(body)
		}),
	deleteAgent: (name: string) =>
		request<{ deleted: string; note: string }>(`/agents/${encodeURIComponent(name)}`, {
			method: 'DELETE'
		}),

	// ===== Agent 配置 (v0.4) =====
	getConfig: () => request<LLMConfigView>('/config'),
	putConfig: (body: LLMConfigInput) =>
		request<{ config: LLMConfigView; test: string; saved_at: string }>('/config', {
			method: 'PUT',
			body: JSON.stringify(body)
		}),
	resetConfig: () => request<{ config: LLMConfigView }>('/config/reset', { method: 'POST' }),
	listAPIFormats: () => request<{ formats: APIFormatInfo[] }>('/config/formats')
};
