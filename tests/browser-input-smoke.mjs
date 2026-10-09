// Real custom Go nodes demonstrate protocol-owned fields without platform edits.
import { readFile } from 'node:fs/promises';
import assert from 'node:assert/strict';
const { chromium } = await import(process.env.DISTVIS_PLAYWRIGHT_MODULE || 'playwright');
const base = process.env.DISTVIS_URL || 'http://localhost:3000';
async function api(path, data) {
  const res = await fetch(base + path, data === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance' }, body: JSON.stringify(data) });
  const result = await res.json();
  assert.ok(res.ok, JSON.stringify(result)); return result;
}
assert.ok(!(await api('/api/runs')).some(r => ['running', 'starting'].includes(r.status)), 'coordinator must be idle');
const source = await readFile(new URL('fixtures/input-node.go', import.meta.url), 'utf8');
const run = await api('/api/runs', { name: '应用输入验收 · 自定义 Go', protocol: 'custom', runtime: 'docker', nodeCount: 2, source });
const browser = await chromium.launch({ headless: true, ...(process.env.DISTVIS_BROWSER_PATH ? { executablePath: process.env.DISTVIS_BROWSER_PATH } : {}) });
const page = await browser.newPage({ viewport: { width: 1440, height: 1100 } });
const errors = [];
page.on('pageerror', e => errors.push(e.message));
try {
  await page.goto(`${base}/#experiment/${run.config.experimentId}/visual`);
  // Nodes that declare inputs carry a badge; activating it opens the popover at the first field.
  await page.locator('#graph [data-input-node="node-1"]').click({ timeout: 360000 });
  await page.locator('#application-panel [name="text"]').waitFor();
  assert.equal(await page.evaluate(() => document.activeElement?.name), 'text', 'badge focuses the first input field');
  const panel = page.locator('#application-panel');
  await panel.locator('[name="text"]').fill('输入 <script> & 中文');
  await panel.locator('[name="count"]').fill('0');
  await panel.locator('[name="enabled"]').uncheck();
  await panel.locator('[name="mode"]').selectOption('second');
  await panel.locator('[name="data"]').fill('{"nested":[1,null,false]}');
  await page.locator('#graph [data-node="node-2"]').click();
  assert.equal(await panel.locator('[name="note"]').count(), 1);
  assert.equal(await panel.locator('[name="count"]').count(), 0, 'schemas belong to individual nodes');
  await panel.locator('[name="note"]').fill('node two');
  await panel.locator('[type="submit"]').click();
  await page.waitForFunction(() => document.querySelector('#node-panel [data-field="lastInput"]')?.textContent.includes('node two'));
  await page.locator('#graph [data-node="node-1"]').click();
  assert.equal(await panel.locator('[name="text"]').inputValue(), '输入 <script> & 中文', 'per-node drafts survive switching');
  await panel.locator('[type="submit"]').click();
  await page.waitForFunction(() => document.querySelector('#node-panel [data-field="lastInput"]')?.textContent.includes('nested'));
  const detail = await api(`/api/runs/${run.id}`);
  const command = detail.events.findLast(e => e.type === 'command' && e.node === 'node-1');
  const state = detail.events.findLast(e => e.type === 'state' && e.node === 'node-1').state;
  assert.deepEqual(command.values, { text: '输入 <script> & 中文', count: 0, enabled: false, mode: 'second', data: { nested: [1, null, false] } });
  assert.deepEqual(state.lastInput, command.values, 'actual Go node receives typed values unchanged');
  assert.equal(state.action, 'customInput');
  assert.equal(state.inputId, `input-${command.seq}`);
  await page.locator('[data-view="spacetime"]').click();
  await page.locator('#spacetime-window').selectOption('30000');
  assert.ok(await page.locator('.time-state.application-input').count());
  await page.locator('.time-state.application-input').first().focus();
  await page.keyboard.press('Enter');
  await page.locator('#event-dialog[open]').waitFor();
  assert.ok((await page.locator('#event-summary').textContent()).length > 0);
  await page.locator('#event-dialog .close-dialog').click();
  await page.screenshot({ path: 'artifacts/application-input.png', fullPage: true });
  // On a phone the same form is a bottom sheet; boolean labels and the JSON field are comfortable targets.
  await page.setViewportSize({ width: 390, height: 844 });
  const targets = await page.locator('#element-popover :is(button, input:not([type=checkbox]), select, textarea, summary, label.checkbox):visible')
    .evaluateAll(els => els.map(el => { const r = el.getBoundingClientRect(); return { name: el.name || el.textContent.trim().slice(0, 20), w: r.width, h: r.height }; }));
  assert.ok(targets.some(t => t.name === 'data') && targets.some(t => t.name.startsWith('启用')), 'the JSON field and the boolean label are measured');
  assert.deepEqual(targets.filter(t => t.w < 36 || t.h < 36), [], 'sheet targets are at least 40px');
  await page.setViewportSize({ width: 1440, height: 1100 });
  await api(`/api/runs/${run.id}/stop`, {});
  await page.waitForFunction(() => document.querySelector('#application-panel button').disabled);
  const archive = await api(`/api/runs/${run.id}/export`);
  assert.deepEqual(archive.events.find(e => e.seq === command.seq).values, command.values);
  assert.deepEqual(errors, []);
  console.log(`PASS: custom Go declarations, all field types, per-node forms/drafts, typed transport, input markers and archive (${run.id})`);
} finally {
  await browser.close();
  if ((await api(`/api/runs/${run.id}`)).status === 'running') await api(`/api/runs/${run.id}/stop`, {});
}
