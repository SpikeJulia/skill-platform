// 验证主页「未纳管」区块：
//   (1) 造一个未纳管 skill（~/.<agent>/skills/ 里的真目录），sync.sh 后应出现在清单
//   (2) 折叠时只显示摘要，展开后才有 local 卡片
//   (3) local 卡片带 description + 一行可复制的 mv 纳管命令
//   (4) 复制按钮把命令写进剪贴板
//   (5) 再次点击可收起
// 背景：容器看不到 ~/.<agent>/skills/，清单由宿主 sync.sh 生成、平台只读。
// 实现同样是 CDP 真实指针事件（内置浏览器快照在 Svelte 5 静态产物下会 STALE）。
import { spawn } from 'node:child_process';
import { mkdir, mkdtemp, rm, writeFile } from 'node:fs/promises';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

const APP_URL = process.env.APP_URL ?? 'http://127.0.0.1:8090/';
const EDGE_PATH =
	process.env.EDGE_PATH ?? '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const HEADLESS = process.env.HEADLESS !== '0';

const NAME = 'zzz-unmanaged-e2e';
const AGENT = 'minimax';
const REAL_DIR = `${process.env.HOME}/.${AGENT}/skills/${NAME}`;
const PERSONAL_DIR = `${process.env.HOME}/AI/agent-skills-personal/${AGENT}/${NAME}`;

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

async function waitFor(client, expr, desc, timeoutMs = 10000) {
	const end = Date.now() + timeoutMs;
	while (Date.now() < end) {
		if (await evaluate(client, expr)) return;
		await new Promise((r) => setTimeout(r, 50));
	}
	throw new Error(`Timed out waiting for ${desc}`);
}

/** 真实指针点击：取元素中心，派发 pressed + released */
async function clickSel(client, selector) {
	const box = await evaluate(
		client,
		`(() => { const e = document.querySelector(${JSON.stringify(selector)});
		   if (!e) return null; const r = e.getBoundingClientRect();
		   return { x: r.x + r.width / 2, y: r.y + r.height / 2 }; })()`
	);
	if (!box) return false;
	for (const type of ['mousePressed', 'mouseReleased']) {
		await client.send('Input.dispatchMouseEvent', {
			type,
			x: box.x,
			y: box.y,
			button: 'left',
			clickCount: 1,
			buttons: type === 'mouseReleased' ? 0 : 1
		});
	}
	await new Promise((r) => setTimeout(r, 250));
	return true;
}

function runSync() {
	return new Promise((res) => {
		const p = spawn('bash', [`${process.env.HOME}/AI/agent-skills/sync.sh`], { stdio: 'ignore' });
		p.on('exit', res);
	});
}

const api = (p) => fetch(APP_URL.replace(/\/$/, '') + p).then((r) => r.json());

async function setup() {
	await mkdir(REAL_DIR, { recursive: true });
	await writeFile(
		`${REAL_DIR}/SKILL.md`,
		`---\nname: ${NAME}\ndescription: 未纳管区块的端到端验证用 skill\n---\n\n# e2e\n`
	);
	await runSync();
	await new Promise((r) => setTimeout(r, 400));
}

async function teardown() {
	await rm(PERSONAL_DIR, { recursive: true, force: true });
	await rm(REAL_DIR, { recursive: true, force: true });
	await runSync();
	await new Promise((r) => setTimeout(r, 400));
}

const failures = [];
const logs = [];
function check(label, ok, detail = '') {
	console.log(`${ok ? 'PASS' : 'FAIL'} ${label}${detail ? ` — ${detail}` : ''}`);
	if (!ok) failures.push(label);
}

const port = await reservePort();
const profileDir = await mkdtemp(path.join(os.tmpdir(), 'skill-platform-unmanaged-'));
const edge = spawn(
	EDGE_PATH,
	[
		...(HEADLESS ? ['--headless=new'] : []),
		'--disable-gpu',
		'--no-first-run',
		'--no-default-browser-check',
		`--remote-debugging-port=${port}`,
		`--user-data-dir=${profileDir}`,
		'--window-size=1440,1100',
		APP_URL
	],
	{ stdio: 'ignore' }
);

