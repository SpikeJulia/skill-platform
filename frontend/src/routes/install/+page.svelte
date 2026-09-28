<script lang="ts">
	import { api } from '$lib/api';
	import { Card } from '$lib/components/ui/card';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Tabs, TabsList, TabsTrigger, TabsContent } from '$lib/components/ui/tabs';
	import { Badge } from '$lib/components/ui/badge';
	import { ArrowLeft, ClipboardPaste, Link2, Loader2 } from '@lucide/svelte';
	import { goto } from '$app/navigation';

	let tab = $state<'content' | 'url'>('content');

	// 内容模式
	let contentName = $state('');
	let contentBody = $state('');
	let contentLoading = $state(false);
	let contentResult = $state<{ installed: string; path: string; sync: string } | null>(null);
	let contentError = $state('');

	async function installContent() {
		contentLoading = true;
		contentResult = null;
		contentError = '';
		try {
			contentResult = await api.installContent(contentName, contentBody);
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
	let urlResult = $state<{ installed: string; url: string; agent_output: string } | null>(null);
	let urlError = $state('');

	async function installURL() {
		urlLoading = true;
		urlResult = null;
		urlError = '';
		try {
			urlResult = await api.installURL(urlInput, urlName || undefined);
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

	<Card class="p-6">
		<Tabs bind:value={tab}>
			<TabsList class="grid w-full grid-cols-2">
				<TabsTrigger value="content">
					<ClipboardPaste class="mr-1 h-4 w-4" /> 粘贴内容
				</TabsTrigger>
				<TabsTrigger value="url">
					<Link2 class="mr-1 h-4 w-4" /> 粘贴 URL
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
						<p class="text-green-700">✓ 已安装 <code>{contentResult.installed}</code></p>
						<p class="mt-1 text-xs text-muted-foreground">{contentResult.path}</p>
					</div>
				{/if}
			</TabsContent>

			<TabsContent value="url" class="space-y-4">
				<div>
					<label class="mb-1 block text-sm font-medium text-foreground">URL</label>
					<Input
						bind:value={urlInput}
						placeholder="https://github.com/owner/repo 或 skills.sh/owner/repo 或本地路径"
					/>
				</div>
				<div>
					<label class="mb-1 block text-sm font-medium text-foreground">
						Skill 名 <span class="text-muted-foreground">(可选，让 LLM 决定)</span>
					</label>
					<Input bind:value={urlName} placeholder="留空则从 SKILL.md frontmatter 取" />
				</div>
				<div class="rounded-lg border border-border bg-muted/30 p-3 text-xs text-muted-foreground">
					<strong>说明：</strong>URL 安装会调 LLM 跑 agent loop（git clone → 找 SKILL.md → 写到中央源 → 跑 sync.sh），可能需要 30 秒-2 分钟。
				</div>
				<Button onclick={installURL} disabled={urlLoading || !urlInput}>
					{#if urlLoading}<Loader2 class="mr-1 h-4 w-4 animate-spin" />{/if}
					调 LLM 安装
				</Button>
				{#if urlError}
					<p class="text-sm text-destructive">❌ {urlError}</p>
				{/if}
				{#if urlResult}
					<div class="rounded-lg border border-green-200 bg-green-50 p-3 text-sm">
						<p class="text-green-700">✓ 安装完成</p>
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
