import { api, type Agent } from '$lib/api';

/**
 * agent 标签的全局唯一数据源。
 *
 * 之前主页 tab 栏（+page.svelte）和管理 Dialog（+layout.svelte → AgentManagerDialog）
 * 各自维护一份 agents，改顺序后只刷新了 Dialog 那份，主页 tab 栏要手动刷新页面才更新。
 * 抽成 rune 模块后所有调用方共享同一份状态，任何一处 loadAgents() 全局同步。
 *
 * 用 `$state({...})` 包一层而不是 `export const agents = $state([])`：
 * 后者是 const，不能重新赋值（svelte-check 会报 "Cannot assign to ... constant"）。
 */
export const agentsState = $state<{ items: Agent[] }>({ items: [] });

export async function loadAgents() {
	try {
		const res = await api.listAgents();
		agentsState.items = res.agents;
	} catch (e) {
		console.warn('loadAgents failed:', e);
		agentsState.items = [{ name: 'all', label: '全部', sort_order: 0 }];
	}
}
