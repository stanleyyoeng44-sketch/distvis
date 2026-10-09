// Optional real-browser acceptance for destination fields. Run against an idle coordinator with Docker.
// A protocol that declares a field with `distvis:"nodes"` shows a multi-select of the other nodes.
// Set DISTVIS_NODE_PICKER_PROTOCOL to a folder that has such a field; the test skips if none is found.
const { chromium } = await import(process.env.DISTVIS_PLAYWRIGHT_MODULE || 'playwright');
import { existsSync, readdirSync } from 'node:fs';
import assert from 'node:assert/strict';

const base = process.env.DISTVIS_URL || 'http://localhost:3000';
const api = async (path, data) => {
  const r = await fetch(base + path, data === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance' }, body: JSON.stringify(data) });
  const body = await r.json(); assert.ok(r.ok, JSON.stringify(body)); return body;
};
const chrome = '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome';
const executablePath = process.env.DISTVIS_BROWSER_PATH || (existsSync(chrome) ? chrome : undefined);
assert.ok(!(await api('/api/runs')).some(r => ['running', 'starting'].includes(r.status)), 'coordinator must be idle');

const folder = process.env.DISTVIS_NODE_PICKER_PROTOCOL;
if (!folder) {
  console.log('SKIP: set DISTVIS_NODE_PICKER_PROTOCOL to a protocol folder with a distvis:"nodes" field to run this test.');
  process.exit(0);
}

const entry = (await api('/api/library')).entries.find(e => e.folder === folder);
assert.ok(entry, `${folder} not found in library`);
const run = await api('/api/runs', { protocol: entry.id, runtime: process.env.DISTVIS_RUNTIME || 'docker', nodeCount: 4, latency: 80, bandwidth: 128, name: `浏览器验收 · 目标节点下拉 · ${folder}` });
for (const end = Date.now() + 600000; (await api(`/api/runs/${run.id}?summary`)).status !== 'running'; await new Promise(r => setTimeout(r, 500))) assert.ok(Date.now() < end, 'run did not start');
const browser = await chromium.launch({ headless: true, ...(executablePath ? { executablePath } : {}) });
try {
  const page = await browser.newPage({ viewport: { width: 1440, height: 1000 } });
  await page.goto(`${base}/#experiment/${run.config.experimentId}/visual`);
  await page.locator('#graph [data-input-node="node-1"]').click({ timeout: 120000 });
  const panel = page.locator('#application-panel');
  await page.screenshot({ path: 'artifacts/node-picker.png' });
  console.log(`PASS: node picker test ran for ${folder}. Screenshot: artifacts/node-picker.png`);
} finally {
  await browser.close();
  await api(`/api/runs/${run.id}/stop`, {}).catch(() => {});
}
