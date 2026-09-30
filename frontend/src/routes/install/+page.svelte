<script lang="ts">
	import { api } from '$lib/api';
	import { Card } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs';
	import { Badge } from '$lib/components/ui/badge';
	import { ArrowLeft, ClipboardPaste, Link2, Loader2, Globe, Lock, Bot } from '@lucide/svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { agentsState, loadAgents } from '$lib/agents-state.svelte';

	// 默认走 URL 安装：多数人来这个页面就是手里有个链接，
	// 粘贴内容是兜底路径（比如从别处抄来的 SKILL.md）。
	let tab = $state<'url' | 'content'>('url');

	// 安装目标：跟随主页当前 tab 传过来的 ?agent=xxx。
	// all（或没传）= 中央源；具体 agent = 它的专属库。用户可以在下面改。
	let targetAgent = $state<string>('');
	let targetTouched = $state(false);

	const isCentral = $derived(!targetAgent || targetAgent === 'all');
	const targetLabel = $derived(isCentral ? '中央源（所有 agent 共享）' : `${targetAgent} 的专属库`);

	// 没显式改过就跟着 URL 上的 ?agent= 走
	$effect(() => {
		if (targetTouched) return;
		const q = page.url.searchParams.get('agent');
		targetAgent = q && q !== 'all' ? q : '';
	});

	loadAgents();

	// 内容模式
	let contentName = $state('');
	let contentBody = $state('');
	let contentLoading = $state(false);
	let contentResult = $state<{
		installed: string;
		path: string;
		target: string;
		sync: string;
	} | null>(null);
	let contentError = $state('');

	async function installContent() {
		contentLoading = true;
		contentResult = null;
		contentError = '';
		try {
			contentResult = await api.installContent(
				contentName,
				contentBody,
				isCentral ? undefined : targetAgent
			);
			contentName = '';
			contentBody = '';
		} catch (e) {
			contentError = (e as Error).message;
		} finally {
			contentLoading = false;
		}
	}

	// URL 模式
	let urlInput = $state('');
	let urlName = $state('');
	let urlLoading = $state(false);
	let urlResult = $state<{
		installed: string;
		url: string;
		target: string;
		agent_output: string;
	} | null>(null);
	let urlError = $state('');

	async function installURL() {
		urlLoading = true;
		urlResult = null;
		urlError = '';
		try {
			urlResult = await api.installURL(
				urlInput,
				urlName || undefined,
				isCentral ? undefined : targetAgent
			);
			urlInput = '';
			urlName = '';
		} catch (e) {
			urlError = (e as Error).message;
		} finally {
			urlLoading = false;
		}
	}
</script>

