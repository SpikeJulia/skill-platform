// 验证主页 skill 卡片的整行拖拽排序：
//   (1) 拖动后卡片数量不变（回归「拖一个少一个」）
//   (2) 顺序真的变了，且写进 localStorage
//   (3) 刷新后顺序保持
//   (4) 带搜索筛选时拖动，不会把看不见的 skill 弄丢
//   (5) 拖动不应顺带切换选中状态
// 实现是自研 pointer 拖拽（非 svelte-dnd-action），所以这里轮询 DOM 卡片序。
import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

const APP_URL = process.env.APP_URL ?? 'http://127.0.0.1:8090/';
const EDGE_PATH =
	process.env.EDGE_PATH ?? '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const HEADLESS = process.env.HEADLESS !== '0';
const GRID_SEL = '[data-skill-grid]';
const CARD_SEL = `${GRID_SEL} > [data-skill-card]`;
const ORDER_KEY = 'skill-platform-order';

class CdpClient {
	constructor(url) {
		this.nextId = 1;
		this.pending = new Map();
		this.socket = new WebSocket(url);
	}
	async connect() {
		await new Promise((res, rej) => {
			this.socket.addEventListener('open', res, { once: true });
			this.socket.addEventListener('error', rej, { once: true });
		});
		this.socket.addEventListener('message', (ev) => {
			const m = JSON.parse(ev.data);
			if (!m.id) return;
			const p = this.pending.get(m.id);
			if (!p) return;
			this.pending.delete(m.id);
			m.error ? p.reject(new Error(m.error.message)) : p.resolve(m.result);
		});
	}
	send(method, params = {}) {
		const id = this.nextId++;
		return new Promise((resolve, reject) => {
			this.pending.set(id, { resolve, reject });
			this.socket.send(JSON.stringify({ id, method, params }));
		});
	}
	close() {
		this.socket.close();
	}
}

async function reservePort() {
	const s = net.createServer();
	await new Promise((r) => s.listen(0, '127.0.0.1', r));
	const p = s.address().port;
	await new Promise((r) => s.close(r));
	return p;
}

async function evaluate(client, expression) {
	const r = await client.send('Runtime.evaluate', {
		expression,
		awaitPromise: true,
		returnByValue: true
	});
	if (r.exceptionDetails) {
		throw new Error(r.exceptionDetails.exception?.description ?? 'evaluation failed');
	}
	return r.result.value;
}

async function waitFor(client, expr, desc, timeoutMs = 8000) {
	const end = Date.now() + timeoutMs;
	while (Date.now() < end) {
		if (await evaluate(client, expr)) return;
		await new Promise((r) => setTimeout(r, 50));
	}
	throw new Error(`Timed out waiting for ${desc}`);
}

async function mouse(client, type, x, y, extra = {}) {
	await client.send('Input.dispatchMouseEvent', {
		type,
		x,
		y,
		button: 'left',
		clickCount: 1,
		buttons: type === 'mouseReleased' ? 0 : 1,
		...extra
	});
}

/** 真实指针拖拽：按下 → 分步移动（每步都让组件重算落点）→ 松开 */
async function dragCard(client, from, to, steps = 14) {
	await mouse(client, 'mouseMoved', from.x, from.y);
	await mouse(client, 'mousePressed', from.x, from.y);
	const seen = [];
	let ghostSeen = false;
	for (let i = 1; i <= steps; i++) {
		const x = from.x + ((to.x - from.x) * i) / steps;
		const y = from.y + ((to.y - from.y) * i) / steps;
		await mouse(client, 'mouseMoved', x, y, { buttons: 1 });
		await new Promise((r) => setTimeout(r, 25));
		const order = await evaluate(client, readCards);
		const key = order.join('|');
		if (seen.at(-1) !== key) seen.push(key);
		if (!ghostSeen) {
			ghostSeen = await evaluate(client, `Boolean(document.querySelector('[data-drag-ghost]'))`);
		}
	}
	for (let i = 0; i < 4; i++) {
		await mouse(client, 'mouseMoved', to.x, to.y, { buttons: 1 });
		await new Promise((r) => setTimeout(r, 25));
	}
	await mouse(client, 'mouseReleased', to.x, to.y);
	await new Promise((r) => setTimeout(r, 450));
	return { trace: seen, ghostSeen };
}

const readCards = `(() => {
	const grid = document.querySelector('${GRID_SEL}');
	if (!grid) return [];
	return [...grid.querySelectorAll(':scope > [data-skill-card]')].map((el) => el.dataset.skillCard);
})()`;

