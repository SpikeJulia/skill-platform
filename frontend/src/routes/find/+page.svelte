<script lang="ts">
	import { api, type FindResponse } from '$lib/api';
	import { Card } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import { ArrowLeft, Search, Loader2, Sparkles, Package } from '@lucide/svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';

	let query = $state(page.url.searchParams.get('q') || '');
	let result = $state<FindResponse | null>(null);
	let loading = $state(false);
	let error = $state('');

	async function search() {
		if (!query.trim()) return;
		loading = true;
		result = null;
		error = '';
		try {
			result = await api.find(query.trim(), 5);
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	// 初次加载如果 URL 有 q，直接搜
	$effect(() => {
		if (query && !result && !loading) {
			search();
		}
	});
</script>

<div class="space-y-6">
	<Button variant="ghost" size="sm" onclick={() => goto('/')}>
		<ArrowLeft class="mr-1 h-4 w-4" /> 返回
	</Button>

	<div>
		<h1 class="text-2xl font-semibold text-foreground">找 Skill</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			用自然语言描述需求，LLM 从所有 skill 里选最匹配的 top 5
		</p>
	</div>

	<form
		onsubmit={(e) => {
			e.preventDefault();
			search();
		}}
		class="flex gap-2"
	>
		<div class="relative flex-1">
			<Search class="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
			<Input bind:value={query} class="pl-9" placeholder="e.g. 压力测试我的想法 / 找清理磁盘的 / 跟 devops 相关的" />
		</div>
		<Button type="submit" disabled={loading || !query.trim()}>
			{#if loading}<Loader2 class="mr-1 h-4 w-4 animate-spin" />{/if}
			搜索
		</Button>
	</form>

	{#if error}
		<Card class="border-destructive/30 bg-destructive/5 p-4">
			<p class="text-sm text-destructive">❌ {error}</p>
		</Card>
	{/if}

	{#if result}
		<div>
			<p class="mb-3 text-sm text-muted-foreground">
				为 <strong class="text-foreground">"{result.query}"</strong> 找到 {result.matches.length} 个匹配
			</p>

			{#if result.matches.length === 0}
				<Card class="p-6 text-center">
					<Sparkles class="mx-auto h-8 w-8 text-muted-foreground" />
					<p class="mt-2 text-sm text-muted-foreground">没有匹配的 skill，试试换个描述</p>
				</Card>
			{:else}
				<div class="space-y-3">
					{#each result.matches as match, i (match.name)}
						<Card class="p-4">
							<div class="flex items-start gap-3">
								<Badge variant={i === 0 ? 'default' : 'secondary'} class="mt-0.5 shrink-0">
									#{i + 1}
								</Badge>
								<div class="min-w-0 flex-1">
									<button
										onclick={() => goto(`/skills/${encodeURIComponent(match.name)}`)}
										class="flex items-center gap-1 text-base font-medium text-foreground hover:text-primary"
									>
										<Package class="h-4 w-4 text-primary" />
										{match.name}
									</button>
									<p class="mt-1 text-sm text-muted-foreground">{match.reason}</p>
								</div>
							</div>
						</Card>
					{/each}
				</div>
			{/if}
		</div>
	{/if}
</div>
