<script lang="ts">
	import { onMount } from 'svelte';
	import { page } from '$app/state';
	import { api, type SkillDetail, type AgentResponse } from '$lib/api';
	import { Card } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Badge } from '$lib/components/ui/badge';
	import { Separator } from '$lib/components/ui/separator';
	import { ArrowLeft, Trash2, FolderInput, Sparkles, Layers, Workflow, GitMerge, Copy, Check } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { marked } from 'marked';
	import MoveSkillDialog from '$lib/components/MoveSkillDialog.svelte';

	let skill = $state<SkillDetail | null>(null);
	let error = $state('');
	let loading = $state(true);
	let moveOpen = $state(false);

	// Agent ops state
	let agentResult = $state<AgentResponse | null>(null);
	let agentLoading = $state(false);
	let agentPrompt = $state('');
	let agentOp = $state<'evolve' | 'compose' | 'orchestrate' | 'merge'>('evolve');
	let mergeTarget = $state('');
	let selectedForAgent = $state<Set<string>>(new Set());

	let copied = $state(false);

	async function load() {
		try {
			loading = true;
			const name = decodeURIComponent(page.params.name);
			skill = await api.getSkill(name);
			error = '';
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	function renderMarkdown(content: string): string {
		return marked.parse(content, { async: false }) as string;
	}

	async function copyPath() {
		if (skill) {
			await navigator.clipboard.writeText(skill.path);
			copied = true;
			setTimeout(() => (copied = false), 1500);
		}
	}

	async function uninstall() {
		if (!skill) return;
		if (!confirm(`确定卸载 "${skill.name}"?`)) return;
		try {
			await api.deleteSkill(skill.name);
			goto('/');
		} catch (e) {
			alert('卸载失败: ' + (e as Error).message);
		}
	}

	async function runAgent() {
		if (!skill) return;
		agentLoading = true;
		agentResult = null;
		try {
			const prompt = agentPrompt;
			if (agentOp === 'evolve') {
				if (!prompt) {
					alert('请填写进化方向');
					agentLoading = false;
					return;
				}
				agentResult = await api.evolve(skill.name, prompt);
			} else if (agentOp === 'compose') {
				const others = Array.from(selectedForAgent);
				if (others.length === 0) {
					alert('compose 需要选其他 skill（先在主页勾选）');
					agentLoading = false;
					return;
				}
				agentResult = await api.compose([skill.name, ...others], prompt);
			} else if (agentOp === 'orchestrate') {
				const others = Array.from(selectedForAgent);
				agentResult = await api.orchestrate([skill.name, ...others], prompt);
			} else if (agentOp === 'merge') {
				if (!mergeTarget) {
					alert('请填写合并后的新 skill 名');
					agentLoading = false;
					return;
				}
				const others = Array.from(selectedForAgent);
				agentResult = await api.merge([skill.name, ...others], mergeTarget, prompt);
			}
		} catch (e) {
			alert('LLM 操作失败: ' + (e as Error).message);
		} finally {
			agentLoading = false;
		}
	}

	onMount(() => {
		load();
		// 解析 ?action=evolve 自动展开
		const action = page.url.searchParams.get('action');
		if (action === 'evolve') {
			agentOp = 'evolve';
		}
	});
</script>

<div class="space-y-6">
	<Button variant="ghost" size="sm" onclick={() => goto('/')}>
		<ArrowLeft class="mr-1 h-4 w-4" /> 返回
	</Button>

	{#if loading}
		<Card class="p-6">加载中...</Card>
	{:else if error}
		<Card class="border-destructive/30 bg-destructive/5 p-6">
			<p class="text-destructive">❌ {error}</p>
		</Card>
	{:else if skill}
		<div class="space-y-2">
			<div class="flex items-center justify-between">
				<div>
					<h1 class="text-3xl font-semibold text-foreground">{skill.name}</h1>
					<button
						onclick={copyPath}
						class="mt-1 flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
					>
						<code class="rounded bg-muted px-1.5 py-0.5">{skill.path}</code>
						{#if copied}<Check class="h-3 w-3 text-green-600" />{:else}<Copy class="h-3 w-3" />{/if}
					</button>
				</div>
				<div class="flex gap-2">
					<Button variant="outline" size="sm" onclick={() => (moveOpen = true)} data-move-open>
						<FolderInput class="mr-1 h-4 w-4" /> 移动
					</Button>
					<Button variant="outline" size="sm" onclick={uninstall}>
						<Trash2 class="mr-1 h-4 w-4" /> 卸载
					</Button>
				</div>
			</div>
		</div>

		<Card class="p-6">
			<div class="prose prose-stone max-w-none">
				{@html renderMarkdown(skill.content)}
			</div>
		</Card>

		<Separator />

		<Card class="p-6">
			<h2 class="mb-3 text-lg font-medium text-foreground">LLM 操作</h2>
			<p class="mb-4 text-sm text-muted-foreground">
				对当前 skill 调 LLM 执行组合/编排/合并/进化。compose / orchestrate / merge 需要在主页先选其他 skill。
			</p>

			<div class="space-y-3">
				<div class="flex flex-wrap gap-2">
					{#each ['evolve', 'compose', 'orchestrate', 'merge'] as op}
						<Button
							variant={agentOp === op ? 'default' : 'outline'}
							size="sm"
							onclick={() => (agentOp = op as typeof agentOp)}
						>
							{#if op === 'evolve'}<Sparkles class="mr-1 h-4 w-4" />{/if}
							{#if op === 'compose'}<Layers class="mr-1 h-4 w-4" />{/if}
							{#if op === 'orchestrate'}<Workflow class="mr-1 h-4 w-4" />{/if}
							{#if op === 'merge'}<GitMerge class="mr-1 h-4 w-4" />{/if}
							{op}
						</Button>
					{/each}
				</div>

				{#if agentOp === 'merge'}
					<input
						bind:value={mergeTarget}
						placeholder="合并后的新 skill 名 (e.g. merged-skill)"
						class="w-full rounded-lg border border-input bg-card px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:ring-2 focus:ring-ring focus:outline-none"
					/>
				{/if}

				<Textarea
					bind:value={agentPrompt}
					placeholder={agentOp === 'evolve'
						? '进化方向 / 改进要求 (必填)'
						: '可选：补充要求或上下文'}
					rows={3}
				/>

				<Button onclick={runAgent} disabled={agentLoading}>
					{agentLoading ? '调 LLM 中...' : '执行'}
				</Button>
			</div>

			{#if agentResult}
				<div class="mt-4 rounded-lg border border-border bg-muted/30 p-4">
					<div class="mb-2 flex items-center gap-2">
						<Badge variant="secondary">操作: {agentResult.operation}</Badge>
						{#if agentResult.affected.length > 0}
							<span class="text-xs text-muted-foreground">
								涉及: {agentResult.affected.join(', ')}
							</span>
						{/if}
						{#if agentResult.target_path}
							<span class="text-xs text-green-600">→ 写入 {agentResult.target_path}</span>
						{/if}
					</div>
					<div class="prose prose-stone max-w-none text-sm">
						{@html renderMarkdown(agentResult.result)}
					</div>
				</div>
			{/if}
		</Card>
	{/if}
</div>

{#if skill}
	<MoveSkillDialog
		bind:open={moveOpen}
		skillName={skill.name}
		currentAgent={skill.agent}
		onMoved={load}
	/>
{/if}
