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
	installContent: (name: string, content: string) =>
		request<{ installed: string; path: string; sync: string }>('/install/content', {
			method: 'POST',
			body: JSON.stringify({ name, content })
		}),
	installURL: (url: string, name?: string) =>
		request<{ installed: string; url: string; agent_output: string }>('/install/url', {
			method: 'POST',
			body: JSON.stringify({ url, name })
		}),

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
		})
};