let client;
try {
	await setup();

	// ---- 接口层断言（不依赖浏览器）----
	const pre = await api('/api/unmanaged');
	check(
		'清单里出现刚造的 local skill',
		pre.unmanaged.some((u) => u.name === NAME && u.kind === 'local'),
		`count=${pre.count}`
	);
	check(
		'local 条目带上了 description',
		pre.unmanaged.find((u) => u.name === NAME)?.description?.includes('端到端验证用')
	);
	check(
		'造出来时它还不在已纳管列表（这正是问题本身）',
		!(await api('/api/skills')).skills.some((s) => s.name === NAME)
	);
	check(
		'builtin 也在清单里（agent 自带那 20 个）',
		pre.unmanaged.filter((u) => u.kind === 'builtin').length > 0
	);

	// ---- 界面层断言 ----
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
	await client.send('Emulation.setFocusEmulationEnabled', { enabled: true }).catch(() => {});
	client.socket.addEventListener('message', (ev) => {
		const m = JSON.parse(ev.data);
		if (m.method === 'Log.entryAdded') logs.push(`[${m.params.entry.level}] ${m.params.entry.text}`);
		if (m.method === 'Runtime.exceptionThrown')
			logs.push(`[exception] ${m.params.exceptionDetails.exception?.description ?? ''}`);
	});

	await waitFor(client, `document.readyState === 'complete'`, 'page load');
	await waitFor(client, `!!document.querySelector('[data-unmanaged-section]')`, '未纳管区块');

	check(
		'折叠态只显示摘要，不渲染 local 卡片',
		!(await evaluate(client, `!!document.querySelector('[data-unmanaged-local]')`))
	);

	await clickSel(client, '[data-unmanaged-toggle]');
	await waitFor(client, `!!document.querySelector('[data-unmanaged-local="${NAME}"]')`, 'local 卡片');

	check(
		'local 卡片显示了 description',
		await evaluate(
			client,
			`document.querySelector('[data-unmanaged-local="${NAME}"]')?.innerText.includes('端到端验证用 skill')`
		)
	);
	const cmd = await evaluate(
		client,
		`document.querySelector('[data-unmanaged-local="${NAME}"] code')?.textContent ?? ''`
	);
	check(
		'给出的是 mv 纳管命令（不是 cp —— cp 会留下两份各自漂移）',
		cmd.startsWith('mv ') && cmd.includes(`agent-skills-personal/${AGENT}`) && cmd.includes('sync.sh'),
		cmd.slice(0, 60) + '…'
	);
	check(
		'builtin 折叠成小徽章列出',
		(await evaluate(
			client,
			`document.querySelectorAll('[data-unmanaged-section] span[title]').length`
		)) > 10
	);

	// 剪贴板：headless 下先授权，再点复制按钮
	await client.send('Browser.grantPermissions', {
		origin: APP_URL.replace(/\/$/, ''),
		permissions: ['clipboardReadWrite', 'clipboardSanitizedWrite']
	}).catch(() => {});
	await clickSel(client, `[data-unmanaged-local="${NAME}"] button`);
	await new Promise((r) => setTimeout(r, 300));
	const clip = await evaluate(client, `navigator.clipboard.readText().catch(() => 'ERR')`);
	check(
		'复制按钮把命令写进剪贴板',
		typeof clip === 'string' && clip.startsWith('mv ') && clip.includes(NAME),
		String(clip).slice(0, 50) + '…'
	);

	await clickSel(client, '[data-unmanaged-toggle]');
	check(
		'再次点击可收起',
		!(await evaluate(client, `!!document.querySelector('[data-unmanaged-local]')`))
	);

	const errs = logs.filter((l) => l.startsWith('[error]') || l.startsWith('[exception]'));
	check('页面无 JS 错误', errs.length === 0, errs.join(' | ') || '干净');
} catch (e) {
	console.error('E2E 崩溃:', e.message);
	failures.push('E2E 崩溃');
} finally {
	client?.close();
	edge.kill();
	await teardown().catch(() => {});
	await rm(profileDir, { recursive: true, force: true }).catch(() => {});
}

console.log('\n' + '-'.repeat(50));
if (failures.length) console.log(`${failures.length} 项断言失败: ${failures.join('; ')}`);
else console.log('全部断言通过');
process.exit(failures.length ? 1 : 0);
