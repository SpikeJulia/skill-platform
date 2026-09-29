<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type LLMConfigView, type LLMConfigInput, type APIFormatInfo } from '$lib/api';
	import { Button } from '$lib/components/ui/button';
	import { Input } from '$lib/components/ui/input';
	import { Textarea } from '$lib/components/ui/textarea';
	import { Badge } from '$lib/components/ui/badge';
	import {
		Dialog,
		DialogContent,
		DialogDescription,
		DialogFooter,
		DialogHeader,
		DialogTitle
	} from '$lib/components/ui/dialog';
	import {
		Plus,
		Trash2,
		Eye,
		EyeOff,
		RotateCcw,
		Loader2,
		Check,
		AlertCircle,
		Plug,
		PlusCircle
	} from '@lucide/svelte';

	let { open = $bindable(false) }: { open: boolean } = $props();

	let cfg = $state<LLMConfigView | null>(null);
	let formats = $state<APIFormatInfo[]>([]);
	let loading = $state(false);
	let saving = $state(false);
	// API Key 输入框永远空着（后端不回显）。空 = 不改。
	let apiKeyInput = $state('');
	let showKey = $state(false);
	let testMsg = $state('');
	let testOk = $state(false);

	// header 动态行：[{key, value}]
	let headerRows = $state<{ key: string; value: string }[]>([]);

	const currentFormat = $derived(formats.find((f) => f.id === cfg?.api_format));
	// 选了协议就提示它的实际端点拼法，省得用户自己猜要不要带 /v1
	const endpointPreview = $derived(
		currentFormat
			? `${cfg?.base_url || '<接口地址>'}${currentFormat.endpoint.replace('{base}', '')}`
			: ''
	);

	onMount(async () => {
		if (formats.length === 0) {
			try {
				formats = (await api.listAPIFormats()).formats;
			} catch {
				formats = [];
			}
		}
	});

	// 打开时拉一次配置
	$effect(() => {
		if (open && cfg === null) void reload();
	});

	async function reload() {
		loading = true;
		try {
			const c = await api.getConfig();
			cfg = c;
			headerRows = Object.entries(c.headers ?? {}).map(([key, value]) => ({ key, value }));
			apiKeyInput = '';
			testMsg = '';
		} catch (e) {
			testMsg = '读取配置失败: ' + (e as Error).message;
			testOk = false;
		} finally {
			loading = false;
		}
	}

	function collectHeaders(): Record<string, string> {
		const out: Record<string, string> = {};
		for (const r of headerRows) {
			const k = r.key.trim();
			if (k) out[k] = r.value;
		}
		return out;
	}

	async function save() {
		if (!cfg) return;
		saving = true;
		testMsg = '';
		try {
			const body: LLMConfigInput = {
				provider: cfg.provider,
				api_format: cfg.api_format,
				base_url: cfg.base_url,
				headers: collectHeaders(),
				model: cfg.model,
				context_window: Number(cfg.context_window) || 0,
				max_output_tokens: Number(cfg.max_output_tokens) || 0,
				reasoning_effort: cfg.reasoning_effort,
				global_prompt: cfg.global_prompt
			};
			// 空字符串 = 保持原 key 不变（否则每次改个模型名都得重输 key）
			if (apiKeyInput.trim()) body.api_key = apiKeyInput.trim();

			const res = await api.putConfig(body);
			cfg = res.config;
			apiKeyInput = '';
			headerRows = Object.entries(res.config.headers ?? {}).map(([key, value]) => ({
				key,
				value
			}));
			testMsg = res.test;
			testOk = !res.test.includes('失败') && !res.test.includes('未配置');
		} catch (e) {
			testMsg = '保存失败: ' + (e as Error).message;
			testOk = false;
		} finally {
			saving = false;
		}
	}

	async function resetAll() {
		if (!confirm('还原出厂配置？\n\n除 API Key 外的项都会重置（key 是你填的凭据，不会被抹掉）。')) return;
		saving = true;
		try {
			const res = await api.resetConfig();
			cfg = res.config;
			headerRows = Object.entries(res.config.headers ?? {}).map(([key, value]) => ({
				key,
				value
			}));
			apiKeyInput = '';
			testMsg = '已还原出厂配置';
			testOk = true;
		} catch (e) {
			testMsg = '还原失败: ' + (e as Error).message;
			testOk = false;
		} finally {
			saving = false;
		}
	}

	function resetPrompt() {
		if (!cfg) return;
		cfg.global_prompt = cfg.default_global_prompt;
	}

	function pickFormat(id: string) {
		if (!cfg) return;
		cfg.api_format = id as LLMConfigView['api_format'];
		// 切协议时把地址换成该协议的默认端点，省得用户对着旧地址发懵
		const defaults: Record<string, string> = {
			anthropic: 'https://api.minimaxi.com/anthropic',
			openai_responses: 'https://api.openai.com/v1'
		};
		cfg.base_url = defaults[id] ?? cfg.base_url;
	}
