<script lang="ts">
	import { api, type Agent } from '$lib/api';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Badge } from '$lib/components/ui/badge';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import { Plus, Trash2, AlertCircle, GripVertical } from '@lucide/svelte';
	import { flip } from 'svelte/animate';
	import { agentsState, loadAgents } from '$lib/agents-state.svelte';

	// 这里没用 svelte-dnd-action：0.9.79 在 Svelte 5 下会把被拖节点摘出 DOM
	// 再隐藏挂回 <dialog>，结果是「拖完少一行」（主页 skill 网格同样中招）。
	// 下面的实现只重排数据、从不替换节点，行为可预测。
	// dndzone 还要求把 id 当 each key，而拖拽中 id 会被占位符改写，同样会触发上面的问题。

	let { open = $bindable(false) }: { open: boolean } = $props();

	// 共享状态：主页 tab 栏读的是同一份，改顺序后那边会跟着刷新
	// 「全部」是系统 tab，后端不允许改它的 sort_order，所以放在排序区外、不可拖动。
	const fixedAgents = $derived(agentsState.items.filter((a) => a.name === 'all'));

	/** 可拖动列表，「全部」不在其中 */
	let movableAgents = $state<Agent[]>([]);
	/** 排序区容器 */
	let zoneEl = $state<HTMLElement | null>(null);

	/** 当前正在拖动的行下标；null = 没在拖 */
	let dragIndex = $state<number | null>(null);
	/** 按下但还没超过阈值的候选行 */
	let pendingIndex = $state<number | null>(null);
	let pointerId = -1;
	let startPointerY = 0;
	let dragMoved = false;

	const DRAG_THRESHOLD_PX = 4;

	async function reloadAgents() {
		await loadAgents();
		movableAgents = agentsState.items.filter((a) => a.name !== 'all');
		dragIndex = null;
		pendingIndex = null;
	}

	// 打开时拉一次
	let lastOpen = $state(false);
	$effect(() => {
		if (open && !lastOpen) {
			reloadAgents();
			newName = '';
			addError = '';
			reorderError = '';
		}
		lastOpen = open;
	});

	let newName = $state('');
	let adding = $state(false);
	let addError = $state('');
	let deleting = $state<string | null>(null);
	let reordering = $state(false);
	let reorderError = $state('');

	async function addAgent() {
		const name = newName.trim();
		if (!name) {
			addError = 'name 不能为空';
			return;
		}
		if (!/^[a-zA-Z0-9_-]+$/.test(name)) {
			addError = 'name 只能含字母、数字、_、-';
			return;
		}
		adding = true;
		addError = '';
		try {
			await api.createAgent({ name, label: name });
			newName = '';
			await reloadAgents();
		} catch (err) {
			addError = (err as Error).message;
		} finally {
			adding = false;
		}
	}

	async function deleteAgent(name: string) {
		if (name === 'all') return;
		if (
			!confirm(
				`确定删除 agent tab "${name}"?\n（不会动 ~/AI/agent-skills-personal/${name}/ 下的 skill 数据）`
			)
		)
			return;
		deleting = name;
		try {
			await api.deleteAgent(name);
			await reloadAgents();
		} catch (err) {
			alert('删除失败: ' + (err as Error).message);
		} finally {
			deleting = null;
		}
	}

	/** 只重排本地数据，不动 DOM 节点身份 */
	function move(from: number, to: number): boolean {
		if (to < 0 || to >= movableAgents.length || from === to) return false;
		const next = [...movableAgents];
		const [moved] = next.splice(from, 1);
		next.splice(to, 0, moved);
		movableAgents = next;
		return true;
	}

	/** 按 10 递增写回 sort_order；失败以服务端为准回滚 */
	async function persistOrder(list: Agent[]): Promise<void> {
		reorderError = '';
		reordering = true;
		try {
			await Promise.all(
				list.map((agent, i) => api.updateAgent(agent.name, { sort_order: (i + 1) * 10 }))
			);
			// 以服务端为准重拉：共享状态刷新后，主页 tab 栏同步更新
			await reloadAgents();
		} catch (err) {
			reorderError = '排序保存失败：' + (err as Error).message;
			await reloadAgents();
		} finally {
			reordering = false;
		}
	}

	// ---- 指针拖拽 ----

	/** 指针落在哪一行：第一个「中线低于指针」的行 */
	function computeTargetIndex(clientY: number): number {
		if (!zoneEl) return 0;
		const rows = zoneEl.querySelectorAll(':scope > [data-agent-row]');
		for (let i = 0; i < rows.length; i++) {
			const rect = rows[i].getBoundingClientRect();
			if (clientY < rect.top + rect.height / 2) return i;
		}
		return Math.max(0, rows.length - 1);
	}

	function onRowPointerDown(e: PointerEvent, index: number) {
		if (e.button !== 0 || reordering || deleting !== null) return;
		// 按钮上的按下不启动拖拽（点删除不应该变成拖动）
		if ((e.target as HTMLElement | null)?.closest('button')) return;
		pendingIndex = index;
		pointerId = e.pointerId;
		startPointerY = e.clientY;
		dragMoved = false;
		window.addEventListener('pointermove', onPointerMove);
		window.addEventListener('pointerup', onPointerUp);
		window.addEventListener('pointercancel', onPointerUp);
	}

	function onPointerMove(e: PointerEvent) {
		if (e.pointerId !== pointerId) return;
		let current = dragIndex;
		if (current === null) {
			if (Math.abs(e.clientY - startPointerY) < DRAG_THRESHOLD_PX) return;
			if (pendingIndex === null) return;
			current = pendingIndex;
			dragIndex = current;
			dragMoved = true;
			document.body.style.userSelect = 'none';
		}
		const target = computeTargetIndex(e.clientY);
		if (target !== current && move(current, target)) {
			dragIndex = target;
		}
	}

	async function onPointerUp(e: PointerEvent) {
		if (e.pointerId !== pointerId) return;
		window.removeEventListener('pointermove', onPointerMove);
		window.removeEventListener('pointerup', onPointerUp);
		window.removeEventListener('pointercancel', onPointerUp);
		document.body.style.userSelect = '';
		const changed = dragMoved;
		dragIndex = null;
		pendingIndex = null;
		pointerId = -1;
		if (changed) await persistOrder(movableAgents);
	}
