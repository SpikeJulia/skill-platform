<script lang="ts">
	import { api, type Agent } from '$lib/api';
	import { Button } from '$lib/components/ui/button';
	import {
		Dialog,
		DialogContent,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { FolderInput, Loader2, Building2, Check } from '@lucide/svelte';

	let {
		open = $bindable(false),
		skillName,
		currentAgent,
		onMoved
	}: {
		open: boolean;
		skillName: string;
		currentAgent: string;
		onMoved?: () => void;
	} = $props();

	let agents = $state<Agent[]>([]);
	// 初始不选任何目标。空串是「中央源」的合法 id，用它当初值会让界面
	// 一打开就把中央源高亮上，看着像已经选好了——其实那是禁用项。
	let target = $state<string | null>(null);
	let loading = $state(false);
	let errorMsg = $state('');
	let result = $state('');

	const isCentral = $derived(currentAgent === 'all' || currentAgent === '');

	// 目标列表：中央源 + 各 agent 专属库。去掉当前所在的那一个（选了等于没动）。
	const options = $derived([
		{ id: '', label: '中央源', sub: '所有 agent 都能用', icon: Building2, current: isCentral },
		...agents.map((a) => ({
			id: a.name,
			label: a.label || a.name,
			sub: `只有 ${a.name} 能用`,
			icon: FolderInput,
			current: a.name === currentAgent
		}))
	]);

	$effect(() => {
		if (open) {
			void load();
		} else {
			errorMsg = '';
			result = '';
		}
	});

	async function load() {
		loading = true;
		try {
			const r = await api.listAgents();
			agents = r.agents.filter((a) => a.name !== 'all');
		} catch {
			agents = [];
		} finally {
			loading = false;
		}
	}

	async function confirm() {
		if (target === null || target === currentAgent) return;
		loading = true;
		errorMsg = '';
		result = '';
		try {
			const res = await api.moveSkill(skillName, target);
			if (res.unchanged) {
				result = '已经在目标位置，没有改动';
			} else {
				result = `已移到 ${target === '' ? '中央源' : target}`;
				onMoved?.();
			}
		} catch (e) {
			errorMsg = (e as Error).message;
		} finally {
			loading = false;
		}
	}
</script>

<Dialog bind:open>
	<DialogContent class="sm:max-w-lg" data-move-dialog>
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<FolderInput class="h-5 w-5" /> 移动 skill
			</DialogTitle>
		</DialogHeader>

		<div class="space-y-2">
			<p class="text-sm text-muted-foreground">
				<code class="rounded bg-muted px-1.5 py-0.5">{skillName}</code>
				当前在
				<strong class="text-foreground">{isCentral ? '中央源' : `${currentAgent} 的专属库`}</strong>
			</p>

			{#each options as o (o.id)}
				{@const Icon = o.icon}
				<button
					type="button"
					disabled={o.current}
					data-move-target={o.id || 'central'}
					onclick={() => (target = o.id)}
					class="flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-left transition-colors
						{target === o.id
						? 'border-primary bg-primary/5'
						: o.current
							? 'cursor-not-allowed border-border opacity-50'
							: 'border-border hover:bg-muted'}"
				>
					<Icon class="h-4 w-4 shrink-0 text-muted-foreground" />
					<div class="min-w-0 flex-1">
						<p class="text-sm font-medium">{o.label}</p>
						<p class="text-xs text-muted-foreground">{o.current ? '当前位置' : o.sub}</p>
					</div>
					{#if o.current}
						<Check class="h-4 w-4 shrink-0 text-muted-foreground" />
					{/if}
				</button>
			{/each}
		</div>

		{#if result}
			<div
				class="rounded-md border border-green-200 bg-green-50 px-3 py-2 text-sm text-green-800"
				data-move-result
			>
				{result}
				<p class="mt-1 text-xs">软链不会自动更新，需要在宿主跑一次 sync.sh。</p>
			</div>
		{/if}
		{#if errorMsg}
			<p class="text-sm text-destructive" data-move-error>{errorMsg}</p>
		{/if}

		<DialogFooter class="gap-2">
			<Button variant="outline" onclick={() => (open = false)}>关闭</Button>
			<Button onclick={confirm} disabled={loading || target === null || target === currentAgent} data-move-confirm>
				{#if loading}<Loader2 class="mr-1 h-4 w-4 animate-spin" />{/if}
				移动
			</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
