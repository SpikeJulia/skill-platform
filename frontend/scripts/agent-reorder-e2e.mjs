// 验证「管理 Agent Tabs」Dialog 的整行拖拽排序：
//   (1) 行数不丢 (2) 顺序变化 (3) sort_order 落库 (4) 关闭重开顺序保持
// 实现用的是自研 pointer 拖拽，不派发 consider/finalize 事件，
// 所以这里直接在拖动过程中轮询 DOM 行序。
import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

const APP_URL = process.env.APP_URL ?? 'http://127.0.0.1:8090/';
const EDGE_PATH =
	process.env.EDGE_PATH ?? '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const HEADLESS = process.env.HEADLESS !== '0';

const ZONE_SEL = '[role="dialog"] [aria-label="agent 排序区"]';

class CdpClient {
	constructor(url) {
		this.nextId = 1;
		this.pending = new Map();
		this.socket = new WebSocket(url);
	}
	async connect() {
		await new Promise((resolve, reject) => {
			this.socket.addEventListener('open', resolve, { once: true });
			this.socket.addEventListener('error', reject, { once: true });
		});
		this.socket.addEventListener('message', (event) => {
			const message = JSON.parse(event.data);
			if (!message.id) return;
			const pending = this.pending.get(message.id);
			if (!pending) return;
			this.pending.delete(message.id);
			message.error ? pending.reject(new Error(message.error.message)) : pending.resolve(message.result);
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
	const server = net.createServer();
	await new Promise((resolve, reject) => {
		server.once('error', reject);
		server.listen(0, '127.0.0.1', resolve);
	});
	const { port } = server.address();
	await new Promise((resolve) => server.close(resolve));
	return port;
}

async function evaluate(client, expression) {
	const result = await client.send('Runtime.evaluate', {
		expression,
		awaitPromise: true,
		returnByValue: true
	});
	if (result.exceptionDetails) {
		throw new Error(result.exceptionDetails.exception?.description ?? 'evaluation failed');
	}
	return result.result.value;
}

async function waitFor(client, expression, description, timeoutMs = 10000) {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		if (await evaluate(client, expression)) return;
		await new Promise((resolve) => setTimeout(resolve, 50));
	}
	throw new Error(`Timed out waiting for ${description}`);
}

// CDP dispatchMouseEvent 在浏览器输入管线里会同时产生 PointerEvent，
// 所以这里对 pointerdown/move/up 驱动的实现同样有效。
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

async function clickAt(client, x, y) {
	await mouse(client, 'mouseMoved', x, y);
	await mouse(client, 'mousePressed', x, y);
	await mouse(client, 'mouseReleased', x, y);
}

async function fetchAgentOrder() {
	const data = await fetch(`${APP_URL}api/agents`).then((r) => r.json());
	return data.agents.map((a) => a.name);
}

async function setOrder(names) {
	for (let i = 0; i < names.length; i++) {
		await fetch(`${APP_URL}api/agents/${encodeURIComponent(names[i])}`, {
			method: 'PATCH',
			headers: { 'Content-Type': 'application/json' },
			body: JSON.stringify({ sort_order: (i + 1) * 10 })
		});
	}
}

const port = await reservePort();
const profileDir = await mkdtemp(path.join(os.tmpdir(), 'skill-platform-reorder-'));
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
function check(label, ok, detail = '') {
	console.log(`${ok ? 'PASS' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`);
	if (!ok) failures.push(label);
}

const readRows = `(() => {
	const zone = document.querySelector('${ZONE_SEL}');
	if (!zone) return [];
	return [...zone.querySelectorAll(':scope > [data-agent-row]')].map((el) => el.dataset.agentRow);
})()`;

async function openDialog() {
	const manage = await evaluate(
		client,
		`(() => {
			const el = document.querySelector('button[title="管理 agent tabs"]');
			const r = el.getBoundingClientRect();
			return { x: r.left + r.width / 2, y: r.top + r.height / 2 };
		})()`
	);
	await clickAt(client, manage.x, manage.y);
	// 排序区是同步渲染的，但行数据来自异步的 listAgents()，
	// 必须等到真的有行再继续，否则会读到空列表。
	// headless 下偶发第一次点击没被接住（遮罩卸载与点击竞争），重试几次。
	for (let attempt = 1; attempt <= 4; attempt++) {
		try {
			await waitFor(
				client,
				`document.querySelectorAll('${ZONE_SEL} > [data-agent-row]').length >= 2`,
				'agent rows to load',
				2500
			);
			return;
		} catch (e) {
			if (attempt === 4) throw e;
			console.log(`  第 ${attempt} 次打开没拿到行，重试`);
			const again = await evaluate(
				client,
				`(() => { const el = document.querySelector('button[title="管理 agent tabs"]'); const r = el.getBoundingClientRect(); return { x: r.left + r.width / 2, y: r.top + r.height / 2 }; })()`
			);
			await clickAt(client, again.x, again.y);
		}
	}
}

async function closeDialog() {
	const done = await evaluate(
		client,
		`(() => {
			const btn = [...document.querySelectorAll('[role="dialog"] button')]
				.find((b) => b.textContent.trim() === '完成');
			if (!btn) return null;
			const r = btn.getBoundingClientRect();
			return {
				x: r.left + r.width / 2,
				y: r.top + r.height / 2,
				atPoint: (document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2) ?? {})
					.outerHTML?.slice(0, 160) ?? null
			};
		})()`
	);
	if (!done) throw new Error('完成 button not found');
	await clickAt(client, done.x, done.y);
	try {
		await waitFor(client, `!document.querySelector('[role="dialog"]')`, 'dialog to close', 5000);
	} catch (error) {
		const debug = await evaluate(
			client,
			`(() => {
				const d = document.querySelector('[role="dialog"]');
				return {
					atPoint: ${JSON.stringify(done.atPoint)},
					dialogPresent: Boolean(d),
					dialogState: d?.getAttribute('data-state') ?? null,
					dialogVisible: d ? getComputedStyle(d).display : null,
					pointerEvents: d ? getComputedStyle(d).pointerEvents : null,
					bodyUserSelect: document.body.style.userSelect,
					activeEl: document.activeElement?.tagName ?? null
				};
			})()`
		);
		throw new Error(`${error.message}\n关闭诊断: ${JSON.stringify(debug, null, 2)}`);
	}
}

let client;
const logs = [];
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
	// headless Edge 会把闲置页面标记为 hidden，rAF 直接停摆；
	// bits-ui 的 Presence 靠 rAF 轮询动画，rAF 不跑 → Dialog 永远卸载不掉。
	// 强制页面保持 focus/visible，让测试环境与真实使用一致。
	await client.send('Emulation.setFocusEmulationEnabled', { enabled: true }).catch(() => {});
	await client.send('Runtime.enable');
	await client.send('Log.enable');
	const raw = client.socket;
	raw.addEventListener('message', (event) => {
		const m = JSON.parse(event.data);
		if (m.method === 'Log.entryAdded') logs.push(`[${m.params.entry.level}] ${m.params.entry.text}`);
		if (m.method === 'Runtime.exceptionThrown')
			logs.push(`[exception] ${m.params.exceptionDetails.exception?.description ?? ''}`);
	});

	await waitFor(client, `document.readyState === 'complete'`, 'page load');
	await waitFor(
		client,
		`Boolean(document.querySelector('button[title="管理 agent tabs"]'))`,
		'app hydration'
	);

	const dbBefore = await fetchAgentOrder();
	console.log(`数据库初始顺序: ${dbBefore.join(' | ')}`);

	await openDialog();
	const before = await evaluate(client, readRows);
	console.log(`拖动前列表: ${before.join(' | ')}`);
	check('可拖动行数 >= 2', before.length >= 2, `${before.length} 行`);
	check(
		'「全部」不在排序区内',
		!(await evaluate(
			client,
			`Boolean(document.querySelector('${ZONE_SEL}')?.textContent?.includes('全部'))`
		))
	);

	const geom = await evaluate(
		client,
		`(() => {
			const zone = document.querySelector('${ZONE_SEL}');
			const rows = [...zone.querySelectorAll(':scope > [data-agent-row]')].map((el) => {
				const r = el.getBoundingClientRect();
				return { x: r.left + 24, y: r.top + r.height / 2 };
			});
			const zr = zone.getBoundingClientRect();
			return { rows, zoneTop: zr.top };
		})()`
	);

	const source = geom.rows[geom.rows.length - 1];
	const dropY = geom.zoneTop + 2;
	console.log(`拖动源=(${source.x}, ${source.y.toFixed(1)})  drop=(${source.x}, ${dropY.toFixed(1)})`);

	await mouse(client, 'mouseMoved', source.x, source.y);
	await mouse(client, 'mousePressed', source.x, source.y);
	const seen = [];
	for (let i = 1; i <= 12; i++) {
		const y = source.y + ((dropY - source.y) * i) / 12;
		await mouse(client, 'mouseMoved', source.x, y, { buttons: 1 });
		await new Promise((r) => setTimeout(r, 30));
		const rows = await evaluate(client, readRows);
		if (seen.at(-1) !== rows.join('|')) seen.push(rows.join('|'));
	}
	console.log(`拖动中行序变化轨迹: ${seen.join('  →  ')}`);
	await mouse(client, 'mouseReleased', source.x, dropY);
	await new Promise((r) => setTimeout(r, 500));

	const after = await evaluate(client, readRows);
	const dbAfter = await fetchAgentOrder();
	console.log(`拖动后列表: ${after.join(' | ')}`);
	console.log(`数据库顺序: ${dbAfter.join(' | ')}`);

	check('拖动后没有丢行', after.length === before.length, `${before.length} → ${after.length}`);
	check('行内容都还在', after.every((n) => before.includes(n)), after.join(', '));
	check('拖动过程中确实重排过', seen.length > 1, `${seen.length} 帧变化`);
	check('顺序变了', after.join('|') !== before.join('|'));
	check(
		'sort_order 已落库且与界面一致',
		JSON.stringify(dbAfter.filter((n) => n !== 'all')) === JSON.stringify(after),
		`db=${dbAfter.join(',')} ui=${after.join(',')}`
	);
	check('「全部」仍在第一位', dbAfter[0] === 'all');

	// 关闭 → 重开：界面顺序保持，且关/开本身不会偷偷改写已保存顺序
	await closeDialog();
	const dbAfterClose = await fetchAgentOrder();
	check(
		'关闭对话框不会改写已保存顺序',
		dbAfterClose.join('|') === dbAfter.join('|'),
		`db=${dbAfterClose.join(',')}`
	);

	await openDialog();
	const reopened = await evaluate(client, readRows);
	console.log(`重开后列表: ${reopened.join(' | ')}`);
	check('重开后顺序保持', reopened.join('|') === after.join('|'));
	const dbAfterReopen = await fetchAgentOrder();
	check(
		'重新打开不会改写已保存顺序',
		dbAfterReopen.join('|') === dbAfter.join('|'),
		`db=${dbAfterReopen.join(',')}`
	);

	await closeDialog();

	// 关键回归：主页 tab 栏必须**不刷新页面**就跟着更新
	// （之前 Dialog 写的是 layout 那份 agents，主页自己那份不知道，tab 栏不刷新）
	const tabbar = await evaluate(
		client,
		`(() => [...document.querySelectorAll('[data-agent-tabbar] [data-agent-tab]')]
			.map((el) => el.dataset.agentTab))()`
	);
	console.log(`主页 tab 栏: ${tabbar.join(' | ')}`);
	check(
		'主页 tab 栏无刷新即同步新顺序',
		JSON.stringify(tabbar) === JSON.stringify(dbAfter),
		`tabbar=${tabbar.join(',')} db=${dbAfter.join(',')}`
	);

	await setOrder(dbBefore);
	const restored = await fetchAgentOrder();
	console.log(`已还原顺序: ${restored.join(' | ')}`);
	check('已还原数据库顺序', restored.join('|') === dbBefore.join('|'));

	const errors = logs.filter((l) => l.startsWith('[error]') || l.startsWith('[exception]'));
	check('页面无 JS 错误', errors.length === 0, errors.join(' / '));
} catch (error) {
	throw new Error(`${error.message}\n页面日志: ${JSON.stringify(logs.slice(-10), null, 2)}`);
} finally {
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
