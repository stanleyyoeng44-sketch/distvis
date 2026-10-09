// Optional real-browser acceptance for `// distvis:start`. Run against an idle local coordinator
// with Docker. A protocol whose nodes stay silent shows the start instruction read from its Go
// header; the button opens that node's input pre-filled, and sending it produces messages.
const { chromium } = await import(process.env.DISTVIS_PLAYWRIGHT_MODULE || 'playwright');
import { mkdir } from 'node:fs/promises';
import { existsSync } from 'node:fs';
import assert from 'node:assert/strict';

const base = process.env.DISTVIS_URL || 'http://localhost:3000', folder = process.env.DISTVIS_START_PROTOCOL || 'grpc-broadcast';
const api = async (path, data) => {
  const r = await fetch(base + path, data === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance' }, body: JSON.stringify(data) });
  const body = await r.json(); assert.ok(r.ok, JSON.stringify(body)); return body;
};
const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const executablePath = process.env.DISTVIS_BROWSER_PATH || (existsSync(chrome) ? chrome : undefined);
assert.ok(!(await api('/api/runs')).some(r => ['running', 'starting'].includes(r.status)), 'coordinator must be idle');
const entry = (await api('/api/library')).entries.find(e => e.folder === folder);
assert.ok(entry?.start, `${folder} must declare // distvis:start`);
const run = await api('/api/runs', { protocol: entry.id, runtime: process.env.DISTVIS_RUNTIME || 'docker', nodeCount: 3, latency: 80, bandwidth: 128, name: `浏览器验收 · 起点提示 · ${folder}` });
for (const end = Date.now() + 600000; (await api(`/api/runs/${run.id}?summary`)).status !== 'running'; await new Promise(r => setTimeout(r, 500))) {
  assert.ok(Date.now() < end, 'run did not start');
}
const browser = await chromium.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
const errors = [];
try {
  await mkdir('artifacts', { recursive: true });
  // Mobile first: the desktop pass sends the start input, after which the hint correctly disappears.
  for (const [label, viewport] of [['mobile', { width: 390, height: 844 }], ['desktop', { width: 1440, height: 1000 }]]) {
    const page = await browser.newPage({ viewport });
    page.on('pageerror', e => errors.push(e.message));
    await page.goto(`${base}/#experiment/${run.config.experimentId}/visual`);
    const hint = page.locator('#start-hint');
    await hint.waitFor({ state: 'visible', timeout: 30000 });
    const text = await page.locator('#start-hint-text').textContent();
    assert.ok(text.includes(entry.start.node) && text.includes(entry.start.method) && text.includes(entry.start.note), `hint shows the code comment: ${text}`);
    const box = await hint.boundingBox();
    assert.ok(box && box.x >= 0 && box.x + box.width <= viewport.width + 1, `${label}: hint fits the viewport`);
    await page.screenshot({ path: `artifacts/start-hint-${label}.png` });
    if (label === 'mobile') { await page.close(); continue; }
    await page.locator('#start-hint-open').click();
    await page.locator('#element-popover').waitFor({ state: 'visible' });
    assert.equal((await page.locator('#popover-title').textContent()).trim(), entry.start.node, 'popover opens the start node');
    const form = page.locator('#application-panel form:not([hidden])');
    assert.equal(await form.getAttribute('data-label'), entry.start.method, 'start input is selected');
    for (const [name, value] of Object.entries(entry.start.values)) assert.equal(await form.locator(`[name="${name}"]`).inputValue(), String(value), `${name} pre-filled`);
    assert.match(await page.locator('#application-panel .start-note').textContent(), /distvis:start/);
    await form.locator('button[type="submit"]').click();
    await hint.waitFor({ state: 'hidden', timeout: 20000 });
    assert.ok(Number((await page.locator('#metric-messages').textContent()).replace(/,/g, '')) > 0, 'the start input produced messages');
    await page.screenshot({ path: 'artifacts/start-hint-after-send.png' });
    await page.close();
  }
  assert.deepEqual(errors, []);
  console.log(`PASS: ${folder} start hint read from distvis:start, opened ${entry.start.node} ${entry.start.method} pre-filled, sent, and messages appeared; desktop and mobile layout checked.`);
} finally {
  await browser.close();
  await api(`/api/runs/${run.id}/stop`, {}).catch(() => {});
}
