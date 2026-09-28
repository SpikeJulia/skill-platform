import { spawn } from 'node:child_process';
import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

const APP_URL = process.env.APP_URL ?? 'http://127.0.0.1:8090/';
const EDGE_PATH =
	process.env.EDGE_PATH ?? '/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge';
const TEST_AGENT = process.env.TEST_AGENT ?? 'dialog-e2e-test';
const HEADLESS = process.env.HEADLESS !== '0';
const ARTIFACT_DIR = path.resolve('test-artifacts/dialog-e2e');

class CdpClient {
	constructor(url) {
		this.nextId = 1;
		this.pending = new Map();
		this.listeners = new Map();
		this.socket = new WebSocket(url);
	}

	async connect() {
		await new Promise((resolve, reject) => {
			this.socket.addEventListener('open', resolve, { once: true });
			this.socket.addEventListener('error', reject, { once: true });
		});
		this.socket.addEventListener('message', (event) => {
			const message = JSON.parse(event.data);
			if (message.id) {
				const pending = this.pending.get(message.id);
				if (!pending) return;
				this.pending.delete(message.id);
				if (message.error) pending.reject(new Error(message.error.message));
				else pending.resolve(message.result);
				return;
			}

			for (const listener of this.listeners.get(message.method) ?? []) {
				listener(message.params);
			}
		});
	}

	send(method, params = {}) {
		const id = this.nextId++;
		return new Promise((resolve, reject) => {
			this.pending.set(id, { resolve, reject });
			this.socket.send(JSON.stringify({ id, method, params }));
		});
	}