const cardPoints = `(() => {
	const grid = document.querySelector('${GRID_SEL}');
	if (!grid) return [];
	return [...grid.querySelectorAll(':scope > [data-skill-card]')].map((el) => {
		const r = el.getBoundingClientRect();
		return { name: el.dataset.skillCard, x: r.left + r.width / 2, y: r.top + 20 };
	});
})()`;

const port = await reservePort();
const profileDir = await mkdtemp(path.join(os.tmpdir(), 'skill-platform-home-drag-'));
const edge = spawn(
	EDGE_PATH,
	[
		...(HEADLESS ? ['--headless=new'] : []),
		'--disable-gpu',
		'--no-first-run',
		'--no-default-browser-check',
		`--remote-debugging-port=${port}`,
		`--user-data-dir=${profileDir}`,
		'--window-size=1440,1000',
		APP_URL
	],
	{ stdio: 'ignore' }
);

const failures = [];
const logs = [];
function check(label, ok, detail = '') {
	console.log(`${ok ? 'PASS' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`);
	if (!ok) failures.push(label);
}

let client;
let originalOrder = null;
try {
	for (let i = 0; i < 100; i++) {
		try {
			await fetch(`http://127.0.0.1:${port}/json/version`);
			break;
		} catch {
			await new Promise((r) => setTimeout(r, 100));
		}
	}
	const pages = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json());
	const page = pages.find((e) => e.type === 'page' && e.url.startsWith(APP_URL));
	if (!page) throw new Error('Edge did not expose the Skill Platform page');

	client = new CdpClient(page.webSocketDebuggerUrl);
	await client.connect();
	await client.send('Page.enable');
	await client.send('Runtime.enable');
	await client.send('Log.enable');
	// headless 下页面会被标成 hidden，rAF 停摆会让 bits-ui / flip 的时序不可靠
	await client.send('Emulation.setFocusEmulationEnabled', { enabled: true }).catch(() => {});
	client.socket.addEventListener('message', (ev) => {
		const m = JSON.parse(ev.data);
		if (m.method === 'Log.entryAdded') logs.push(`[${m.params.entry.level}] ${m.params.entry.text}`);
		if (m.method === 'Runtime.exceptionThrown')
			logs.push(`[exception] ${m.params.exceptionDetails.exception?.description ?? ''}`);
	});

	await waitFor(client, `document.readyState === 'complete'`, 'page load');
	await waitFor(client, `document.querySelectorAll('${CARD_SEL}').length >= 3`, 'skill cards');

	const before = await evaluate(client, readCards);
	originalOrder = await evaluate(client, `localStorage.getItem(${JSON.stringify(ORDER_KEY)})`);
	console.log(`拖动前: ${before.join(' | ')}`);

	const pts = await evaluate(client, cardPoints);
	// 拖「第一张」到「最后一张」的位置
	const from = pts[0];
	const to = pts[pts.length - 1];
	console.log(`拖动源=${from.name} → 目标=${to.name}`);

	const { trace, ghostSeen } = await dragCard(client, from, to);
	console.log(`拖动中行序轨迹: ${trace.join('  →  ') || '(无变化)'}`);

	const after = await evaluate(client, readCards);
	console.log(`拖动后: ${after.join(' | ')}`);

	check('拖动时有跟手的浮动层（拖拽手感）', ghostSeen);
	check(
		'松手后浮动层已清理',
		!(await evaluate(client, `Boolean(document.querySelector('[data-drag-ghost]'))`))
	);
	check(
		'拖动没有把沿途文字整片选中',
		(await evaluate(client, `window.getSelection().toString()`)).length === 0
	);
	check('拖动后卡片数量不变（回归「拖一个少一个」）', after.length === before.length,
		`${before.length} → ${after.length}`);
	check('没有卡片丢失或重复',
		after.length === new Set(after).size && before.every((n) => after.includes(n)),
		after.join(', '));
	check('顺序真的变了', after.join('|') !== before.join('|'));
	check('拖动过程中发生了重排', trace.length > 1, `${trace.length} 帧变化`);

	const stored = await evaluate(client, `localStorage.getItem(${JSON.stringify(ORDER_KEY)})`);
	const parsed = stored ? JSON.parse(stored) : [];
	check('localStorage 顺序与界面一致',
		JSON.stringify(parsed) === JSON.stringify(after),
		`ls=${parsed.join(',')} ui=${after.join(',')}`);

	// 拖动不应顺带切换选中状态
	const selCount = await evaluate(
		client,
		`document.querySelectorAll('[aria-label], button').length && (() => {
			const banner = [...document.querySelectorAll('div')].find((d) => d.textContent?.startsWith('已选'));
			return banner ? 1 : 0;
		})()`
	);
	check('拖动没有顺带选中卡片', selCount === 0);

	// 刷新后顺序应保持
	// 竞态提示：Page.reload 之后旧文档可能短暂还能应答查询，会在「新页面还在骨架屏」
	// 时就误判通过。先在旧文档里埋一个标记，再等它消失 + 文档就绪。
	await evaluate(client, `window.__preReloadMarker = true; true`);
	await client.send('Page.reload');
	await waitFor(
		client,
		`typeof window.__preReloadMarker === 'undefined' && document.readyState === 'complete'`,
		'reload to take effect',
		10000
	);
	await waitFor(client, `document.querySelectorAll('${CARD_SEL}').length >= 3`, 'cards after reload');
	const afterReload = await evaluate(client, readCards);
	console.log(`刷新后: ${afterReload.join(' | ')}`);
	check('刷新后顺序保持', afterReload.join('|') === after.join('|'));

	// ---- 筛选态拖动：不该丢数据（旧实现会直接把筛选结果赋回 skills）----
	const fullCount = afterReload.length;
	// 注意：布局头部也有一个 type=search 的搜索框（那个是跳到 /find 的），
	// 必须用 placeholder 精确定位到主页这个「过滤当前 tab」的输入框。
	const searchSel = 'input[placeholder*="过滤"]';
	// 用原生 setter + input 事件，才能被 Svelte 的 bind:value 接住
	const typeSearch = (text) => `(() => {
		const inp = document.querySelector('${searchSel}');
		const setter = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, 'value').set;
		setter.call(inp, ${JSON.stringify(text)});
		inp.dispatchEvent(new Event('input', { bubbles: true }));
		return inp.value;
	})()`;

	await evaluate(client, typeSearch('grill'));
	await new Promise((r) => setTimeout(r, 350));
	const filtered = await evaluate(client, readCards);
	console.log(`筛选 "grill" 后: ${filtered.join(' | ')}`);
	check('筛选生效且至少 2 张卡', filtered.length >= 2 && filtered.length < fullCount,
		`${filtered.length}/${fullCount}`);

	if (filtered.length >= 2) {
		const fpts = await evaluate(client, cardPoints);
		await dragCard(client, fpts[0], fpts[fpts.length - 1], 10);
		const filteredAfter = await evaluate(client, readCards);
		check('筛选态拖动后可见卡片数量不变',
			filteredAfter.length === filtered.length,
			`${filtered.length} → ${filteredAfter.length}`);

		// 清空搜索，看完整列表是否还完整
		await evaluate(client, typeSearch(''));
		await new Promise((r) => setTimeout(r, 300));
		const fullAfter = await evaluate(client, readCards);
		console.log(`清空筛选后: ${fullAfter.join(' | ')}`);
		check('筛选态拖动没有弄丢看不见的 skill',
			fullAfter.length === fullCount && afterReload.every((n) => fullAfter.includes(n)),
			`${fullCount} → ${fullAfter.length}`);
	}

	const errors = logs.filter((l) => l.startsWith('[error]') || l.startsWith('[exception]'));
	check('页面无 JS 错误', errors.length === 0, errors.join(' / '));
} catch (error) {
	throw new Error(`${error.message}\n页面日志: ${JSON.stringify(logs.slice(-8), null, 2)}`);
} finally {
	// 还原 localStorage 里的顺序，别把测试结果留给用户
	try {
		if (client) {
			await evaluate(
				client,
				originalOrder === null
					? `localStorage.removeItem(${JSON.stringify(ORDER_KEY)}); true`
					: `localStorage.setItem(${JSON.stringify(ORDER_KEY)}, ${JSON.stringify(originalOrder)}); true`
			);
		}
	} catch {
		/* 页面可能已关闭，忽略 */
	}
	client?.close();
	edge.kill();
	await new Promise((r) => setTimeout(r, 300));
	await rm(profileDir, { recursive: true, force: true, maxRetries: 5, retryDelay: 200 }).catch(
		() => {}
	);
}

if (failures.length) {
	console.log(`\n${failures.length} 项断言失败: ${failures.join('; ')}`);
	process.exit(1);
}
console.log('\n全部断言通过');