</script>

<!--
	Dialog open 完全受父级 prop 控制。父级用 bind:open={settingsOpen}，
	bits-ui 内部改 open 会通过 bind 反向写到 settingsOpen。
	所有关闭路径（X/ESC/overlay click/完成按钮）都走 bits-ui 内部 onOpenChange。
-->
<Dialog bind:open>
	<DialogContent class="max-w-md">
		<DialogHeader>
			<DialogTitle>管理 Agent Tabs</DialogTitle>
			<DialogDescription>
				拖动整行可以调整顺序（「全部」固定在第一位）。删除 tab 不会动
				~/AI/agent-skills-personal/&lt;name&gt;/ 下的数据。
			</DialogDescription>
		</DialogHeader>

		<div class="space-y-2">
			<div class="flex items-center gap-2">
				<Input
					bind:value={newName}
					placeholder="新 agent name (e.g. my-agent)"
					class="h-8 text-sm"
					disabled={adding}
					onkeydown={(e) => {
						if (e.key === 'Enter') addAgent();
					}}
				/>
				<Button size="sm" onclick={addAgent} disabled={adding || !newName.trim()}>
					<Plus class="mr-1 h-3 w-3" /> 添加
				</Button>
			</div>
			{#if addError}
				<p class="flex items-center gap-1 text-xs text-destructive">
					<AlertCircle class="h-3 w-3" /> {addError}
				</p>
			{/if}
		</div>

		<div class="max-h-80 overflow-y-auto rounded-md border border-border p-1">
			{#each fixedAgents as a (a.name)}
				<div class="flex items-center justify-between px-2 py-1.5 text-sm">
					<div class="flex items-center gap-2">
						<span class="font-medium text-foreground">{a.label}</span>
						<Badge variant="secondary" class="text-[0.625rem]">系统</Badge>
					</div>
				</div>
			{/each}

			<div
				bind:this={zoneEl}
				role="listbox"
				aria-label="agent 排序区"
				class="min-h-8 outline-none"
			>
				{#each movableAgents as a, index (a.name)}
					<div
						data-agent-row={a.name}
						role="option"
						aria-selected="false"
						aria-label={a.label}
						onpointerdown={(e) => onRowPointerDown(e, index)}
						animate:flip={{ duration: 160 }}
						class="flex touch-none items-center justify-between rounded px-2 py-1.5 text-sm select-none hover:bg-muted focus-visible:ring-2 focus-visible:ring-ring focus-visible:outline-none"
						class:cursor-grabbing={dragIndex === index}
						class:bg-muted={dragIndex === index}
						class:shadow-md={dragIndex === index}
						class:z-10={dragIndex === index}
					>
						<span class="font-medium text-foreground">{a.label}</span>
						<div class="flex items-center gap-1">
							<GripVertical class="h-3.5 w-3.5 text-muted-foreground/40" />
							<button
								type="button"
								onclick={() => deleteAgent(a.name)}
								disabled={deleting === a.name}
								class="rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:opacity-50"
								title="删除 {a.name}"
								aria-label="删除 {a.label}"
							>
								<Trash2 class="h-3.5 w-3.5" />
							</button>
						</div>
					</div>
				{/each}
			</div>
		</div>

		{#if reorderError}
			<p class="flex items-center gap-1 text-xs text-destructive">
				<AlertCircle class="h-3 w-3" /> {reorderError}
			</p>
		{/if}

		<DialogFooter>
			<Button variant="ghost" size="sm" onclick={() => (open = false)}>完成</Button>
		</DialogFooter>
	</DialogContent>
</Dialog>