	on(method, listener) {
		const listeners = this.listeners.get(method) ?? [];
		listeners.push(listener);
		this.listeners.set(method, listeners);
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
	const address = server.address();
	const port = typeof address === 'object' && address ? address.port : 0;
	await new Promise((resolve) => server.close(resolve));
	return port;
}

async function waitForHttp(url, timeoutMs = 10000) {
	const deadline = Date.now() + timeoutMs;
	let lastError;
	while (Date.now() < deadline) {
		try {
			const response = await fetch(url);
			if (response.ok) return response;
		} catch (error) {
			lastError = error;
		}
		await new Promise((resolve) => setTimeout(resolve, 100));
	}
	throw new Error(`Timed out waiting for ${url}: ${lastError ?? 'unknown error'}`);
}

async function evaluate(client, expression) {
	const result = await client.send('Runtime.evaluate', {
		expression,
		awaitPromise: true,
		returnByValue: true
	});
	if (result.exceptionDetails) {
		throw new Error(result.exceptionDetails.exception?.description ?? 'Browser evaluation failed');
	}
	return result.result.value;
}

async function waitFor(client, expression, description, timeoutMs = 5000) {
	const deadline = Date.now() + timeoutMs;
	while (Date.now() < deadline) {
		if (await evaluate(client, expression)) return;
		await new Promise((resolve) => setTimeout(resolve, 50));
	}
	throw new Error(`Timed out waiting for ${description}`);
}

async function screenshot(client, name) {
	const { data } = await client.send('Page.captureScreenshot', {
		format: 'png',
		captureBeyondViewport: false
	});
	await writeFile(path.join(ARTIFACT_DIR, `${name}.png`), Buffer.from(data, 'base64'));
}

async function click(client, expression, description) {
	const target = await evaluate(
		client,
		`(() => {
			const element = ${expression};
			if (!element) return null;
			const rect = element.getBoundingClientRect();
			const x = rect.left + rect.width / 2;
			const y = rect.top + rect.height / 2;
			const hit = document.elementFromPoint(x, y);
			const stack = document.elementsFromPoint(x, y);
			return {
				x,
				y,
				rect: { top: rect.top, left: rect.left, width: rect.width, height: rect.height },
				viewport: { width: window.innerWidth, height: window.innerHeight },
				scroll: { x: window.scrollX, y: window.scrollY },
				target: element.outerHTML.slice(0, 300),
				hit: hit?.outerHTML.slice(0, 300) ?? null,
				stack: stack.map((node) => node.tagName + (node.getAttribute?.('data-slot') ? '[' + node.getAttribute('data-slot') + ']' : '')),
				targetContainsHit: Boolean(hit && element.contains(hit))
			};
		})()`
	);
	if (!target) throw new Error(`Could not find ${description}`);
	if (!target.targetContainsHit) {
		throw new Error(
			`Pointer target for ${description} is blocked.\n` +
				`  point: ${target.x},${target.y} rect: ${JSON.stringify(target.rect)}\n` +
				`  viewport: ${JSON.stringify(target.viewport)} scroll: ${JSON.stringify(target.scroll)}\n` +
				`  hit stack: ${target.stack.join(' < ')}\n` +
				`  expected: ${target.target}`
		);
	}
	await client.send('Input.dispatchMouseEvent', {
		type: 'mouseMoved',
		x: target.x,
		y: target.y
	});
	await client.send('Input.dispatchMouseEvent', {
		type: 'mousePressed',
		x: target.x,
		y: target.y,
		button: 'left',
		clickCount: 1
	});
	await client.send('Input.dispatchMouseEvent', {
		type: 'mouseReleased',
		x: target.x,
		y: target.y,
		button: 'left',
		clickCount: 1
	});
}

const MANAGE_BUTTON = `document.querySelector('button[title="管理 agent tabs"]')`;

async function openDialog(client) {
	// 条件等待：等按钮真正可被命中，而不是加固定延时。
	// 关闭动画结束后布局可能短暂未稳定，此时 elementFromPoint 会落空。
	const deadline = Date.now() + 5000;
	let lastBlocker = null;
	while (Date.now() < deadline) {
		const state = await evaluate(
			client,
			`(() => {
				const element = ${MANAGE_BUTTON};
				if (!element) return { ready: false, reason: 'missing' };
				const rect = element.getBoundingClientRect();
				const x = rect.left + rect.width / 2;
				const y = rect.top + rect.height / 2;
				const hit = document.elementFromPoint(x, y);
				return {
					ready: Boolean(hit && element.contains(hit)),
					x,
					y,
					rect: { top: rect.top, left: rect.left, width: rect.width, height: rect.height },
					stack: document.elementsFromPoint(x, y).map((n) => n.tagName)
				};
			})()`
		);
		if (state.ready) break;
		lastBlocker = state;
		await new Promise((resolve) => setTimeout(resolve, 50));
		if (Date.now() >= deadline) {
			throw new Error(
				`manage agent tabs button never became pointer-hittable: ${JSON.stringify(lastBlocker)}`
			);
		}
	}
	await click(client, MANAGE_BUTTON, 'manage agent tabs button');
	await waitFor(client, `Boolean(document.querySelector('[role="dialog"]'))`, 'dialog to open');
}

async function expectDialogClosed(client) {
	try {
		await waitFor(client, `!document.querySelector('[role="dialog"]')`, 'dialog portal to unmount');
	} catch (error) {
		const diagnostics = await evaluate(
			client,
			`(() => {
				const content = document.querySelector('[data-slot="dialog-content"]');
				const overlay = document.querySelector('[data-slot="dialog-overlay"]');
				const describe = (element) => element && ({
					html: element.outerHTML.slice(0, 500),
					style: {
						display: getComputedStyle(element).display,
						pointerEvents: getComputedStyle(element).pointerEvents,
						animationName: getComputedStyle(element).animationName,
						animationDuration: getComputedStyle(element).animationDuration,
						animationIterationCount: getComputedStyle(element).animationIterationCount
					},
					animations: element.getAnimations().map((animation) => ({
						playState: animation.playState,
						pending: animation.pending,
						currentTime: animation.currentTime,
						effectTiming: animation.effect?.getComputedTiming()
					}))
				});
				return {
					page: {
						visibilityState: document.visibilityState,
						hidden: document.hidden,
						hasFocus: document.hasFocus(),
						timelineCurrentTime: document.timeline.currentTime
					},
					content: describe(content),
					overlay: describe(overlay)
				};
			})()`
		);
		throw new Error(`${error.message}\n${JSON.stringify(diagnostics, null, 2)}`);
	}
}

async function createTestAgent() {
	const current = await fetch(`${APP_URL}api/agents`).then((response) => response.json());
	if (current.agents.some((agent) => agent.name === TEST_AGENT)) return;

	const response = await fetch(`${APP_URL}api/agents`, {
		method: 'POST',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify({ name: TEST_AGENT, label: TEST_AGENT })
	});
	if (!response.ok) {
		throw new Error(`Failed to create test agent: ${response.status} ${await response.text()}`);
	}
}

async function run() {
	await mkdir(ARTIFACT_DIR, { recursive: true });
	await createTestAgent();

	const port = await reservePort();
	const profileDir = await mkdtemp(path.join(os.tmpdir(), 'skill-platform-edge-'));
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

	let client;
	try {
		await waitForHttp(`http://127.0.0.1:${port}/json/version`);
		const pages = await fetch(`http://127.0.0.1:${port}/json/list`).then((response) => response.json());
		const page = pages.find((entry) => entry.type === 'page' && entry.url.startsWith(APP_URL));
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

		const browserErrors = [];
		client.on('Runtime.exceptionThrown', ({ exceptionDetails }) => {
			browserErrors.push(exceptionDetails.exception?.description ?? exceptionDetails.text);
		});
		client.on('Log.entryAdded', ({ entry }) => {
			if (entry.level === 'error') browserErrors.push(entry.text);
		});

		await waitFor(client, `document.readyState === 'complete'`, 'page load');
		await waitFor(
			client,
			`Boolean(document.querySelector('button[title="管理 agent tabs"]'))`,
			'app hydration'
		);
		await openDialog(client);
		await screenshot(client, '01-open');
		console.log('PASS open dialog');

		await click(
			client,
			`[...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.trim() === '完成')`,
			'complete button'
		);
		await expectDialogClosed(client);
		console.log('PASS complete closes dialog');

		await openDialog(client);
		await click(client, `document.querySelector('[data-slot="dialog-close"]')`, 'close button');
		await expectDialogClosed(client);
		console.log('PASS X closes dialog');

		await openDialog(client);
		await client.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Escape', code: 'Escape' });
		await client.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Escape', code: 'Escape' });
		await expectDialogClosed(client);
		console.log('PASS Escape closes dialog');

		await openDialog(client);
		await waitFor(
			client,
			`Boolean(document.querySelector('button[title="删除 ${TEST_AGENT}"]'))`,
			'test agent to appear'
		);
		const dialogConfirmation = new Promise((resolve) => {
			client.on('Page.javascriptDialogOpening', async () => {
				await client.send('Page.handleJavaScriptDialog', { accept: true });
				resolve();
			});
		});
		await click(
			client,
			`document.querySelector('button[title="删除 ${TEST_AGENT}"]')`,
			'test agent delete button'
		);
		await dialogConfirmation;
		await waitFor(
			client,
			`!document.querySelector('button[title="删除 ${TEST_AGENT}"]')`,
			'test agent deletion',
			10000
		);
		await screenshot(client, '02-after-delete');
		await click(
			client,
			`[...document.querySelectorAll('[role="dialog"] button')].find((button) => button.textContent.trim() === '完成')`,
			'complete button after delete'
		);
		await expectDialogClosed(client);
		console.log('PASS delete then complete closes dialog');

		await openDialog(client);
		await click(client, `document.querySelector('[data-slot="dialog-close"]')`, 'close button after reopen');
		await expectDialogClosed(client);
		console.log('PASS parent open state remains synchronized');

		const agents = await fetch(`${APP_URL}api/agents`).then((response) => response.json());
		if (agents.agents.some((agent) => agent.name === TEST_AGENT)) {
			throw new Error('Temporary test agent still exists after browser deletion');
		}
		if (browserErrors.length) {
			throw new Error(`Browser errors:\n${browserErrors.join('\n')}`);
		}
		console.log('PASS temporary agent removed and browser console is clean');
	} finally {
		client?.close();
		edge.kill('SIGTERM');
		await Promise.race([
			new Promise((resolve) => edge.once('exit', resolve)),
			new Promise((resolve) => setTimeout(resolve, 2000))
		]);
		for (let attempt = 0; attempt < 5; attempt += 1) {
			try {
				await rm(profileDir, { recursive: true, force: true, maxRetries: 3, retryDelay: 100 });
				break;
			} catch (error) {
				if (attempt === 4) console.warn(`Could not remove temporary Edge profile: ${error.message}`);
				else await new Promise((resolve) => setTimeout(resolve, 200));
			}
		}
	}
}

run().catch((error) => {
	console.error(error.stack ?? error);
	process.exitCode = 1;
});
