// Optional real-browser layout acceptance for the visualization view. Run against an idle local coordinator.
// Measures the space budgets (header, graph share, sidebar), popover anchoring and focus, the mobile
// sheet, and topology geometry for 2, 5 and 12 nodes. Uses the built-in reference model only.
const { chromium } = await import(process.env.DISTVIS_PLAYWRIGHT_MODULE || 'playwright');
import { mkdir } from 'node:fs/promises';
import assert from 'node:assert/strict';
const base = process.env.DISTVIS_URL || 'http://localhost:3000';
const tolerance = 4; // font rendering differs slightly between Chromium builds
async function api(path, data) {
  const res = await fetch(base + path, data === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-Distvis-Purpose': 'acceptance' }, body: JSON.stringify(data) });
  const result = await res.json();
  assert.ok(res.ok, JSON.stringify(result)); return result;
}
assert.ok(!(await api('/api/runs')).some(r => ['running', 'starting'].includes(r.status)), 'coordinator must be idle');
const browser = await chromium.launch({ headless: true, ...(process.env.DISTVIS_BROWSER_PATH ? { executablePath: process.env.DISTVIS_BROWSER_PATH } : {}) });
const page = await browser.newPage({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
await page.setExtraHTTPHeaders({ 'X-Distvis-Purpose': 'acceptance' });
const errors = [], started = [];
page.on('pageerror', e => errors.push(e.message));
const rect = selector => page.locator(selector).first().evaluate(el => { const r = el.getBoundingClientRect(); return { left: r.left, right: r.right, top: r.top, bottom: r.bottom, width: r.width, height: r.height }; });
const overlaps = (a, b) => a.left < b.right && a.right > b.left && a.top < b.bottom && a.bottom > b.top;
const noPageOverflow = () => page.evaluate(() => document.documentElement.scrollWidth <= innerWidth);
async function open(nodeCount) {
  const run = await api('/api/runs', { name: `布局验收 · ${nodeCount} 节点`, protocol: 'raft', runtime: 'simulation', nodeCount });
  started.push(run.id);
  // A fresh load, so the client's experiment list already contains the new experiment.
  await page.goto('about:blank');
  await page.goto(`${base}/#experiment/${run.config.experimentId}/visual`);
  await page.waitForFunction(count => document.querySelectorAll('#graph .graph-node').length === count, nodeCount);
  await page.locator('#run-status').filter({ hasText: '运行中' }).waitFor();
  return run;
}
async function stop(run) { await api(`/api/runs/${run.id}/stop`, {}); }
try {
  await mkdir('artifacts', { recursive: true });
  const five = await open(5);

  // Space budget at 1440×900.
  const graph = await rect('.graph-area');
  assert.ok(graph.top <= 104 + tolerance, `chrome above the graph is ${graph.top}px`);
  assert.ok(graph.height >= 900 * 0.6 - tolerance, `graph area is ${graph.height}px tall`);
  assert.equal((await rect('.sidebar')).width, 0, 'navigation is hidden in the visualization view');
  const primary = await rect('#stop-run');
  assert.ok(primary.top >= 0 && primary.bottom <= 900, 'the primary run control is on the first screen');
  assert.equal(await page.locator('#new-run').isVisible(), false, 'only one primary action is shown');
  assert.equal(await page.locator('#element-popover').isVisible(), false, 'opening a run does not open the popover');
  assert.equal(await page.locator('#graph').getAttribute('viewBox'), `0 0 ${Math.floor(graph.width)} ${Math.floor(graph.height)}`, 'topology uses the whole container');
  for (const gone of ['.inspector-tabs', '#fault-kind', '#fault-node', '#fault-from', '#fault-to', '.experiment-bar', '#metric-time']) assert.equal(await page.locator(gone).count(), 0, `${gone} removed`);
  await page.screenshot({ path: 'artifacts/layout-desktop.png' });

  // Temporary drawer: opens from the header, keeps navigation reachable, never changes the saved preference.
  const saved = await page.evaluate(() => localStorage.getItem('distvis-sidebar-collapsed'));
  await page.locator('#toggle-sidebar').click();
  assert.ok((await rect('.sidebar')).width > 150);
  for (const target of ['#nav-library', '#create-experiment-side', '.sidebar-section', '#nav-guide']) assert.ok(await page.locator(target).isVisible(), `${target} reachable from the drawer`);
  assert.notEqual(await page.locator('#recent-experiments').evaluate(el => getComputedStyle(el).display), 'none', 'recent protocols are listed in the drawer');
  await page.keyboard.press('Escape');
  assert.equal((await rect('.sidebar')).width, 0);
  assert.equal(await page.evaluate(() => localStorage.getItem('distvis-sidebar-collapsed')), saved);

  // The popover sits beside its node, inside the graph, and survives live redraws while typing.
  await page.locator('#graph [data-node="node-1"]').click();
  await page.locator('#element-popover').waitFor();
  const pop = await rect('#element-popover'), node = await rect('#graph [data-node="node-1"]'), area = await rect('.graph-area');
  assert.ok(!overlaps(pop, node), 'popover never covers its node');
  assert.ok(pop.left >= area.left && pop.right <= area.right && pop.top >= area.top && pop.bottom <= area.bottom + 1, 'popover stays inside the graph area');
  assert.ok(pop.top >= (await rect('.graph-toolbar')).bottom, 'popover leaves the view toolbar usable');
  const input = page.locator('#application-panel input[name="number"]');
  await input.fill('123');
  const before = await page.evaluate(() => document.querySelectorAll('#graph .packet').length + ':' + document.querySelector('#event-count').textContent);
  await page.waitForTimeout(1500);
  assert.notEqual(await page.evaluate(() => document.querySelectorAll('#graph .packet').length + ':' + document.querySelector('#event-count').textContent), before, 'live events kept arriving');
  assert.equal(await page.evaluate(() => document.activeElement?.name), 'number', 'focus survives live redraws');
  assert.equal(await input.inputValue(), '123');
  const after = await rect('#element-popover');
  assert.ok(Math.abs(after.left - pop.left) <= 2 && Math.abs(after.top - pop.top) <= 2, 'popover stays anchored during redraws');

  // Escape closes the innermost layer first: drawer, then popover; focus returns to the node.
  await page.locator('#toggle-sidebar').click();
  await page.keyboard.press('Escape');
  assert.equal((await rect('.sidebar')).width, 0);
  assert.ok(await page.locator('#element-popover').isVisible(), 'the drawer closes before the popover');
  await page.locator('#popover-title').focus();
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#element-popover').isVisible(), false);
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.node), 'node-1', 'focus returns to the node');

  // Links: from the graph (lower → higher index) and by keyboard through the peer list.
  await page.locator('#graph [data-from="node-1"][data-to="node-3"]').dispatchEvent('click');
  assert.equal(await page.locator('#popover-title').textContent(), 'node-1 ↔ node-3');
  assert.equal(await page.locator('#dir-ab').getAttribute('aria-pressed'), 'true');
  assert.ok(!overlaps(await rect('#element-popover'), await rect('#graph [data-from="node-1"][data-to="node-3"]').then(r => ({ left: (r.left + r.right) / 2 - 4, right: (r.left + r.right) / 2 + 4, top: (r.top + r.bottom) / 2 - 4, bottom: (r.top + r.bottom) / 2 + 4 }))), 'popover leaves the link midpoint visible');
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#popover-title').textContent(), 'node-1', 'Escape in link mode returns to the node');
  await page.locator('#peer-links [data-peer="node-2"]').focus();
  await page.keyboard.press('Enter');
  assert.equal(await page.locator('#popover-title').textContent(), 'node-1 ↔ node-2');
  await page.keyboard.press('Escape'); await page.keyboard.press('Escape');

  // The input badge opens the popover with the cursor in the first field; closing returns to that badge,
  // even after live redraws replaced the element.
  async function badgeRoundTrip(close) {
    await page.locator('#graph [data-input-node="node-2"]').click();
    assert.equal(await page.evaluate(() => document.activeElement?.name), 'number');
    await page.waitForTimeout(400);
    if (close === 'Escape') await page.keyboard.press('Escape'); else await page.locator('#popover-close').click();
    assert.equal(await page.locator('#element-popover').isVisible(), false);
    assert.equal(await page.evaluate(() => document.activeElement?.dataset.inputNode), 'node-2', `${close} returns focus to the badge`);
  }
  await badgeRoundTrip('Escape'); await badgeRoundTrip('close');

  // Geometry follows the container without a resize feedback loop.
  await page.setViewportSize({ width: 1100, height: 900 });
  await page.waitForTimeout(300);
  const narrow = await page.locator('#graph').getAttribute('viewBox');
  assert.equal(narrow.split(' ')[2], String(Math.floor((await rect('.graph-area')).width)));
  await page.waitForTimeout(500);
  assert.equal(await page.locator('#graph').getAttribute('viewBox'), narrow, 'geometry is stable after a resize');

  // Phone: no page overflow, a usable graph, a closed sheet, a bottom sheet on tap, large targets.
  await page.setViewportSize({ width: 390, height: 844 });
  await page.reload();
  await page.waitForFunction(() => document.querySelectorAll('#graph .graph-node').length === 5);
  assert.ok(await noPageOverflow(), 'no horizontal page scroll at 390px');
  assert.ok((await rect('.graph-area')).height >= 300 - tolerance);
  assert.equal(await page.locator('#element-popover').isVisible(), false, 'the sheet is closed (0px) until a node is tapped');
  const small = await page.locator('.run-strip button:visible, .run-strip select:visible, .run-strip summary:visible').evaluateAll(els => els.map(el => ({ id: el.id || el.textContent.trim(), h: el.getBoundingClientRect().height, w: el.getBoundingClientRect().width })).filter(t => t.h < 36 || t.w < 36));
  assert.deepEqual(small, [], 'strip targets are at least 40px');
  // Strip menus open fully inside the phone viewport, whichever row their button wrapped to.
  for (const menu of ['#network-menu', '#run-details']) {
    await page.locator(`${menu} > summary`).click();
    const box = await rect(`${menu} .menu-content`);
    assert.ok(box.left >= 0 && box.right <= 390, `${menu} stays inside the viewport (${box.left}..${box.right})`);
    assert.ok(await noPageOverflow());
    if (menu === '#network-menu') {
      const heal = await rect('#heal-links'), history = await rect('#fault-history');
      assert.ok(heal.left >= 0 && heal.right <= 390 && history.left >= 0 && history.right <= 390, 'heal and fault history are fully visible');
    }
    await page.keyboard.press('Escape');
    assert.equal(await page.locator(`${menu}[open]`).count(), 0);
  }
  await page.locator('#graph [data-node="node-1"]').click();
  const sheet = await rect('#element-popover');
  assert.ok(Math.abs(sheet.bottom - 844) <= 1 && sheet.height <= 844 * 0.6 + 1 && sheet.left <= 0.5 && sheet.right >= 389.5, 'popover becomes a bottom sheet');
  assert.ok(await noPageOverflow());
  // Every interactive sheet control, including checkbox labels and disclosure summaries, is at least 40×40.
  const sheetTargets = () => page.locator('#element-popover :is(button, input:not([type=checkbox]), select, textarea, summary, label.checkbox):visible')
    .evaluateAll(els => els.map(el => { const r = el.getBoundingClientRect(); return { id: el.id || el.textContent.trim().slice(0, 20), w: r.width, h: r.height }; }));
  const tooSmall = targets => targets.filter(t => t.w < 36 || t.h < 36);
  const nodeTargets = await sheetTargets();
  assert.ok(nodeTargets.some(t => t.id.startsWith('完整状态')), 'the raw-state summary is measured');
  assert.deepEqual(tooSmall(nodeTargets), [], 'node sheet targets are at least 40px');
  await page.locator('#peer-links [data-peer="node-2"]').click();
  const linkTargets = await sheetTargets();
  assert.ok(linkTargets.some(t => t.id.startsWith('中断')), 'the link checkbox label is measured');
  assert.deepEqual(tooSmall(linkTargets), [], 'link sheet targets are at least 40px');
  await page.keyboard.press('Escape');
  await page.screenshot({ path: 'artifacts/layout-mobile.png' });
  await page.keyboard.press('Escape');
  assert.equal(await page.locator('#element-popover').isVisible(), false);
  assert.equal(await page.evaluate(() => document.activeElement?.dataset.node), 'node-1');
  await badgeRoundTrip('Escape'); await badgeRoundTrip('close');

  // The phone drawer offers all navigation, API docs included, whatever the saved sidebar preference.
  for (const preference of [null, 'false', 'true']) {
    await page.evaluate(value => value === null ? localStorage.removeItem('distvis-sidebar-collapsed') : localStorage.setItem('distvis-sidebar-collapsed', value), preference);
    await page.reload();
    await page.waitForFunction(() => document.querySelectorAll('#graph .graph-node').length === 5);
    await page.locator('#toggle-sidebar').click();
    for (const target of ['#nav-library', '#create-experiment-side', '.sidebar-section', '#nav-guide']) {
      assert.ok(await page.locator(target).isVisible(), `${target} in the phone drawer (preference ${preference})`);
      assert.ok(await page.locator(target).isEnabled());
    }
    assert.notEqual(await page.locator('#recent-experiments').evaluate(el => getComputedStyle(el).display), 'none');
    await page.keyboard.press('Escape');
    assert.equal((await rect('.sidebar')).width, 0);
    assert.equal(await page.evaluate(() => localStorage.getItem('distvis-sidebar-collapsed')), preference, 'the drawer never writes the preference');
  }
  await stop(five);

  // Twelve nodes keep readable cards; below the minimum size the graph scrolls inside its area.
  await page.setViewportSize({ width: 1440, height: 900 });
  const twelve = await open(12);
  const cards = await page.locator('#graph .node-box').evaluateAll(boxes => boxes.map(b => [Number(b.getAttribute('width')), Number(b.getAttribute('height'))]));
  assert.ok(cards.length === 12 && cards.every(([w, h]) => w >= 76 && h >= 52));
  assert.ok(await noPageOverflow());
  await page.setViewportSize({ width: 390, height: 844 });
  await page.waitForTimeout(300);
  assert.ok(await page.locator('.graph-area').evaluate(el => el.scrollWidth > el.clientWidth), 'the graph area scrolls internally');
  assert.ok(await noPageOverflow(), 'but the page does not');
  await stop(twelve);

  await page.setViewportSize({ width: 1440, height: 900 });
  const two = await open(2);
  assert.ok(await noPageOverflow());
  await stop(two);
  assert.deepEqual(errors, []);
  console.log('PASS: layout budgets, drawer, anchored popover (focus, redraw, Escape, links, badge), mobile sheet and 2/5/12-node geometry. Screenshots: artifacts/layout-desktop.png, artifacts/layout-mobile.png');
} finally {
  for (const id of started) await fetch(`${base}/api/runs/${id}/stop`, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{}' }).catch(() => {});
  await browser.close();
}
