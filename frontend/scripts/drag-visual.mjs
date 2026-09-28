// 拖动中途截图：确认浮动层真的"拎起来了"（尺寸/阴影/半透明是否正常）
import { spawn } from 'node:child_process';
import { mkdtemp, rm, writeFile } from 'node:fs/promises';
import net from 'node:net';
import os from 'node:os';
import path from 'node:path';

const APP_URL = 'http://127.0.0.1:8090/';
const OUT = process.env.OUT ?? '/tmp/drag-mid.png';

class C {
	constructor(u) {
		this.i = 1;
		this.p = new Map();
		this.s = new WebSocket(u);
	}
	async connect() {
		await new Promise((r, j) => {
			this.s.addEventListener('open', r, { once: true });
			this.s.addEventListener('error', j, { once: true });
		});
		this.s.addEventListener('message', (e) => {
			const m = JSON.parse(e.data);
			if (!m.id) return;
			const q = this.p.get(m.id);
			if (!q) return;
			this.p.delete(m.id);
			m.error ? q.j(new Error(m.error.message)) : q.r(m.result);
		});
	}
	send(method, params = {}) {
		const id = this.i++;
		return new Promise((r, j) => {
			this.p.set(id, { r, j });
			this.s.send(JSON.stringify({ id, method, params }));
		});
	}
}
const ev = async (c, expr) => {
	const r = await c.send('Runtime.evaluate', { expression: expr, awaitPromise: true, returnByValue: true });
	if (r.exceptionDetails) throw new Error(r.exceptionDetails.exception?.description);
	return r.result.value;
};
const port = await (async () => {
	const s = net.createServer();
	await new Promise((r) => s.listen(0, '127.0.0.1', r));
	const p = s.address().port;
	await new Promise((r) => s.close(r));
	return p;
})();
const dir = await mkdtemp(path.join(os.tmpdir(), 'dragvis-'));
const edge = spawn(
	'/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge',
	['--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
	 `--remote-debugging-port=${port}`, `--user-data-dir=${dir}`, '--window-size=1440,900', APP_URL],
	{ stdio: 'ignore' }
);
let c;
try {
	for (let i = 0; i < 100; i++) {
		try { await fetch(`http://127.0.0.1:${port}/json/version`); break; }
		catch { await new Promise((r) => setTimeout(r, 100)); }
	}
	const pages = await fetch(`http://127.0.0.1:${port}/json/list`).then((r) => r.json());
	const pg = pages.find((e) => e.type === 'page' && e.url.startsWith(APP_URL));
	c = new C(pg.webSocketDebuggerUrl);
	await c.connect();
	await c.send('Page.enable');
	await c.send('Runtime.enable');
	await c.send('Emulation.setFocusEmulationEnabled', { enabled: true }).catch(() => {});
	for (let i = 0; i < 60; i++) {
		if (await ev(c, `document.querySelectorAll('[data-skill-card]').length >= 3`)) break;
		await new Promise((r) => setTimeout(r, 200));
	}

	const pts = await ev(c, `(() => {
		const g = document.querySelector('[data-skill-grid]');
		const cards = [...g.querySelectorAll(':scope > [data-skill-card]')];
		const a = cards[0].getBoundingClientRect();
		const b = cards[cards.length - 1].getBoundingClientRect();
		return { from: { x: a.left + a.width/2, y: a.top + 24 }, to: { x: b.left + b.width/2, y: b.top + 24 } };
	})()`);

	const m = (type, x, y, buttons = 1) =>
		c.send('Input.dispatchMouseEvent', { type, x, y, button: 'left', clickCount: 1, buttons });

	await m('mouseMoved', pts.from.x, pts.from.y, 0);
	await m('mousePressed', pts.from.x, pts.from.y);
	for (let i = 1; i <= 10; i++) {
		const t = i / 10;
		await m('mouseMoved', pts.from.x + (pts.to.x - pts.from.x) * t, pts.from.y + (pts.to.y - pts.from.y) * t);
		await new Promise((r) => setTimeout(r, 60));
	}

	const ghost = await ev(c, `(() => {
		const g = document.querySelector('[data-drag-ghost]');
		if (!g) return null;
		const r = g.getBoundingClientRect();
		const s = getComputedStyle(g);
		return { w: Math.round(r.width), h: Math.round(r.height), opacity: s.opacity, shadow: s.boxShadow.slice(0, 40), z: s.zIndex, transform: s.transform };
	})()`);
	const placeholder = await ev(c, `(() => {
		const p = document.querySelector('[data-skill-card].border-dashed');
		if (!p) return null;
		const s = getComputedStyle(p);
		return { opacity: s.opacity, dashed: s.borderStyle };
	})()`);
	console.log('浮动层:', JSON.stringify(ghost));
	console.log('占位卡:', JSON.stringify(placeholder));

	const shot = await c.send('Page.captureScreenshot', { format: 'png' });
	await writeFile(OUT, Buffer.from(shot.data, 'base64'));
	console.log('截图已存:', OUT);

	await m('mouseReleased', pts.to.x, pts.to.y, 0);
} catch (e) {
	console.log('探针失败:', e.message);
	process.exitCode = 1;
} finally {
	c?.s.close();
	edge.kill();
	await new Promise((r) => setTimeout(r, 300));
	await rm(dir, { recursive: true, force: true }).catch(() => {});
}
