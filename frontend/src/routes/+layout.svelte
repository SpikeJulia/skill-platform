<script lang="ts">
	import { onMount } from 'svelte';
	import '../app.css';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { Search, Package, Wrench, Home, Settings } from '@lucide/svelte';
	import AgentManagerDialog from '$lib/components/AgentManagerDialog.svelte';
	import { loadAgents } from '$lib/agents-state.svelte';

	const { children } = $props();

	let searchQuery = $state('');
	let settingsOpen = $state(false);

	// agent 列表是共享状态（$lib/agents-state.svelte），主页 tab 栏和 Dialog 读同一份。
	// 这里只负责首次拉取；后续增删改由 Dialog 自己调 loadAgents() 同步。
	onMount(() => {
		loadAgents();
	});

	function goHome() {
		goto('/');
	}
	function goInstall() {
		goto('/install');
	}
	function goFind() {
		goto('/find');
	}
	function onSearch(e: SubmitEvent) {
		e.preventDefault();
		if (searchQuery.trim()) {
			goto(`/find?q=${encodeURIComponent(searchQuery.trim())}`);
		}
	}
</script>

<div class="flex min-h-screen flex-col bg-background">
	<header class="sticky top-0 z-10 border-b border-border bg-background/80 backdrop-blur">
		<div class="mx-auto flex max-w-6xl items-center gap-4 px-6 py-3">
			<button
				onclick={goHome}
				class="flex items-center gap-2 text-lg font-semibold text-foreground hover:opacity-80"
			>
				<Package class="h-5 w-5 text-primary" />
				<span>Skill Platform</span>
				<Badge variant="secondary" class="ml-1 text-xs">v0.1</Badge>
			</button>

			<form onsubmit={onSearch} class="flex-1 max-w-md">
				<div class="relative">
					<Search class="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
					<input
						bind:value={searchQuery}
						type="search"
						placeholder="找 skill (e.g. 清理磁盘、压力测试想法)..."
						class="w-full rounded-lg border border-input bg-card px-9 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:ring-2 focus:ring-ring focus:outline-none"
					/>
				</div>
			</form>

			<nav class="flex items-center gap-2">
				<Button variant="ghost" size="sm" onclick={goHome}>
					<Home class="mr-1 h-4 w-4" /> 主页
				</Button>
				<Button variant="ghost" size="sm" onclick={goInstall}>
					<Wrench class="mr-1 h-4 w-4" /> 安装
				</Button>
				<Button
					variant="ghost"
					size="icon"
					onclick={() => (settingsOpen = true)}
					title="管理 agent tabs"
				>
					<Settings class="h-4 w-4" />
				</Button>
			</nav>
		</div>
	</header>

	<AgentManagerDialog bind:open={settingsOpen} />

	<main class="mx-auto w-full max-w-6xl flex-1 px-6 py-8">
		{@render children?.()}
	</main>

	<footer class="border-t border-border py-4 text-center text-xs text-muted-foreground">
		Skill Platform · <code class="rounded bg-muted px-1">~/AI/skill-platform</code> · 127.0.0.1:8090
	</footer>
</div>