</script>

<Dialog bind:open>
	<DialogContent class="max-h-[88vh] max-w-2xl overflow-y-auto" data-config-dialog>
		<DialogHeader>
			<DialogTitle class="flex items-center gap-2">
				<Plug class="h-5 w-5" /> Agent 配置
			</DialogTitle>
			<DialogDescription>
				决定平台的内部 agent 调哪个模型、用什么协议、以及附加什么提示词。
			</DialogDescription>
		</DialogHeader>

		{#if loading}
			<div class="flex items-center gap-2 py-8 text-sm text-muted-foreground">
				<Loader2 class="h-4 w-4 animate-spin" /> 读取中…
			</div>
		{:else if cfg}
			<div class="space-y-5">
				<!-- ===== 模型 ===== -->
				<section class="space-y-3">
					<h3 class="text-sm font-semibold text-foreground">模型</h3>

					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
						<div class="space-y-1.5">
							<label class="text-sm font-medium" for="cfg-provider">提供商</label>
							<Input
								id="cfg-provider"
								data-config-field="provider"
								bind:value={cfg.provider}
								placeholder="DeepSeek / MiniMax / Ollama…"
							/>
							<p class="text-xs text-muted-foreground">只是个显示名，不影响调用。</p>
						</div>

						<div class="space-y-1.5">
							<label class="text-sm font-medium" for="cfg-format">API 格式</label>
							<select
								id="cfg-format"
								data-config-field="api_format"
								class="h-9 w-full rounded-md border border-input bg-card px-3 text-sm"
								value={cfg.api_format}
								onchange={(e) => pickFormat(e.currentTarget.value)}
							>
								{#each formats as f (f.id)}
									<option value={f.id}>{f.label}</option>
								{/each}
							</select>
							{#if currentFormat}
								<p class="text-xs text-muted-foreground">
									实际请求：<code class="rounded bg-muted px-1">{endpointPreview}</code>
								</p>
							{/if}
						</div>
					</div>

					<div class="space-y-1.5">
						<label class="text-sm font-medium" for="cfg-base">接口地址</label>
						<Input
							id="cfg-base"
							data-config-field="base_url"
							bind:value={cfg.base_url}
							placeholder="https://api.minimaxi.com/anthropic"
						/>
						{#if currentFormat}
							<p class="text-xs text-muted-foreground">{currentFormat.note}</p>
						{/if}
					</div>

					<div class="space-y-1.5">
						<label class="text-sm font-medium" for="cfg-key">API Key</label>
						<div class="relative">
							<Input
								id="cfg-key"
								data-config-field="api_key"
								type={showKey ? 'text' : 'password'}
								bind:value={apiKeyInput}
								placeholder={cfg.has_api_key
									? `已配置（${cfg.api_key_masked}）· 留空表示不修改`
									: '尚未配置'}
								class="pr-10"
							/>
							<button
								type="button"
								onclick={() => (showKey = !showKey)}
								class="absolute top-1/2 right-2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:bg-muted hover:text-foreground"
								title={showKey ? '隐藏' : '显示'}
							>
								{#if showKey}<EyeOff class="h-4 w-4" />{:else}<Eye class="h-4 w-4" />{/if}
							</button>
						</div>
						<p class="text-xs text-muted-foreground">
							{#if cfg.has_api_key}
								已保存 {cfg.api_key_masked}。出于安全考虑这里不回显原文，留空即保持不变。
							{:else if cfg.key_from_env}
								配置里没填 key，目前用的是环境变量里的值。
							{:else}
								不填的话所有调 LLM 的功能（组合 / 编排 / 合并 / 进化 / 语义搜索 / URL 安装）都用不了。
							{/if}
						</p>
					</div>

					<!-- 自定义 Headers -->
					<div class="space-y-2">
						<label class="text-sm font-medium">自定义 Headers</label>
						{#if currentFormat}
							<p class="text-xs text-muted-foreground">
								默认认证方式：{currentFormat.auth_default}。这里可以覆盖。
							</p>
						{/if}
						{#each headerRows as row, i (i)}
							<div class="flex gap-2" data-config-header-row={i}>
								<Input bind:value={row.key} placeholder="Header 名" class="flex-1" />
								<Input bind:value={row.value} placeholder="值" class="flex-1" />
								<Button
									variant="ghost"
									size="icon"
									onclick={() => headerRows.splice(i, 1)}
									title="删除"
								>
									<Trash2 class="h-4 w-4" />
								</Button>
							</div>
						{/each}
						<Button variant="outline" size="sm" onclick={() => headerRows.push({ key: '', value: '' })}>
							<PlusCircle class="mr-1 h-4 w-4" /> 添加
						</Button>
					</div>

					<div class="grid grid-cols-1 gap-3 sm:grid-cols-2">
						<div class="space-y-1.5">
							<label class="text-sm font-medium" for="cfg-model">模型名称</label>
							<Input id="cfg-model" data-config-field="model" bind:value={cfg.model} />
						</div>
						<div class="space-y-1.5">
							<label class="text-sm font-medium" for="cfg-effort">推理等级</label>
							<select
								id="cfg-effort"
								data-config-field="reasoning_effort"
								class="h-9 w-full rounded-md border border-input bg-card px-3 text-sm"
								bind:value={cfg.reasoning_effort}
							>
								<option value="">（不指定）</option>
								<option value="low">low</option>
								<option value="medium">medium</option>
								<option value="high">high</option>
							</select>
							<p class="text-xs text-muted-foreground">只有部分模型支持，填错会被供应商拒。</p>
						</div>
						<div class="space-y-1.5">
							<label class="text-sm font-medium" for="cfg-ctx">上下文窗口</label>
							<Input
								id="cfg-ctx"
								data-config-field="context_window"
								type="number"
								bind:value={cfg.context_window}
							/>
							<p class="text-xs text-muted-foreground">仅记录，平台目前不据此截断。</p>
						</div>
						<div class="space-y-1.5">
							<label class="text-sm font-medium" for="cfg-max">最大输出 Token</label>
							<Input
								id="cfg-max"
								data-config-field="max_output_tokens"
								type="number"
								bind:value={cfg.max_output_tokens}
							/>
							<p class="text-xs text-muted-foreground">
								Anthropic 协议必填，留 0 会自动用 8192。
							</p>
						</div>
					</div>
				</section>

				<!-- ===== 提示词 ===== -->
				<section class="space-y-2">
					<div class="flex items-center justify-between">
						<h3 class="text-sm font-semibold text-foreground">全局提示词</h3>
						<Button
							variant="ghost"
							size="sm"
							onclick={resetPrompt}
							title="恢复成默认（当前默认是空）"
						>
							<RotateCcw class="mr-1 h-3.5 w-3.5" /> 还原默认
						</Button>
					</div>
					<Textarea
						data-config-field="global_prompt"
						bind:value={cfg.global_prompt}
						rows={6}
						placeholder="留空 = 不改写各操作自带的提示词。填了会加在每个操作提示词的最前面。"
					/>
					<p class="text-xs text-muted-foreground">
						会作用于组合 / 编排 / 合并 / 进化 / 语义搜索 / URL 安装这六处。留空则各自用内置提示词。
					</p>
				</section>

				{#if testMsg}
					<div
						data-config-test
						class="flex items-start gap-2 rounded-md px-3 py-2 text-sm {testOk
							? 'border border-green-200 bg-green-50 text-green-800'
							: 'border border-destructive/30 bg-destructive/10 text-destructive'}"
					>
						{#if testOk}
							<Check class="mt-0.5 h-4 w-4 shrink-0" />
						{:else}
							<AlertCircle class="mt-0.5 h-4 w-4 shrink-0" />
						{/if}
						<span class="break-words">{testMsg}</span>
					</div>
				{/if}
			</div>
		{/if}

		<DialogFooter class="gap-2 sm:justify-between">
			<Button variant="ghost" onclick={resetAll} disabled={saving || loading}>
				<RotateCcw class="mr-1 h-4 w-4" /> 还原出厂配置
			</Button>
			<div class="flex gap-2">
				<Button variant="outline" onclick={() => (open = false)}>关闭</Button>
				<Button onclick={save} disabled={saving || loading || !cfg} data-config-save>
					{#if saving}<Loader2 class="mr-1 h-4 w-4 animate-spin" />{/if}
					保存并测试
				</Button>
			</div>
		</DialogFooter>
	</DialogContent>
</Dialog>