<div class="space-y-6">
	<Button variant="ghost" size="sm" onclick={() => goto('/')}>
		<ArrowLeft class="mr-1 h-4 w-4" /> 返回
	</Button>

	<div>
		<h1 class="text-2xl font-semibold text-foreground">安装 Skill</h1>
		<p class="mt-1 text-sm text-muted-foreground">
			支持粘贴 SKILL.md 内容 / 粘贴 URL (GitHub / skills.sh / 本地路径)
		</p>
	</div>

	<Card class="p-4">
		<div class="flex flex-wrap items-center gap-2">
			<span class="text-sm font-medium text-foreground">装到</span>
			{#snippet targetChip(value: string, label: string, icon: typeof Globe)}
				<button
					data-install-target={value || 'central'}
					onclick={() => {
						targetAgent = value === 'all' ? '' : value;
						targetTouched = true;
					}}
					class="flex items-center gap-1.5 rounded-full border px-3 py-1 text-sm transition {targetAgent === (value === 'all' ? '' : value)
						? 'border-primary bg-primary text-primary-foreground'
						: 'border-border text-muted-foreground hover:bg-muted hover:text-foreground'}"
				>
					<svelte:component this={icon} class="h-3.5 w-3.5" />
					{label}
				</button>
			{/snippet}
			{@render targetChip('all', '中央源', Globe)}
			{#each agentsState.items.filter((a) => a.name !== 'all') as a (a.name)}
				{@render targetChip(a.name, `${a.label} 专属`, Lock)}
			{/each}
			<span class="ml-auto text-xs text-muted-foreground">
				{isCentral ? '装好后所有 agent 都能用' : `只有 ${targetAgent} 能用，不影响其他 agent`}
			</span>
		</div>
	</Card>

	<Card class="p-6">
		<Tabs bind:value={tab}>
			<TabsList class="grid w-full grid-cols-2">
				<TabsTrigger value="url">
					<Link2 class="mr-1 h-4 w-4" /> 粘贴 URL
				</TabsTrigger>
				<TabsTrigger value="content">
					<ClipboardPaste class="mr-1 h-4 w-4" /> 粘贴内容
				</TabsTrigger>
			</TabsList>

			<TabsContent value="content" class="space-y-4">
				<div>
					<label class="mb-1 block text-sm font-medium text-foreground">Skill 名</label>
					<Input bind:value={contentName} placeholder="my-skill (字母数字下划线连字符)" />
				</div>
				<div>
					<label class="mb-1 block text-sm font-medium text-foreground">SKILL.md 内容</label>
					<Textarea
						bind:value={contentBody}
						rows={15}
						placeholder={'---\nname: my-skill\ndescription: A short description\n---\n\n# My Skill\n\nBody of the skill...'}
					/>
				</div>
				<Button onclick={installContent} disabled={contentLoading || !contentName || !contentBody}>
					{#if contentLoading}<Loader2 class="mr-1 h-4 w-4 animate-spin" />{/if}
					安装
				</Button>
				{#if contentError}
					<p class="text-sm text-destructive">❌ {contentError}</p>
				{/if}
				{#if contentResult}
					<div class="rounded-lg border border-green-200 bg-green-50 p-3 text-sm">
						<p class="text-green-700">
							✓ 已安装 <code>{contentResult.installed}</code>
							<span class="text-xs">→ {contentResult.target ?? targetLabel}</span>
						</p>
						<p class="mt-1 text-xs text-muted-foreground">{contentResult.path}</p>
					</div>
				{/if}
			</TabsContent>

			<TabsContent value="url" class="space-y-4">
				<div class="flex items-start gap-2 rounded-lg border border-border bg-muted/30 p-3 text-sm">
					<Bot class="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground" />
					<p class="text-muted-foreground">
						由 <strong class="text-foreground">agent 自动执行</strong>：克隆仓库 → 找到 SKILL.md → 写入
						<span class="text-foreground">{targetLabel}</span> → 更新软链。通常 30 秒到 2 分钟。
					</p>
				</div>
				<div>
					<label class="mb-1 block text-sm font-medium text-foreground">URL</label>
					<Input
						bind:value={urlInput}
						placeholder="https://github.com/owner/repo 或 skills.sh/owner/repo 或本地路径"
					/>
				</div>
				<div>
					<label class="mb-1 block text-sm font-medium text-foreground">
						Skill 名 <span class="text-muted-foreground">(可选，让 agent 决定)</span>
					</label>
					<Input bind:value={urlName} placeholder="留空则从 SKILL.md frontmatter 取" />
				</div>
				<Button onclick={installURL} disabled={urlLoading || !urlInput}>
					{#if urlLoading}<Loader2 class="mr-1 h-4 w-4 animate-spin" />{/if}
					让 agent 安装
				</Button>
				{#if urlError}
					<p class="text-sm text-destructive">❌ {urlError}</p>
				{/if}
				{#if urlResult}
					<div class="rounded-lg border border-green-200 bg-green-50 p-3 text-sm">
						<p class="text-green-700">
							✓ 安装完成<span class="text-xs">→ {urlResult.target ?? targetLabel}</span>
						</p>
						<details class="mt-2">
							<summary class="cursor-pointer text-xs text-muted-foreground">查看 agent 输出</summary>
							<pre class="mt-2 max-h-60 overflow-auto whitespace-pre-wrap text-xs">{urlResult.agent_output}</pre>
						</details>
					</div>
				{/if}
			</TabsContent>
		</Tabs>
	</Card>
</div>
