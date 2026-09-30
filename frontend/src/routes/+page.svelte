<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type Skill, type Unmanaged } from '$lib/api';
	import { agentsState, loadAgents } from '$lib/agents-state.svelte';
	import { Card } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Badge } from '$lib/components/ui/badge';
	import { Separator } from '$lib/components/ui/separator';
	import { Skeleton } from '$lib/components/ui/skeleton';
	import { Input } from '$lib/components/ui/input';
	import {
		Package,
		Plus,
		Trash2,
		Layers,
		GitMerge,
		Sparkles,
		Workflow,
		Globe,
		Lock,
		RefreshCw,
		X,
		Search,
		EyeOff,
		Copy,
		Check,
		FolderInput,
		PackageOpen
	} from '@lucide/svelte';
	import { flip } from 'svelte/animate';
	import { goto } from '$app/navigation';

	let skills = $state<Skill[]>([]);
	let selected = $state<Set<string>>(new Set());
	let loading = $state(true);
	let error = $state('');

	// 未纳管：平台管不到但确实存在的 skill。
	// 容器看不到 ~/.<agent>/skills/，所以这份清单由宿主 sync.sh 生成、平台只读。
	let unmanaged = $state<Unmanaged[]>([]);
	let showUnmanaged = $state(false);
	let copiedName = $state('');

	const localUnmanaged = $derived(unmanaged.filter((u) => u.kind === 'local'));
	const builtinUnmanaged = $derived(unmanaged.filter((u) => u.kind === 'builtin'));

	// 纳管 = 把真目录 mv 进专属源，重跑 sync.sh 后就变成软链。
	// 容器读不到宿主 home，所以这步只能给命令让用户跑，不能由后端代劳。
	function claimCommand(u: Unmanaged): string {
		return `mv "${u.path}" ~/AI/agent-skills-personal/${u.agent}/ && bash ~/AI/agent-skills/sync.sh`;
	}

	async function copyClaim(u: Unmanaged, e: MouseEvent) {
		e.stopPropagation();
		const cmd = claimCommand(u);
		try {
			await navigator.clipboard.writeText(cmd);
			copiedName = u.agent + '/' + u.name;
			setTimeout(() => (copiedName = ''), 1500);
		} catch {
			alert('复制失败，请手动选中下面的命令');
		}
	}

	// 当前 tab：'all' | 某个 agent 名
	let activeTab = $state<string>('all');

	// v0.2.1: 主页 inline 搜索（与 tab 联动；不调 LLM，纯本地匹配 name + description）
	let searchQuery = $state('');

	// 拖拽顺序（仅 frontend localStorage 持久化，v0.1 不入 DB）
	const ORDER_KEY = 'skill-platform-order';

	function loadOrder() {
		try {
			return JSON.parse(localStorage.getItem(ORDER_KEY) || '[]') as string[];
		} catch {
			return [];
		}
	}
	function saveOrder(order: string[]) {
		localStorage.setItem(ORDER_KEY, JSON.stringify(order));
	}

	function applyOrder(list: Skill[]): Skill[] {
		const order = loadOrder();
		if (order.length === 0) return list;
		const map = new Map(list.map((s) => [s.name, s]));
		const ordered: Skill[] = [];
		for (const name of order) {
			const s = map.get(name);
			if (s) {
				ordered.push(s);
				map.delete(name);
			}
		}
		for (const s of map.values()) ordered.push(s);
		return ordered;
	}

	// ---- 卡片拖拽排序（自研 pointer 实现）----
	// 不用 svelte-dnd-action：0.9.79 在 Svelte 5 下会把被拖节点摘出 DOM 再隐藏挂回，
	// 表现为「拖一个少一个」（AgentManagerDialog 里也踩过同一个坑）。
	// 这里只重排数组、节点身份从不改变；另外单独做一个跟手的浮层（ghost），
	// 原卡片留在列表里当「落点占位」，配合 animate:flip 做位移动画。
	let gridEl = $state<HTMLElement | null>(null);
	/** 正在拖动的项在**可见列表**里的下标；null = 没在拖 */
	let dragIndex = $state<number | null>(null);
	/** 按下但还没超过阈值的候选下标 */
	let pendingIndex: number | null = null;
	/** 候选下标对应的 DOM 节点，用来克隆出浮层 */
	let pendingEl: HTMLElement | null = null;
	let pointerId = -1;
	let startX = 0;
	let startY = 0;
	let dragMoved = false;
	/** 跟手的浮动卡片 */
	let ghostEl: HTMLElement | null = null;
	let ghostBase: { x: number; y: number } | null = null;
	/**
	 * 这个时间戳之前到达的 click 不算点选。
	 * 卡片整体是「点选」区，拖完松手浏览器还会补一个 click，不拦就会被误当成点选。
	 * 用时间戳而不是布尔标记：拖拽结束时 pointerup 与后续 click 的 target 可能不是同一张卡
	 * （拖动过程中列表重排过），布尔标记可能一直没被消费，反而吃掉后面真实的一次点击。
	 */
	let suppressClickUntil = 0;
	const DRAG_THRESHOLD_PX = 5;

	/** 拎起：克隆一张卡片钉在视口上，跟着指针走 */
	function startGhost(el: HTMLElement, clientX: number, clientY: number) {
		const r = el.getBoundingClientRect();
		const ghost = el.cloneNode(true) as HTMLElement;
		ghost.removeAttribute('data-skill-card');
		ghost.setAttribute('data-drag-ghost', '');
		// 去掉「已选中」的描边。用 classList 精确移除而不是正则替换——
		// 正则的 \bring-2\b 会连 focus:ring-2 一起削掉，留下半个类名。
		ghost.classList.remove('ring-2', 'ring-primary');
		ghost.classList.add('opacity-90');
		Object.assign(ghost.style, {
			position: 'fixed',
			left: `${r.left}px`,
			top: `${r.top}px`,
			width: `${r.width}px`,
			height: `${r.height}px`,
			margin: '0',
			pointerEvents: 'none',
			zIndex: '9999',
			transition: 'none',
			boxShadow: '0 16px 40px rgba(0,0,0,0.22)',
			cursor: 'grabbing'
		});
		document.body.appendChild(ghost);
		ghostEl = ghost;
		ghostBase = { x: clientX, y: clientY };
	}

	function moveGhost(clientX: number, clientY: number) {
		if (!ghostEl || !ghostBase) return;
		const dx = clientX - ghostBase.x;
		const dy = clientY - ghostBase.y;
		ghostEl.style.transform = `translate3d(${dx}px, ${dy}px, 0) scale(1.02)`;
	}

	function clearGhost() {
		ghostEl?.remove();
		ghostEl = null;
		ghostBase = null;
	}

	/** 指针落在哪个可见卡片上：网格要先定行，再定行内位置 */
	function computeTargetIndex(clientX: number, clientY: number): number {
		if (!gridEl) return 0;
		const cards = Array.from(gridEl.querySelectorAll<HTMLElement>(':scope > [data-skill-card]'));
		if (cards.length === 0) return 0;

		// 用 offset* 而不是 getBoundingClientRect()：offset 不含 transform，
		// 而 animate:flip 动画期间正是用 transform 在搬运卡片。
		// 直接用 rect 会把「正在飞行的卡片」当成已经就位，落点来回跳。
		const gridRect = gridEl.getBoundingClientRect();
		const px = clientX - gridRect.left;
		const py = clientY - gridRect.top;
		const rects = cards.map((el) => ({
			left: el.offsetLeft,
			top: el.offsetTop,
			right: el.offsetLeft + el.offsetWidth,
			bottom: el.offsetTop + el.offsetHeight,
			midX: el.offsetLeft + el.offsetWidth / 2
		}));

		// CSS grid 同一行内各卡 top 相同 → 按 top 聚成「行」
		const rows: { top: number; bottom: number; idx: number[] }[] = [];
		rects.forEach((r, i) => {
			const row = rows.find((x) => Math.abs(x.top - r.top) < 8);
			if (row) {
				row.bottom = Math.max(row.bottom, r.bottom);
				row.idx.push(i);
			} else {
				rows.push({ top: r.top, bottom: r.bottom, idx: [i] });
			}
		});

		// 选行：指针在行带内直接命中，否则取最近的一行
		let target = rows[0];
		let best = Infinity;
		for (const row of rows) {
			if (py >= row.top && py <= row.bottom) {
				target = row;
				break;
			}
			const d = py < row.top ? row.top - py : py - row.bottom;
			if (d < best) {
				best = d;
				target = row;
			}
		}

		// 行内：第一个「中线在指针右侧」的卡片即落点
		for (const i of target.idx) {
			if (px < rects[i].midX) return i;
		}
		return target.idx[target.idx.length - 1];
	}

	/**
	 * 把可见列表的 from 移到 to，再映射回完整 skills 数组。
	 * 只置换「可见项占用的那些槽位」，不可见的 skill 保持原位——
	 * 这样带 tab/搜索筛选时拖动也不会把看不见的 skill 弄丢。
	 * （旧的库实现直接把筛选结果赋回 skills，筛选中拖一次就真删了数据。）
	 */
	function moveVisible(from: number, to: number): boolean {
		const visible = filteredSkills();
		if (to < 0 || to >= visible.length || from === to) return false;
		const moved = visible[from];
		if (!moved) return false;

		const nextVisible = [...visible];
		nextVisible.splice(from, 1);
		nextVisible.splice(to, 0, moved);

		const visibleNames = new Set(visible.map((s) => s.name));
		const byName = new Map(skills.map((s) => [s.name, s]));
		const queue = nextVisible.map((s) => s.name);
		let qi = 0;
		const next: Skill[] = [];
		for (const s of skills) {
			if (visibleNames.has(s.name)) {
				const picked = byName.get(queue[qi++]);
				next.push(picked ?? s);
			} else {
				next.push(s);
			}
		}
		skills = next;
		return true;
	}

	function onCardPointerDown(e: PointerEvent, index: number) {
		if (e.button !== 0) return;
		// 卡片内的按钮（卸载 / 查看 SKILL.md / 进化）不启动拖拽
		if ((e.target as HTMLElement | null)?.closest('button')) return;
		pendingIndex = index;
		pendingEl = e.currentTarget as HTMLElement;
		pointerId = e.pointerId;
		startX = e.clientX;
		startY = e.clientY;
		dragMoved = false;
		window.addEventListener('pointermove', onPointerMove);
		window.addEventListener('pointerup', onPointerUp);
		window.addEventListener('pointercancel', onPointerUp);
	}

	function onPointerMove(e: PointerEvent) {
		if (e.pointerId !== pointerId) return;
		let current = dragIndex;
		if (current === null) {
			if (
				Math.abs(e.clientX - startX) < DRAG_THRESHOLD_PX &&
				Math.abs(e.clientY - startY) < DRAG_THRESHOLD_PX
			) {
				return;
			}
			if (pendingIndex === null) return;
			current = pendingIndex;
			dragIndex = current;
			dragMoved = true;
			// 拖动期间禁掉文本选择，否则会把沿途卡片的文字整片选中
			document.body.style.userSelect = 'none';
			if (pendingEl) startGhost(pendingEl, startX, startY);
		}
		moveGhost(e.clientX, e.clientY);
		const target = computeTargetIndex(e.clientX, e.clientY);
		if (target !== current && moveVisible(current, target)) {
			dragIndex = target;
		}
	}

	function onPointerUp(e: PointerEvent) {
		if (e.pointerId !== pointerId) return;
		window.removeEventListener('pointermove', onPointerMove);
		window.removeEventListener('pointerup', onPointerUp);
		window.removeEventListener('pointercancel', onPointerUp);
		document.body.style.userSelect = '';
		const moved = dragMoved;
		clearGhost();
		dragIndex = null;
		pendingIndex = null;
		pendingEl = null;
		pointerId = -1;
		dragMoved = false;
		if (moved) {
			suppressClickUntil = Date.now() + 300;
			saveOrder(skills.map((s) => s.name));
		}
	}

	function toggleSelect(name: string) {
		const next = new Set(selected);
		if (next.has(name)) next.delete(name);
		else next.add(name);
		selected = next;
	}

	async function refresh() {
		try {
			loading = true;
			const skillsRes = await api.listSkills();
			skills = applyOrder(skillsRes.skills);
			error = '';
			await loadAgents();
			// 未纳管清单拿不到不影响主列表（sync.sh 没跑过时就是空的）
			try {
				const uRes = await api.listUnmanaged();
				unmanaged = uRes.unmanaged;
			} catch {
				unmanaged = [];
			}
		} catch (e) {
			error = (e as Error).message;
		} finally {
			loading = false;
		}
	}

	// 搜索匹配：name 或 description 包含 query（大小写不敏感）
	function matchesQuery(s: Skill, q: string): boolean {
		if (!q) return true;
		const lq = q.toLowerCase();
		return (
			s.name.toLowerCase().includes(lq) ||
			(s.description || '').toLowerCase().includes(lq)
		);
	}

	// 满足 tab 约束
	function matchesTab(s: Skill, tab: string): boolean {
		if (tab === 'all') return true;
		return s.agents?.includes(tab) ?? false;
	}

	// 当前视图下应展示的 skill（应用 search + tab 过滤）
	function filteredSkills(): Skill[] {
		const q = searchQuery.trim();
		return skills.filter((s) => matchesQuery(s, q) && matchesTab(s, activeTab));
	}

	// tab 计数：考虑搜索词
	function countForTab(tab: string): number {
		const q = searchQuery.trim();
		return skills.filter((s) => matchesQuery(s, q) && matchesTab(s, tab)).length;
	}

	// "全部" tab 下，搜索命中里"其他 agent 还有多少"
	function otherAgentMatches(): { name: string; label: string; count: number }[] {
		if (activeTab !== 'all' || !searchQuery.trim()) return [];
		const q = searchQuery.trim();
		const out: { name: string; label: string; count: number }[] = [];
		for (const a of agentsState.items) {
			if (a.name === 'all') continue;
			const c = skills.filter(
				(s) => matchesQuery(s, q) && s.agents?.includes(a.name) && !s.agents?.includes('all')
			).length;
			if (c > 0) out.push({ name: a.name, label: a.label, count: c });
		}
		return out;
	}

	async function uninstall(name: string, e: MouseEvent) {
		e.stopPropagation();
		if (!confirm(`确定卸载 "${name}"?`)) return;
		try {
			await api.deleteSkill(name);
			await refresh();
		} catch (err) {
			alert('卸载失败: ' + (err as Error).message);
		}
	}

	async function doCompose() {
		const list = Array.from(selected);
		if (list.length < 2) {
			alert('至少选 2 个 skill');
			return;
		}
		alert('调 LLM 组合（异步），结果在控制台看');
		try {
			const res = await api.compose(list);
			console.log('compose result:', res);
		} catch (err) {
			alert('失败: ' + (err as Error).message);
		}
	}

	// 安装目标跟着当前 tab：在「已纳管」(all) 装 = 中央源；在具体 agent 装 = 它的专属库
	function installTargetAgent(): string {
		return activeTab;
	}
	function installTargetNote(): string {
		return activeTab === 'all'
			? '装到中央源，所有 agent 都能用'
			: `只装到 ${activeTab} 的专属库，不影响其他 agent`;
	}

	function agentLabel(agents?: string[]): { text: string; icon: typeof Globe; class: string } {
		if (!agents || agents.length === 0) {
			return { text: 'unknown', icon: Globe, class: 'text-muted-foreground' };
		}
		if (agents.length === 1 && agents[0] === 'all') {
			return { text: 'central', icon: Globe, class: 'text-primary' };
		}
		const agent = agents[0];
		return { text: agent, icon: Lock, class: 'text-amber-600' };
	}

	onMount(refresh);
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-2xl font-semibold text-foreground">所有 Skill</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			{#if loading}加载中...{:else if error}❌ {error}{:else}{skills.length} 个 skill · 拖拽排序{/if}
		</p>
	</div>

	<!-- Agent tab 过滤栏 + 安装入口。
	     「安装新 skill」放在这里而不是页头，因为它必须和当前 tab 绑定：
	     在 minimax 页签点安装，就只装到 minimax 的专属库（?agent=minimax 传给安装页）。
	     在「已纳管」页签点安装，则装到中央源、所有 agent 都有。 -->
	<div
		data-agent-tabbar
		class="flex flex-wrap items-center gap-2 border-b border-border pb-2"
	>
		{#each agentsState.items as tab (tab.name)}
			<button
				data-agent-tab={tab.name}
				onclick={() => (activeTab = tab.name)}
				class="flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition {activeTab === tab.name
					? 'bg-primary text-primary-foreground'
					: 'text-muted-foreground hover:bg-muted hover:text-foreground'}"
			>
				{tab.label}
				<span
					class="rounded-full px-1.5 text-xs {activeTab === tab.name
						? 'bg-primary-foreground/20'
						: 'bg-muted'}"
				>
					{countForTab(tab.name)}
				</span>
			</button>
		{/each}

		<!-- 安装目标跟着当前 tab 走：在 minimax 页签装 = 只给 minimax -->
		<Button
			data-install-btn
			variant="outline"
			size="sm"
			class="ml-auto"
			onclick={() => goto(`/install?agent=${encodeURIComponent(installTargetAgent())}`)}
			title={installTargetNote()}
		>
			<Plus class="mr-1 h-4 w-4" />
			安装到{activeTab === 'all' ? '中央源' : activeTab}
		</Button>
	</div>

	<!-- v0.2.2 增量：主页 inline 搜索框（与 tab 联动） -->
	<div class="relative">
		<Search class="absolute top-1/2 left-3 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
		<Input
			bind:value={searchQuery}
			type="search"
			placeholder="按 name 或 description 过滤（仅当前 tab）"
			class="h-9 pl-9 pr-9"
		/>
		{#if searchQuery}
			<button
				onclick={() => (searchQuery = '')}
				class="absolute top-1/2 right-2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
				title="清空"
			>
				<X class="h-3.5 w-3.5" />
			</button>
		{/if}
	</div>

	<!-- "全部" tab 搜索时：提示其他 agent 还有匹配（点击切 tab） -->
	{#if otherAgentMatches().length > 0}
		<div class="flex flex-wrap items-center gap-2 rounded-md border border-border bg-muted/30 px-3 py-2 text-xs text-muted-foreground">
			<span>其他 agent 还有匹配：</span>
			{#each otherAgentMatches() as m (m.name)}
				<button
					onclick={() => (activeTab = m.name)}
					class="rounded-full border border-border bg-background px-2.5 py-0.5 text-foreground hover:border-primary hover:text-primary"
				>
					{m.label} <span class="ml-1 text-muted-foreground">{m.count}</span>
				</button>
			{/each}
		</div>
	{/if}

	{#if selected.size > 0}
		<div class="rounded-lg border border-primary/20 bg-accent p-3">
			<div class="flex items-center justify-between">
				<span class="text-sm text-foreground">
					已选 <strong>{selected.size}</strong> 个 skill
				</span>
				<div class="flex gap-2">
					<Button size="sm" variant="outline" onclick={() => alert('orchestrate 暂未接入')}>
						<Workflow class="mr-1 h-4 w-4" /> 编排
					</Button>
					<Button size="sm" variant="outline" onclick={doCompose}>
						<Layers class="mr-1 h-4 w-4" /> 组合
					</Button>
					<Button size="sm" variant="outline" onclick={() => alert('merge 暂未接入')}>
						<GitMerge class="mr-1 h-4 w-4" /> 合并
					</Button>
					<Button size="sm" variant="ghost" onclick={() => (selected = new Set())}>清空</Button>
				</div>
			</div>
		</div>
	{/if}

	{#if loading}
		<div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
			{#each Array(6) as _}
				<Card class="p-4">
					<Skeleton class="mb-2 h-5 w-3/4" />
					<Skeleton class="h-4 w-full" />
					<Skeleton class="mt-1 h-4 w-5/6" />
				</Card>
			{/each}
		</div>
	{:else if filteredSkills().length === 0}
		<Card class="p-12 text-center">
			<Package class="mx-auto h-12 w-12 text-muted-foreground" />
			<h3 class="mt-4 text-lg font-medium text-foreground">
				{searchQuery
					? `没有匹配 "${searchQuery}" 的 skill`
					: activeTab === 'all'
						? '还没有任何 skill'
						: `没有属于 ${activeTab} 的 skill`}
			</h3>
			<p class="mt-2 text-sm text-muted-foreground">
				{searchQuery
					? '试试别的关键词，或清空搜索看看全部'
					: activeTab === 'all'
						? '点上方的「安装到中央源」开始'
						: `这里还没有 ${activeTab} 专属的 skill。用上方的「安装到${activeTab}」，或在 ~/AI/agent-skills-personal/${activeTab}/<name>/ 加一个再跑 sync.sh`}
			</p>
			{#if !searchQuery && activeTab === 'all'}
				<Button class="mt-4" onclick={() => goto('/install')}>
					<Plus class="mr-1 h-4 w-4" /> 安装一个
				</Button>
			{/if}
		</Card>
	{:else}
		<section
			bind:this={gridEl}
			data-skill-grid
			class="relative grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3"
		>
			{#each filteredSkills() as skill, index (skill.name)}
				{@const lbl = agentLabel(skill.agents)}
				{@const dragging = dragIndex === index}
				<div
					data-skill-card={skill.name}
					onpointerdown={(e) => onCardPointerDown(e, index)}
					onclick={() => {
						// 刚拖完的那次松手不算点选
						if (Date.now() < suppressClickUntil) return;
						toggleSelect(skill.name);
					}}
					onkeydown={(e) => (e.key === 'Enter' || e.key === ' ') && toggleSelect(skill.name)}
					role="button"
					tabindex="0"
					animate:flip={{ duration: 180 }}
					class="block w-full select-none rounded-xl border border-border bg-card p-4 text-left shadow-sm transition-[color,background-color,border-color,box-shadow,opacity] duration-150 focus:ring-2 focus:ring-ring focus:outline-none {dragging
						? 'cursor-grabbing border-dashed opacity-40'
						: 'cursor-pointer hover:border-primary/40 hover:shadow-md'}"
					class:ring-2={selected.has(skill.name)}
					class:ring-primary={selected.has(skill.name)}
				>
					<div class="flex items-start justify-between">
						<div class="min-w-0 flex-1">
							<div class="flex items-center gap-2">
								<Package class="h-4 w-4 shrink-0 text-primary" />
								<h3 class="truncate font-medium text-foreground">{skill.name}</h3>
								<Badge variant="outline" class="gap-1 px-1.5 py-0 text-[0.625rem] {lbl.class}">
									<svelte:component this={lbl.icon} class="h-3 w-3" />
									{lbl.text}
								</Badge>
							</div>
							<p class="mt-2 line-clamp-3 text-sm text-muted-foreground">
								{skill.description || '(无描述)'}
							</p>
						</div>
						<button
							onclick={(e) => uninstall(skill.name, e)}
							class="ml-2 rounded p-1 text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
							title="卸载"
						>
							<Trash2 class="h-4 w-4" />
						</button>
					</div>
					<div class="mt-3 flex items-center justify-between text-xs text-muted-foreground">
						<button
							onclick={(e) => {
								e.stopPropagation();
								goto(`/skills/${encodeURIComponent(skill.name)}`);
							}}
							class="text-primary hover:underline"
						>
							查看 SKILL.md →
						</button>
						<button
							onclick={(e) => {
								e.stopPropagation();
								goto(`/skills/${encodeURIComponent(skill.name)}?action=evolve`);
							}}
							class="flex items-center gap-1 text-primary hover:underline"
						>
							<Sparkles class="h-3 w-3" /> 进化
						</button>
					</div>
				</div>
			{/each}
		</section>
	{/if}

	<!-- 未纳管：平台列不出来、但 agent 确实能用的 skill -->
	{#if unmanaged.length > 0}
		<section data-unmanaged-section class="border-t border-border pt-4">
			<button
				data-unmanaged-toggle
				onclick={() => (showUnmanaged = !showUnmanaged)}
				class="flex w-full items-center gap-2 text-left text-sm text-muted-foreground hover:text-foreground"
			>
				<EyeOff class="h-4 w-4" />
				<span>
					还有 <strong class="text-foreground">{unmanaged.length}</strong> 个 skill 平台管不到
					{#if localUnmanaged.length > 0}
						（其中 <strong class="text-amber-600">{localUnmanaged.length}</strong> 个可以纳管）
					{/if}
				</span>
				<span class="ml-auto text-xs">{showUnmanaged ? '收起' : '展开'}</span>
			</button>

			{#if showUnmanaged}
				<!-- 可纳管：~/.<agent>/skills/ 里的真目录，agent 能用但平台列不出来 -->
				{#if localUnmanaged.length > 0}
					<div class="mt-3 space-y-2">
						<p class="text-xs text-muted-foreground">
							这些在你的 agent 目录里，agent 能正常调用，但平台看不见。要纳管就把目录移进专属源，
							再跑一次 sync.sh —— 之后它会自动变成软链，平台也就列出来了。
						</p>
						{#each localUnmanaged as u (u.agent + '/' + u.name)}
							{@const cmd = claimCommand(u)}
							{@const copied = copiedName === u.agent + '/' + u.name}
							<div
								data-unmanaged-local={u.name}
								class="rounded-lg border border-amber-500/30 bg-amber-500/5 p-3"
							>
								<div class="flex items-start gap-2">
									<FolderInput class="mt-0.5 h-4 w-4 shrink-0 text-amber-600" />
									<div class="min-w-0 flex-1">
										<div class="flex items-center gap-2">
											<span class="font-medium text-foreground">{u.name}</span>
											<Badge variant="outline" class="px-1.5 py-0 text-[0.625rem] text-amber-600">
												{u.agent}
											</Badge>
										</div>
										{#if u.description}
											<p class="mt-1 line-clamp-2 text-xs text-muted-foreground">{u.description}</p>
										{/if}
										<div class="mt-2 flex items-center gap-2">
											<code
												class="flex-1 truncate rounded bg-muted px-2 py-1 font-mono text-[0.6875rem] text-muted-foreground"
												title={cmd}
											>
												{cmd}
											</code>
											<Button size="sm" variant="outline" onclick={(e) => copyClaim(u, e)}>
												{#if copied}
													<Check class="mr-1 h-3.5 w-3.5" /> 已复制
												{:else}
													<Copy class="mr-1 h-3.5 w-3.5" /> 复制命令
												{/if}
											</Button>
										</div>
									</div>
								</div>
							</div>
						{/each}
					</div>
				{/if}

				<!-- agent 自带：会自动更新，不该纳管，只列出来免得以为平台漏了 -->
				{#if builtinUnmanaged.length > 0}
					<div class="mt-4">
						<p class="text-xs text-muted-foreground">
							另外这 {builtinUnmanaged.length} 个是 agent 自带的（会随版本自动更新），
							按设计不纳管，列出来只是让你知道它们存在。
						</p>
						<div class="mt-2 flex flex-wrap gap-1.5">
							{#each builtinUnmanaged as u (u.agent + '/' + u.name)}
								<span
									title={u.description || u.path}
									class="inline-flex items-center gap-1 rounded-full border border-border bg-muted/50 px-2.5 py-0.5 text-[0.6875rem] text-muted-foreground"
								>
									<PackageOpen class="h-3 w-3" />
									{u.name}
								</span>
							{/each}
						</div>
					</div>
				{/if}
			{/if}
		</section>
	{/if}
</div>
