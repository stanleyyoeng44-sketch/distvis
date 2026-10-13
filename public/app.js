const $ = s => document.querySelector(s);
const $$ = s => [...document.querySelectorAll(s)];
const icons = {
  network: '<circle cx="12" cy="5" r="2.5"/><circle cx="5" cy="18" r="2.5"/><circle cx="19" cy="18" r="2.5"/><path d="m11 7-5 8m7-8 5 8M8 18h8"/>',
  history: '<path d="M3 11a9 9 0 1 1 2.5 7M3 4v7h7m2-4v6l4 2"/>',
  code: '<path d="m7 6-6 6 6 6m10-12 6 6-6 6M14 3l-4 18"/>',
  book: '<path d="M12 5C8 2 4 3 2 4v15c4-2 7-1 10 1 3-2 6-3 10-1V4c-2-1-6-2-10 1zm0 0v15"/>',
  spark: '<path d="m12 2 2.5 7.5L22 12l-7.5 2.5L12 22l-2.5-7.5L2 12l7.5-2.5z"/>',
  plus: '<path d="M12 5v14M5 12h14"/>',
  flask: '<path d="M9 3h6m-5 0v7L4 20c0 1 1 1 2 1h12c1 0 2 0 2-1l-6-10V3M7 15h10"/>',
  download: '<path d="M12 3v12m-5-5 5 5 5-5M4 16v5h16v-5"/>',
  stop: '<rect x="6" y="6" width="12" height="12" rx="2"/>',
  server: '<rect x="3" y="3" width="18" height="7" rx="2"/><rect x="3" y="14" width="18" height="7" rx="2"/><path d="M7 6.5h.1M7 17.5h.1M12 7h5m-5 10h5"/>',
  message: '<path d="M21 4H3v13h5l4 4 4-4h5zM7 8h10M7 12h7"/>',
  clock: '<circle cx="12" cy="12" r="9"/><path d="M12 6v6l4 2"/>',
  bolt: '<path d="m13 2-9 12h7l-1 8 10-13h-7z"/>',
  expand: '<path d="M9 3H3v6m12-6h6v6M3 15v6h6m12-6v6h-6"/>',
  pointer: '<path d="m4 3 6 17 3-7 7-3z"/>',
  rewind: '<path d="M5 5v14m14-14L8 12l11 7z"/>',
  play: '<path d="m8 5 11 7-11 7z" fill="currentColor" stroke="none"/>',
  pause: '<path d="M8 5v14M16 5v14" stroke-width="4"/>',
  step: '<path d="M19 5v14M5 5l11 7-11 7z"/>',
  info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v6M12 7v1"/>',
  terminal: '<rect x="2" y="4" width="20" height="16" rx="2"/><path d="m6 9 3 3-3 3m7 0h5"/>',
};
function icon(name) { return `<svg viewBox="0 0 24 24" aria-hidden="true">${icons[name] || icons.info}</svg>`; }
function hydrateIcons() { $$('[data-icon]').forEach(el => { el.innerHTML = icon(el.dataset.icon); }); }
hydrateIcons();
const esc = value => String(value ?? '').replace(/[&<>"']/g, c => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
const labels = { send: 'Message sent', receive: 'Message received', deliver: 'Delivered to SDK', state: 'State change', fault: 'Fault injected', drop: 'Message dropped', lifecycle: 'Run lifecycle', runtime: 'Runtime', node: 'Node status', command: 'Run operation', input_schema: 'Available operations', command_error: 'Operation failed', command_result: 'Operation result', stderr: 'Program log', stdout: 'Program output' };
const statuses = { created: 'Created', starting: 'Starting', running: 'Running', completed: 'Completed', failed: 'Failed to start', interrupted: 'Interrupted' };
const roleLabels = { leader: 'LEADER', follower: 'FOLLOWER', candidate: 'CANDIDATE', critical: 'CRITICAL', waiting: 'WAITING', replica: 'REPLICA', ready: 'READY' };
let protocolProject=null, protocolProjects=[], runFormMode='run';
let experiment=null, experiments=[], directoryProjects=[], workspaceView='library', codeDirty=false, codeEdits=0, loadingExperiment=0;
let protocols = {}, runs = [], run = null, events = [], stream, selected = 'node-1', cursor = 0, playTime = 0, live = true, playing = false, speed = 1;
let cache, cacheCursor = -1, renderDirty = true, latestTime = 0, lastFrame = 0, lastPaint = 0, toastTimer, codeTarget = 'shared', codeDrafts = {}, loadingRun = 0;
// The popover edits one element at a time: a node, or an unordered link {a, b} with a direction.
let linkSel = null, popoverOpen = false, popoverOrigin = null, popoverError = null;
// Controls act on the running experiment, so they read this projection of every received event,
// never the replay snapshot, which stops at the playback cursor.
let liveState, schemaRev = 0, anchorDirty = true, refreshSeq = 0, refreshApplied = 0, drawerOpen = false;
let graphBox = { width: 0, height: 0 };
const pending = new Set(), linkDrafts = new Map(), inputActions = new Map();
// Busy state belongs to the run that sent the request: a slow reply from an earlier run never
// disables the controls of the run on screen.
const faultsInFlight = new Map(), stopsInFlight = new Set();
function faultBusy(runId = run?.id) { return (faultsInFlight.get(runId) || 0) > 0; }
function stopPending(runId = run?.id) { return stopsInFlight.has(runId); }
// A run only moves forward through these; an older snapshot never moves it back.
const statusRank = { created: 0, starting: 1, running: 2, completed: 3, failed: 3, interrupted: 3 };
let graphView = 'topology', spaceWindow = 1000, graphStamp = '';
let spaceStart = 0, spaceFollow = true, spaceNode = 'all', hideHeartbeats = false;
let spaceDrag = null, suppressSpaceClick = false;
const ends = new Map();
const sends = new Map();
const fields = { role: 'Role', term: 'Term', votedFor: 'Voted for', leader: 'Leader', candidate: 'Candidate', granted: 'Granted', hasToken: 'Holds token', entries: 'Critical-section entries', round: 'Round', version: 'Logical version', store: 'Replica data', key: 'Key', value: 'Value', writer: 'Writer node', text: 'Text', received: 'Received count', lastFrom: 'Last sender' };
function valueText(value) {
  if (value === null) return 'null';
  if (value === undefined) return '—';
  return typeof value === 'object' ? JSON.stringify(value) : String(value);
}
function short(value, max = 72) {
  const text = valueText(value);
  return text.length > max ? `${text.slice(0, max)}…` : text;
}
function fieldRows(value, { omit = [], limit = 12 } = {}) {
  const entries = value && typeof value === 'object' && !Array.isArray(value)
    ? Object.entries(value).filter(([key]) => !omit.includes(key))
    : [['value', value]];
  const rows = entries.slice(0, limit).map(([key, val]) => `<div class="detail-row payload-field" data-field="${esc(key)}"><span>${esc(fields[key] || key)}<small>${esc(key)}</small></span><strong title="${esc(valueText(val))}">${esc(short(val, 240))}</strong></div>`).join('');
  return rows + (entries.length > limit ? '<p class="form-help">See the full JSON for the remaining fields.</p>' : '');
}
function messageLabel(e) {
  const payload = e.payload;
  if (payload?.rpcId) return short(`${payload.type === 'RPCRequest' ? 'Call' : payload.type === 'RPCResponse' ? 'Return' : 'Cancel'} ${payload.method} · ${payload.error || JSON.stringify(payload.body ?? {})}`, 100);
  if (!payload || typeof payload !== 'object' || Array.isArray(payload)) return short(payload, 55);
  const pairs = Object.entries(payload).filter(([key]) => key !== 'type');
  return short([payload.type || 'Message', ...pairs.slice(0, 2).map(([k, v]) => `${k}=${short(v, 28)}`)].join(' · '), 85);
}
function observedEnd(e) {
  const end = ends.get(e.id);
  return end && end.seq <= cursor ? end : null;
}
function notify(message) {
  $('#toast').textContent = message; $('#toast').classList.add('show');
  clearTimeout(toastTimer); toastTimer = setTimeout(() => $('#toast').classList.remove('show'), 4500);
}
async function api(path, data) {
  const response = await fetch(path, data === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(data) });
  const result = await response.json();
  if (!response.ok) throw new Error(result.error || 'Request failed');
  return result;
}
function guard(fn) {
  return (...args) => {
    // Start synchronously so form handlers can cancel the browser's default submit.
    try { return Promise.resolve(fn(...args)).catch(e => notify(e.message)); }
    catch (e) { notify(e.message); }
  };
}
function time(ms, precise = false) {
  const whole = Math.floor(ms / 1000), minutes = Math.floor(whole / 60), seconds = whole % 60;
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}${precise ? `.${String(Math.floor(ms % 1000)).padStart(3, '0')}` : ''}`;
}
function nodes() { return Array.from({ length: run?.config.nodeCount || 0 }, (_, i) => `node-${i + 1}`); }
function emptyState() { return { states: {}, online: Object.fromEntries(nodes().map(n => [n, true])), links: {}, sends: 0, receives: 0, faults: [], sentBy: {}, receivedBy: {}, schemas: {}, results: {} }; }
// One reducer serves both the replay snapshot and the live projection, so they cannot disagree.
function reduceEvent(acc, e) {
  if (e.type === 'state') acc.states[e.node] = e.state;
  else if (e.type === 'node') acc.online[e.node] = e.online;
  else if (e.type === 'send') { acc.sends++; acc.sentBy[e.from] = (acc.sentBy[e.from] || 0) + 1; }
  else if (e.type === 'receive') { acc.receives++; acc.receivedBy[e.to] = (acc.receivedBy[e.to] || 0) + 1; }
  else if (e.type === 'input_schema') acc.schemas[e.node] = e.schema;
  else if (e.type === 'command_result' || e.type === 'command_error') acc.results[e.node] = e;
  else if (e.type === 'fault') {
    const f = e.fault; acc.faults.push(e);
    if (f.kind === 'heal') acc.links = {};
    if (f.kind === 'link') {
      const rule = { latency: f.latency, bandwidth: f.bandwidth, blocked: f.blocked, ...(f.delayModel ? { delayModel: f.delayModel, jitter: f.delayModel === 'fixed' ? 0 : f.jitter } : {}) };
      acc.links[`${f.from}>${f.to}`] = rule;
      if (f.bidirectional) acc.links[`${f.to}>${f.from}`] = rule;
    }
  }
  return acc;
}
function resetCache() { cache = emptyState(); cacheCursor = 0; }
function snapshot() {
  if (!cache || cursor < cacheCursor) resetCache();
  for (let i = cacheCursor; i < cursor; i++) reduceEvent(cache, events[i]);
  cacheCursor = cursor;
  return cache;
}
function hydrateLive() { liveState = emptyState(); for (const e of events) reduceEvent(liveState, e); schemaRev++; }
// The furthest status seen for each run, from any source (events, loads, stop replies, polls).
// It outlives navigation, so a poll that started before a run ended cannot revive it later.
const knownStatus = new Map();
function noteStatus(runId, status) {
  if (!runId || !(status in statusRank)) return knownStatus.get(runId);
  const known = knownStatus.get(runId);
  // Only a strictly later stage replaces the known one, so the first terminal status wins.
  if (!known || statusRank[status] > statusRank[known]) knownStatus.set(runId, status);
  return knownStatus.get(runId);
}
// Every status source goes through here: the store, the inspected run and the active-run cache agree.
function setRunStatus(runId, status) {
  const best = noteStatus(runId, status);
  if (run?.id === runId) run.status = best;
  for (const item of experiments) if (item.latestRun?.id === runId) item.latestRun.status = best;
  return best;
}
function receiveEvent(e) {
  if (events.length && e.seq <= events.at(-1).seq) return;
  events.push(e); latestTime = Math.max(latestTime, e.time);
  if (e.type === 'receive' || e.type === 'drop') ends.set(e.id, e);
  if (e.type === 'send') sends.set(e.id, e);
  if (liveState) reduceEvent(liveState, e);
  if (e.type === 'input_schema') schemaRev++;
  if (e.type === 'lifecycle' && run) {
    const next = { start: 'running', stop: 'completed', failed: 'failed' }[e.action];
    // Statuses only move forward, so a late start never revives a run that has already ended.
    if (next) { setRunStatus(run.id, run.status); setRunStatus(run.id, next); }
  }
  if (live) { cursor = events.length; playTime = latestTime; }
  renderDirty = true;
}
async function openRun(id, asLive = false) {
  const requestId = ++loadingRun;
  const result = await api(`/api/runs/${id}`);
  if (requestId !== loadingRun || result.config.experimentId !== experiment?.id) return;
  stream?.close();
  run = result; events = result.events; delete run.events;
  setRunStatus(run.id, run.status);
  ends.clear();
  sends.clear();
  for (const e of events) {
    if (e.type === 'receive' || e.type === 'drop') ends.set(e.id, e);
    if (e.type === 'send') sends.set(e.id, e);
  }
  selected = 'node-1'; live = asLive; playing = false; latestTime = Math.max(result.time, events.at(-1)?.time || 0);
  cursor = asLive ? events.length : 0; playTime = asLive ? latestTime : 0;
  spaceStart = 0; spaceFollow = true; spaceNode = 'all';
  resetPopover();
  resetCache(); hydrateLive(); populateNodes(); renderDirty = true; graphStamp = '';
  localStorage.setItem(`distvis-run:${experiment.id}`, id);
  stream = new EventSource(`/api/runs/${id}/events?after=${events.at(-1)?.seq || 0}`);
  stream.onmessage = message => { if(run?.id===id)receiveEvent(JSON.parse(message.data)); };
  stream.onerror = () => { $('#connection-status').textContent = 'Reconnecting'; };
  stream.onopen = () => { $('#connection-status').textContent = 'Connected'; };
  render();
}
function populateNodes() {
  $('#spacetime-node').innerHTML = '<option value="all">All nodes</option>' + nodes().map(n => `<option>${n}</option>`).join('');
}
function jitterText(model, jitter) { return model === 'exponential' ? ` + Exp(${jitter})` : ''; }
// Experiment-level packet policy (server/network.js). Percentages in the form, rates in the API.
function networkText(n) {
  if (!n) return '';
  const pct = r => `${+(r * 100).toFixed(1)}%`, parts = [];
  if (n.lossRate > 0) parts.push(`Loss ${pct(n.lossRate)}`);
  if (n.reorderRate > 0) parts.push(`${n.ordering === 'unordered' ? 'reordered' : 'delayed'} ${pct(n.reorderRate)}`);
  else if (n.ordering === 'unordered') parts.push('Reordering allowed');
  return parts.length ? ` · ${parts.join(' · ')}` : '';
}
function fillNetworkForm(form, n = {}) {
  form.elements.lossPercent.value = +((n.lossRate || 0) * 100).toFixed(1);
  form.elements.ordering.value = n.ordering || 'fifo';
  form.elements.reorderPercent.value = +((n.reorderRate || 0) * 100).toFixed(1);
  form.elements.reorderMin.value = n.reorderMin ?? 200;
  form.elements.reorderMax.value = n.reorderMax ?? 2200;
  $('#network-settings').open = !!(n.lossRate || n.reorderRate || n.ordering === 'unordered');
}
function networkFromForm(form) {
  const e = form.elements, rate = v => Number((Number(v) / 100).toFixed(4));
  return { lossRate: rate(e.lossPercent.value), ordering: e.ordering.value, reorderRate: rate(e.reorderPercent.value), reorderMin: Number(e.reorderMin.value), reorderMax: Number(e.reorderMax.value) };
}
function ruleText(rule) { return !rule ? 'Default' : rule.blocked ? 'Blocked' : `${rule.latency}${jitterText(rule.delayModel, rule.jitter)} ms · ${rule.bandwidth} KiB/s`; }
function faultText(f) {
  return { network: `Network policy${networkText(f.network) || ' · no loss/reordering'}`, crash: `${f.node} crashed`, recover: `${f.node} recovered`, heal: 'Restored all links', link: `${f.from} ${f.bidirectional ? '↔' : '→'} ${f.to} · ${f.blocked ? 'blocked' : `${f.latency}${jitterText(f.delayModel, f.jitter)}ms / ${f.bandwidth} KiB/s`}` }[f.kind];
}
function eventContent(e) {
  if (e.type === 'command_result') return `${e.commandId} ${e.error || JSON.stringify(e.result)}`;
  if (e.payload?.rpcId) return `${e.payload.type} ${e.payload.method} ${e.payload.error || JSON.stringify(e.payload.body ?? {})}`;
  if (e.type === 'command') return `${e.action || 'write'} ${JSON.stringify(e.values || {key:e.key,value:e.value})}`;
  if (e.payload) return `${e.payload.type || 'Message'}  ${JSON.stringify(e.payload)}`;
  if (e.state) return JSON.stringify(e.state);
  if (e.fault) return faultText(e.fault);
  return e.message || e.reason || e.action || (e.type === 'command' ? `${e.key} = ${e.value}` : JSON.stringify(e));
}
// Node cards keep a readable minimum size; below it the graph area scrolls instead of shrinking them.
function graphGeometry(count) {
  const min = count > 8 ? { width: 640, height: 440 } : count > 6 ? { width: 520, height: 380 } : { width: 380, height: 320 };
  const width = Math.max(min.width, Math.floor(graphBox.width || 800)), height = Math.max(min.height, Math.floor(graphBox.height || 430));
  const w = count > 8 ? 76 : 96, h = count > 8 ? 52 : 62, top = 44;
  return { width, height, w, h, cx: width / 2, cy: (height + top) / 2, rx: width / 2 - w / 2 - 28, ry: (height - top) / 2 - h / 2 - 28 };
}
function drawGraph(state) {
  const list = nodes(), count = list.length;
  $('#empty-graph').hidden = Boolean(run);
  if (!run) { $('#graph').innerHTML = ''; return; }
  const geo = graphGeometry(count), svg = $('#graph');
  svg.setAttribute('viewBox', `0 0 ${geo.width} ${geo.height}`);
  svg.style.width = `${geo.width}px`; svg.style.height = `${geo.height}px`;
  const positions = Object.fromEntries(list.map((n, i) => {
    const angle = -Math.PI / 2 + i * 2 * Math.PI / count;
    return [n, { x: geo.cx + Math.cos(angle) * geo.rx, y: geo.cy + Math.sin(angle) * geo.ry }];
  }));
  const parts = ['<defs><filter id="shadow" x="-30%" y="-30%" width="160%" height="160%"><feDropShadow dx="0" dy="4" stdDeviation="5" flood-color="#355e53" flood-opacity=".07"/></filter></defs>'];
  for (let i = 0; i < count; i++) for (let j = i + 1; j < count; j++) {
    const from = list[i], to = list[j], a = positions[from], b = positions[to];
    const forward = state.links[`${from}>${to}`], reverse = state.links[`${to}>${from}`];
    const blocked = forward?.blocked || reverse?.blocked, chosen = linkSel?.a === from && linkSel?.b === to;
    const summary = `${from} → ${to}: ${ruleText(forward)}; ${to} → ${from}: ${ruleText(reverse)}`;
    parts.push(`<path class="link-line ${blocked ? 'blocked' : ''} ${chosen ? 'selected' : ''}" d="M${a.x} ${a.y}L${b.x} ${b.y}"/><path class="link-hit" data-from="${from}" data-to="${to}" role="button" tabindex="-1" aria-label="${esc(`Link between ${from} and ${to}. ${summary}`)}" d="M${a.x} ${a.y}L${b.x} ${b.y}"><title>${esc(summary)}\nClick to configure this link</title></path>`);
  }
  parts.push(`<text class="center-label" x="${geo.cx}" y="${geo.cy - 3}" text-anchor="middle">DISTRIBUTED</text><text class="center-protocol" x="${geo.cx}" y="${geo.cy + 17}" text-anchor="middle">${esc({ raft: 'RAFT', token: 'TOKEN RING', gossip: 'LWW STORE', custom: 'YOUR PROTOCOL' }[experiment?.protocol || run.config.protocol] || 'RPC PROTOCOL')}</text>`);
  // The packet's position comes from recorded send/receive times and the playback clock.
  const messages = [];
  for (let i = cursor - 1; i >= 0; i--) {
    const e = events[i];
    if (playTime - e.time > 35000) break;
    if (e.type !== 'send' || !positions[e.from] || !positions[e.to]) continue;
    const end = observedEnd(e);
    if (end?.type === 'drop') continue;
    const endTime = end ? end.time : e.time + (e.delay || 0);
    const visualEnd = Math.max(endTime, e.time + 550);
    if (playTime > visualEnd + 160) continue;
    messages.push({ e, visualEnd });
    if (messages.length >= 40) break;
  }
  for (const { e, visualEnd } of messages.reverse()) {
    const a = positions[e.from], b = positions[e.to], fraction = Math.min(1, Math.max(0, (playTime - e.time) / Math.max(1, visualEnd - e.time)));
    const distance = Math.hypot(b.x - a.x, b.y - a.y), ux = distance ? (b.x - a.x) / distance : 1, uy = distance ? (b.y - a.y) / distance : 0;
    // Keep arrows outside node cards; offset opposite directions onto separate lanes.
    const inset = count > 8 ? 49 : 61;
    let x = a.x + ux * (inset + Math.max(0, distance - inset * 2) * fraction) - uy * 9;
    let y = a.y + uy * (inset + Math.max(0, distance - inset * 2) * fraction) + ux * 9;
    let angle = Math.atan2(uy, ux) * 180 / Math.PI;
    if (e.from === e.to) {
      const theta = fraction * 2 * Math.PI;
      x = a.x + 72 + Math.cos(theta) * 18; y = a.y + Math.sin(theta) * 27;
      angle = Math.atan2(Math.cos(theta) * 27, -Math.sin(theta) * 18) * 180 / Math.PI;
    }
    const color = e.payload?.type === 'Token' || e.payload?.method === 'Ring.Pass' ? '#d9a04b' : '#9274cd';
    parts.push(`<g data-event="${e.seq}" class="packet" tabindex="0" role="button" aria-label="${esc(`${e.from} → ${e.to} · ${messageLabel(e)}`)}"><title>${esc(`${e.from} → ${e.to}\n${eventContent(e)}`)}</title><g transform="translate(${x},${y}) rotate(${angle})"><rect x="-19" y="-15" width="38" height="30" rx="6" fill="transparent"/><path class="packet-arrow" d="M-13 -4H1V-9L14 0 1 9V4H-13Z" fill="${color}" stroke="white" stroke-width="1.5"/></g>${count <= 6 ? `<text class="message-label" x="${x}" y="${y - 18}" text-anchor="middle">${esc(messageLabel(e))}</text>` : ''}</g>`);
  }
  for (const [i, node] of list.entries()) {
    const p = positions[node], st = state.states[node] || {}, online = state.online[node], active = ['leader', 'critical'].includes(st.role), chosen = selected === node && !linkSel;
    const color = !online ? '#d59195' : active ? '#289780' : st.role === 'candidate' ? '#a18bd2' : '#94a8bc';
    const fill = !online ? '#fdf3f3' : active ? '#f0faf5' : '#ffffff';
    const { w, h } = geo, actions = (liveState?.schemas[node] || []).length;
    // Input badges follow the live schema: inputs always go to the running node.
    const badge = actions ? `<g class="input-badge" data-input-node="${node}" role="button" tabindex="-1" aria-label="${esc(`Run an operation on ${node}`)}"><title>Run on ${node} · ${actions} operations</title><rect x="${-w / 2 - 10}" y="${-h / 2 - 10}" width="${actions > 1 ? 50 : 38}" height="20" rx="10"/><text x="${-w / 2 + (actions > 1 ? 15 : 9)}" y="${-h / 2 + 4}" text-anchor="middle">Run${actions > 1 ? ` ${actions}` : ''}</text></g>` : '';
    parts.push(`<g class="graph-node ${!online ? 'node-offline' : ''}" data-node="${node}" tabindex="0" role="button" aria-label="${node}, ${online ? st.role || 'starting' : 'offline'}${actions ? '; operations can run on this node' : ''}" transform="translate(${p.x},${p.y})"><title>Click to open the action panel for ${node}</title>${chosen ? `<rect x="${-w / 2 - 5}" y="${-h / 2 - 5}" width="${w + 10}" height="${h + 10}" rx="16" fill="none" stroke="${color}" opacity=".35" stroke-dasharray="3 3"/>` : ''}<rect class="node-box" x="${-w / 2}" y="${-h / 2}" width="${w}" height="${h}" rx="11" fill="${fill}" stroke="${chosen || active ? color : '#dfe8ec'}" stroke-width="${active ? 1.5 : 1}" filter="url(#shadow)"/><circle cx="${-w / 2 + 13}" cy="${-h / 2 + 13}" r="3" fill="${color}"/><path d="M-7 -13h14v6H-7zm0 9h14v6H-7z" fill="none" stroke="${color}" stroke-width="1.2" transform="translate(0,-3)"/><text class="node-name" x="0" y="15" text-anchor="middle" style="font-size:${count > 8 ? 11 : 13}px">${node}</text><text class="node-role" x="0" y="${h / 2 + 18}" text-anchor="middle">${online ? esc(roleLabels[st.role] || st.role || 'STARTING') : 'OFFLINE'}${run.config.protocol === 'raft' ? ` · T${st.term || 0}` : ''}</text>${active && online ? `<rect x="${w / 2 - 8}" y="${-h / 2 - 7}" width="16" height="16" rx="5" fill="${color}"/><path d="m${w / 2 - 4} ${-h / 2 + 1} 3 3 5-6" stroke="#fff" fill="none" stroke-width="1.5"/>` : ''}${badge}</g>`);
  }
  replaceGraph(svg, parts.join(''));
}
function elementSelector(el) {
  if (el?.dataset.inputNode) return `[data-input-node="${el.dataset.inputNode}"]`;
  if (el?.dataset.node) return `[data-node="${el.dataset.node}"]`;
  if (el?.dataset.event) return `[data-event="${el.dataset.event}"]`;
  if (el?.dataset.from) return `[data-from="${el.dataset.from}"][data-to="${el.dataset.to}"]`;
  return null;
}
function replaceGraph(element, html) {
  const focused = element.contains(document.activeElement) ? document.activeElement : null;
  const selector = elementSelector(focused);
  element.innerHTML = html;
  if (selector) element.querySelector(selector)?.focus();
}

// Recorded time on X, one horizontal lane per node. Only the visible event prefix is used:
// a future receive must never turn an in-flight arrow into an already delivered one.
function spaceMaxStart() { return Math.max(0, playTime - spaceWindow * 0.85); }
function spaceMetrics() {
  const width = Math.max(320, $('.spacetime-scroll').clientWidth || 800);
  return { width, left: 112, right: width - 24, plotWidth: width - 136 };
}
function panSpace(start) {
  spaceFollow = false;
  spaceStart = Math.max(0, Math.min(spaceMaxStart(), start));
  graphStamp = ''; render();
}
function followSpace() {
  spaceFollow = true;
  graphStamp = ''; render();
}
function zoomSpace(windowMs) {
  // Zoom around the middle of the inspected interval; following keeps the playhead in view.
  const center = spaceStart + spaceWindow / 2;
  spaceWindow = windowMs;
  spaceStart = Math.max(0, Math.min(spaceMaxStart(), center - spaceWindow / 2));
  $('#spacetime-window').value = String(spaceWindow);
  graphStamp = ''; render();
}
function drawSpaceTime(state) {
  const svg = $('#spacetime'), list = nodes();
  if (!run) { svg.innerHTML = ''; $('#spacetime-count').textContent = ''; return; }
  if (spaceFollow) spaceStart = spaceMaxStart();
  else spaceStart = Math.min(spaceStart, spaceMaxStart());
  const { width, left, right, plotWidth } = spaceMetrics();
  const start = spaceStart, finish = start + spaceWindow, height = list.length * 100 + 100;
  const ys = Object.fromEntries(list.map((n, i) => [n, 94 + i * 100]));
  const x = t => left + (t - start) / spaceWindow * plotWidth;
  const parts = [`<defs><marker id="time-arrow" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0 10 5 0 10Z" fill="#8871bb"/></marker><marker id="time-drop" viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse"><path d="M0 0 10 5 0 10Z" fill="#cb7b7c"/></marker><clipPath id="time-clip"><rect x="${left}" y="52" width="${plotWidth}" height="${height - 85}"/></clipPath></defs>`];
  parts.push(`<text x="${left}" y="19" class="time-axis-title">Time →</text>`);
  const steps = Math.max(1, Math.floor(plotWidth / 112));
  for (let i = 0; i <= steps; i++) {
    const t = start + spaceWindow * i / steps, xx = x(t);
    parts.push(`<line x1="${xx}" y1="51" x2="${xx}" y2="${height - 33}" class="time-grid"/><text x="${xx}" y="39" text-anchor="${i === 0 ? 'start' : i === steps ? 'end' : 'middle'}" class="time-tick">${time(t, true)}</text>`);
  }
  for (const [i, node] of list.entries()) {
    const yy = ys[node];
    parts.push(`<rect x="${left}" y="${yy - 37}" width="${plotWidth}" height="74" class="time-lane ${i % 2 ? 'alternate' : ''}"/><line data-lane="${node}" x1="${left}" y1="${yy}" x2="${right}" y2="${yy}" class="time-lifeline"/>`);
  }
  parts.push('<g clip-path="url(#time-clip)">');
  const messages = [];
  let total = 0, filtered = 0;
  // A long-delayed packet may have been sent before this window.
  for (let i = cursor - 1; i >= 0; i--) {
    const e = events[i];
    if (e.type !== 'send' || ys[e.from] === undefined || ys[e.to] === undefined || e.time > finish) continue;
    const end = observedEnd(e);
    const endAt = end ? end.time : playTime;
    if (endAt < start) continue;
    if ((spaceNode !== 'all' && e.from !== spaceNode && e.to !== spaceNode)
        || (hideHeartbeats && (['Heartbeat', 'HeartbeatAck'].includes(e.payload?.type) || /[./]Heartbeat(?:Ack)?$/.test(e.payload?.method || '')))) { filtered++; continue; }
    total++;
    if (messages.length < 250) messages.push({ e, end, endAt });
  }
  // Label bounding boxes must not intersect. Hidden labels reappear on hover/focus,
  // while every arrow remains selectable and its complete payload stays in the dialog.
  const labelBoxes = [];
  for (const { e, end, endAt } of messages) {
    const x1 = x(e.time), x2 = x(endAt), y1 = ys[e.from], y2 = ys[e.to];
    const dropped = end?.type === 'drop', kind = dropped ? 'dropped' : end ? 'received' : 'pending';
    const status = dropped ? `Dropped: ${end.reason}` : end ? 'Received' : 'Not yet received at the current position';
    const path = e.from === e.to ? `M${x1} ${y1}C${x1 - 16} ${y1 - 52},${x2 + 16} ${y2 - 52},${x2} ${y2}` : `M${x1} ${y1}L${x2} ${y2}`;
    const text = short(messageLabel(e), Math.min(36, Math.floor((plotWidth - 16) / 7)));
    const labelWidth = Math.min(plotWidth - 8, [...text].reduce((w, c) => w + (c.charCodeAt(0) > 255 ? 11 : 6.5), 12));
    const labelX = Math.max(left + labelWidth / 2 + 3, Math.min(right - labelWidth / 2 - 3, (x1 + x2) / 2));
    const labelY = e.from === e.to ? y1 - 40 : (y1 + y2) / 2 - 8;
    const box = { l: labelX - labelWidth / 2, r: labelX + labelWidth / 2, t: labelY - 13, b: labelY + 4 };
    const show = !labelBoxes.some(b => box.l < b.r + 6 && box.r > b.l - 6 && box.t < b.b + 5 && box.b > b.t - 5);
    if (show) labelBoxes.push(box);
    parts.push(`<g class="time-message ${kind}${show ? '' : ' quiet-label'}" data-event="${e.seq}" data-message-id="${esc(e.id)}" data-status="${kind}" tabindex="0" role="button" aria-label="${esc(`${e.from} → ${e.to} · ${messageLabel(e)} · ${status}`)}"><title>${esc(`#${e.seq} ${e.from} → ${e.to}\n${eventContent(e)}\n${status}`)}</title><path d="${path}" class="time-message-hit"/><path d="${path}" class="time-message-line" marker-end="url(#${dropped ? 'time-drop' : 'time-arrow'})"/><text class="time-message-label" x="${labelX}" y="${labelY}" text-anchor="middle">${esc(text)}</text>${dropped ? `<path class="time-cross" d="M${x2 - 5} ${y2 - 5}l10 10m0-10-10 10"/>` : ''}</g>`);
  }
  // Repeated reports of the same state (e.g. heartbeat acknowledgements) are not changes.
  const previousStates = new Map(), changes = [];
  for (let i = 0; i < cursor; i++) {
    const e = events[i];
    if (!['state', 'node', 'fault', 'command'].includes(e.type)) continue;
    if (e.type === 'state') {
      const value = JSON.stringify(e.state);
      if (previousStates.get(e.node) === value) continue;
      previousStates.set(e.node, value);
    }
    if (e.time < start || e.time > finish) continue;
    const n = e.node || e.fault?.node || e.fault?.from;
    if (spaceNode !== 'all' && n && n !== spaceNode && e.fault?.to !== spaceNode) continue;
    changes.push({ e, n });
  }
  for (const { e, n } of changes.slice(-150)) {
    const xx = x(e.time), yy = (ys[n] ?? 57) - (e.type === 'command' ? 16 : 0);
    parts.push(`<g class="time-state ${e.type === 'command' ? 'application-input' : ''} ${e.type === 'fault' || e.type === 'node' ? 'fault' : ''}" data-event="${e.seq}" tabindex="0" role="button" aria-label="${esc(`${n || 'global'} ${eventContent(e)}`)}"><title>${esc(`#${e.seq} ${n || 'global'} ${eventContent(e)}`)}</title><rect x="${xx - 8}" y="${yy - 8}" width="16" height="16" fill="transparent"/><path d="M${xx} ${yy - 4}l4 4-4 4-4-4Z"/></g>`);
  }
  const currentX = x(playTime);
  parts.push(`<line x1="${currentX}" y1="52" x2="${currentX}" y2="${height - 33}" class="time-playhead"/>`);
  parts.push('</g>');
  for (const node of list) {
    const st = state.states[node] || {}, yy = ys[node];
    parts.push(`<g data-node="${node}" tabindex="0" role="button" aria-label="View ${node} state" class="time-node ${selected === node ? 'selected' : ''}"><rect x="9" y="${yy - 25}" width="91" height="50" rx="8"/><text x="54" y="${yy - 3}" text-anchor="middle" class="node-name">${node}</text><text x="54" y="${yy + 14}" text-anchor="middle" class="node-role">${state.online[node] ? esc(short(st.role || 'no report yet', 13)) : 'OFFLINE'}</text></g>`);
  }
  parts.push(`<text x="${left}" y="${height - 12}" class="time-current">Replay ${time(playTime, true)} · #${cursor}</text>`);
  svg.setAttribute('viewBox', `0 0 ${width} ${height}`);
  svg.style.height = `${height}px`;
  svg.dataset.start = String(start); svg.dataset.end = String(finish);
  replaceGraph(svg, parts.join(''));
  $('#spacetime-follow').setAttribute('aria-pressed', String(spaceFollow));
  $('#spacetime-follow').textContent = spaceFollow ? 'Follow playback' : 'Back to play position';
  $('#spacetime-range').textContent = `${time(start, true)} — ${time(finish, true)}`;
  $('#spacetime-pan').max = String(Math.ceil(spaceMaxStart()));
  $('#spacetime-pan').value = String(start);
  $('#spacetime-pan').disabled = spaceMaxStart() === 0;
  $('#spacetime-count').textContent = `Messages ${messages.length}/${total}${filtered ? ` · filtered out ${filtered}` : ''} · state changes/faults ${Math.min(changes.length, 150)}/${changes.length}${total > 250 || changes.length > 150 ? ' (zoom in the timeline)' : ''}`;
}
function switchView(view) {
  graphView = view;
  const isSpace = view === 'spacetime';
  // SVGElement.hidden is not reflected to an HTML hidden attribute in real browsers.
  $('#graph').toggleAttribute('hidden', isSpace);
  $('#spacetime-panel').hidden = !isSpace;
  $('.topology').classList.toggle('spacetime-mode', isSpace);
  $$('.view-switch button').forEach(b => b.setAttribute('aria-pressed', String(b.dataset.view === view)));
  graphStamp = ''; renderDirty = true; render();
}
const inputDrafts = new Map();
// Nodes chosen to receive the same input at the same instant, per run and source node.
const concurrentPicks = new Map();
// A peer can join a concurrent send if it is online and declares an input with the same label.
function peerAction(peer, label) { return liveOnline(peer) ? nodeInputSchema(peer).find(s => s.label === label)?.action : undefined; }
function nodeInputSchema(node = selected) { return liveState?.schemas[node] || []; }
function inputDraftKey(node = selected) { return `${run?.id}:${node}`; }
function liveOnline(node = selected) { return liveState?.online[node] !== false; }
function pairKey(a, b) { return `${a}>${b}`; }
function chosenAction(node = selected) {
  const schema = nodeInputSchema(node), chosen = inputActions.get(inputDraftKey(node));
  return schema.some(s => s.action === chosen) ? chosen : startAction(node) || schema[0]?.action;
}
// The protocol's own `// distvis:start` comment names the node and input that begin message traffic.
function runStart() { return run?.config.project?.start; }
function startAction(node = selected) { const s = runStart(); return s?.node === node ? nodeInputSchema(node).find(a => a.label === s.method)?.action : undefined; }
function startText(s) { return `Start by calling ${s.method}${Object.keys(s.values || {}).length ? ` ${JSON.stringify(s.values)}` : ''} on ${s.node}${s.note ? `: ${s.note}` : ''}`; }
// Operation identities: an input action, a node's crash/recover toggle, or one direction of a link.
function inputOperation(node, action) { return `${run.id}:node:${node}:input:${action}`; }
function toggleOperation(node) { return `${run.id}:node:${node}:toggle`; }
function linkOperation() { return `${run.id}:link:${pairKey(linkSel.a, linkSel.b)}:${linkSel.dir}`; }
function visibleOperations() {
  if (!run || !popoverOpen) return [];
  if (linkSel) return [linkOperation()];
  const action = chosenAction();
  return [toggleOperation(selected), ...(action ? [inputOperation(selected, action)] : [])];
}
function isCurrent(token) {
  return token.loadGen === loadingRun && run?.id === token.runId && (token.scope === 'run' || visibleOperations().includes(token.key));
}
// Each request remembers the run, load generation and element it was sent for. A late reply
// after the user switched runs or elements only clears its own pending flag.
async function mutate({ key, path, body, fault = false, scope = 'target', success }) {
  if (!run) throw new Error('Run an experiment first');
  const token = { loadGen: loadingRun, runId: run.id, key, scope };
  pending.add(key); if (fault) faultsInFlight.set(token.runId, (faultsInFlight.get(token.runId) || 0) + 1);
  if (isCurrent(token)) popoverError = null;
  renderDirty = true; render();
  try {
    const result = await api(path, body);
    success?.(result, isCurrent(token));
    return result;
  } catch (error) {
    if (isCurrent(token)) {
      if (scope === 'target') popoverError = { target: key, message: error.message };
      notify(error.message);
    }
  } finally {
    pending.delete(key);
    if (fault) { const left = (faultsInFlight.get(token.runId) || 1) - 1; if (left > 0) faultsInFlight.set(token.runId, left); else faultsInFlight.delete(token.runId); }
    renderDirty = true;
  }
}
// A draft value survives a schema change only if its field keeps the same name and type.
function draftValue(field, entry) {
  const saved = entry?.type === field.type ? entry.value : undefined;
  if (saved === undefined) return field.type === 'boolean' ? false : field.type === 'nodes' ? [] : '';
  if (field.type === 'select' && saved !== '' && !field.options.includes(saved)) return '';
  if (field.type === 'node' && saved !== '' && !nodes().includes(saved)) return '';
  if (field.type === 'nodes') return Array.isArray(saved) ? saved.filter(n => nodes().includes(n)) : [];
  return saved;
}
// Destination fields (`node` / `nodes`) offer the run's other nodes instead of free text.
function destinations() { return nodes().filter(n => n !== selected); }
function pickedNodes(form, field) { return [...form.querySelectorAll('[data-node-choice]')].filter(b => b.dataset.nodeChoice === field && b.checked).map(b => b.value); }
function nodePicker(f, saved) {
  return `<details class="node-picker" data-field="${esc(f.name)}"><summary>${esc(saved.length ? saved.join(', ') : 'Choose nodes (multi-select)')}</summary><div class="node-picker-list">${destinations().map(n => `<label class="checkbox"><input type="checkbox" data-node-choice="${esc(f.name)}" value="${esc(n)}" ${saved.includes(n) ? 'checked' : ''}>${esc(n)}</label>`).join('')}</div></details>`;
}
function applicationForm(s, values) {
  const controls = s.fields.map(f => {
    const name = esc(f.name), saved = draftValue(f, values[f.name]);
    const required = f.required ? 'required' : '';
    let control;
    if (f.type === 'boolean') control = `<input name="${name}" type="checkbox" ${saved === true ? 'checked' : ''}>`;
    else if (f.type === 'select') control = `<select name="${name}" ${required}>${!f.required ? '<option value="">Use default</option>' : ''}${f.options.map(o => `<option value="${esc(o)}" ${saved === o ? 'selected' : ''}>${esc(o)}</option>`).join('')}</select>`;
    else if (f.type === 'node') control = `<select name="${name}" ${required}><option value="">${f.required ? 'Choose a node' : 'Unspecified'}</option>${destinations().map(n => `<option value="${esc(n)}" ${saved === n ? 'selected' : ''}>${esc(n)}</option>`).join('')}</select>`;
    else if (f.type === 'nodes') return `<div class="field"><span>${esc(f.label)}</span>${nodePicker(f, saved)}</div>`;
    else if (f.type === 'json') control = `<textarea name="${name}" rows="3" ${required} placeholder='{"value":42}'>${esc(saved)}</textarea>`;
    else control = `<input name="${name}" type="${f.type === 'number' ? 'number' : 'text'}" value="${esc(saved)}" ${required} ${f.type === 'number' ? `step="${f.integer ? '1' : 'any'}"` : `maxlength="${f.maxLength || 8192}"`} ${f.min !== undefined ? `min="${f.min}"` : ''} ${f.max !== undefined ? `max="${f.max}"` : ''}>`;
    return `<label class="${f.type === 'boolean' ? 'checkbox' : 'field'}">${esc(f.label)}${control}</label>`;
  }).join('');
  const peers = nodes().filter(n => n !== selected), picked = concurrentPicks.get(inputDraftKey()) || new Set();
  const concurrent = peers.length ? `<fieldset class="concurrent-targets"><legend>Run on other nodes at the same time (concurrent events)</legend><div class="concurrent-list">${peers.map(n => `<label class="checkbox"><input type="checkbox" data-concurrent="${esc(n)}" ${picked.has(n) ? 'checked' : ''}>${esc(n)}</label>`).join('')}</div><p class="form-help">Picked nodes run the same operation with the same arguments at the same instant; there is no causal ordering between them.</p></fieldset>` : '';
  return `<form class="application-form" data-action="${esc(s.action)}" data-label="${esc(s.label)}"><h4>${esc(s.label)}</h4>${s.description ? `<p class="form-help">${esc(s.description)}</p>` : ''}${controls}${concurrent}<button type="submit" class="button primary">Run on ${esc(selected)}</button></form>`;
}
function renderApplicationInput() {
  const panel = $('#application-panel'), node = selected, schema = nodeInputSchema(node), key = inputDraftKey(node);
  const chosen = chosenAction(node);
  // Forms are rebuilt only when the node or its declared schema changes, so typing survives live updates.
  const stamp = `${key}:${schemaRev}`;
  // An IME composition in progress would be destroyed by a rebuild; wait until it ends.
  if (panel.dataset.stamp !== stamp && !composingInput) {
    const active = panel.contains(document.activeElement) ? document.activeElement : null;
    const focus = active && { action: active.closest('form')?.dataset.action, name: active.name, id: active.id, start: active.selectionStart, end: active.selectionEnd };
    panel.dataset.stamp = stamp;
    const draft = inputDrafts.get(key) || {};
    const chooser = schema.length > 1 ? `<label class="field action-choice">Action<select id="input-action" aria-label="Choose the operation to run">${schema.map(s => `<option value="${esc(s.action)}">${esc(s.label)}</option>`).join('')}</select></label>` : '';
    const start = runStart(), startNote = start ? `<p class="form-help start-note">${esc(start.node === node ? `Protocol start (distvis:start comment): ${startText(start)}` : `The protocol starts on ${start.node}: ${startText(start)}`)}</p>` : '';
    panel.innerHTML = `<div class="inspector-section-title">Run on ${esc(node)} <span class="live-tag">live</span></div>` + startNote + (schema.length ? chooser + schema.map(s => applicationForm(s, draft[s.action] || {})).join('') : '<p class="form-help">This node has not declared an application input interface.</p>') + '<p id="application-note" class="form-help"></p><div id="application-result"></div>';
    if (focus?.id === 'input-action') $('#input-action')?.focus();
    else if (focus?.name) {
      const form = [...panel.querySelectorAll('form')].find(f => f.dataset.action === focus.action);
      const field = form && [...form.querySelectorAll('[name]')].find(el => el.name === focus.name);
      field?.focus();
      try { if (focus.start != null) field?.setSelectionRange(focus.start, focus.end); } catch {}
    }
  }
  if ($('#input-action') && $('#input-action').value !== chosen) $('#input-action').value = chosen;
  const reason = run.status !== 'running' ? 'The experiment is not running, so operations cannot run.' : !liveOnline(node) ? `${node} is offline right now; operations cannot run on it.` : '';
  const picked = concurrentPicks.get(key) || new Set();
  for (const form of panel.querySelectorAll('form.application-form')) {
    form.hidden = form.dataset.action !== chosen;
    let targets = 0;
    for (const box of form.querySelectorAll('[data-concurrent]')) {
      const peer = box.dataset.concurrent, ok = !!peerAction(peer, form.dataset.label);
      box.disabled = !ok; box.checked = ok && picked.has(peer);
      box.parentElement.title = ok ? '' : liveOnline(peer) ? `${peer} has not declared ${form.dataset.label}` : `${peer} is offline`;
      if (box.checked) targets++;
    }
    const button = form.querySelector('button[type="submit"]');
    button.disabled = !!reason || pending.has(inputOperation(node, form.dataset.action));
    button.textContent = targets ? `Run on ${node} and ${targets} other node${targets > 1 ? 's' : ''}` : `Run on ${node}`;
  }
  $('#application-note').textContent = reason || (live ? 'The operation runs on the live node and is recorded in the event stream and the space-time diagram.' : 'You are viewing a replay; the operation still runs on the live node of the running experiment.');
  $('#application-note').classList.toggle('warning', !!reason);
  // The latest reply comes from live events, even when the playback cursor is earlier.
  const result = liveState.results[node], resultPanel = $('#application-result');
  if (resultPanel.dataset.seq !== String(result?.seq ?? '')) {
    resultPanel.dataset.seq = String(result?.seq ?? '');
    const failure = result?.error || result?.message;
    resultPanel.innerHTML = result ? `<div class="inspector-section-title">${result.type === 'command_error' ? 'Last operation failed' : 'Last RPC reply'} <span>${esc(result.commandId || `#${result.commandSeq}`)}</span></div>${failure ? `<p class="form-help warning">${esc(failure)}</p>` : fieldRows(result.result)}` : '';
  }
}
$('#application-panel').addEventListener('input', e => {
  if (e.target.dataset.nodeChoice) {
    const form = e.target.closest('form'), field = e.target.dataset.nodeChoice, picked = pickedNodes(form, field);
    const key = inputDraftKey(), draft = inputDrafts.get(key) || {};
    (draft[form.dataset.action] ||= {})[field] = { value: picked, type: 'nodes' };
    inputDrafts.set(key, draft);
    const summary = [...form.querySelectorAll('.node-picker')].find(d => d.dataset.field === field)?.querySelector('summary');
    if (summary) summary.textContent = picked.length ? picked.join(', ') : 'Choose nodes (multi-select)';
    return;
  }
  if (e.target.dataset.concurrent) {
    const key = inputDraftKey(), picked = concurrentPicks.get(key) || new Set();
    if (e.target.checked) picked.add(e.target.dataset.concurrent); else picked.delete(e.target.dataset.concurrent);
    concurrentPicks.set(key, picked); renderDirty = true; render(); return;
  }
  const form = e.target.closest('form');
  if (!form || !e.target.name) return;
  const key = inputDraftKey(), draft = inputDrafts.get(key) || {};
  const type = nodeInputSchema().find(s => s.action === form.dataset.action)?.fields.find(f => f.name === e.target.name)?.type;
  draft[form.dataset.action] ||= {};
  draft[form.dataset.action][e.target.name] = { value: e.target.type === 'checkbox' ? e.target.checked : e.target.value, type };
  inputDrafts.set(key, draft);
});
let composingInput = false;
$('#application-panel').addEventListener('compositionstart', () => { composingInput = true; });
$('#application-panel').addEventListener('compositionend', () => { composingInput = false; renderDirty = true; });
$('#application-panel').addEventListener('change', e => {
  if (e.target.id !== 'input-action') return;
  inputActions.set(inputDraftKey(), e.target.value); renderDirty = true; render();
});
$('#application-panel').addEventListener('submit', guard(async e => {
  e.preventDefault();
  const form = e.target, action = form.dataset.action, node = selected;
  const schema = nodeInputSchema(node).find(s => s.action === action), values = {};
  if (!schema) throw new Error('The input declaration changed; fill in the form again');
  for (const f of schema.fields) {
    if (f.type === 'nodes') {
      const picked = pickedNodes(form, f.name);
      if (f.required && !picked.length) throw new Error(`Choose ${f.label}`);
      values[f.name] = picked; continue;
    }
    const el = form.elements.namedItem(f.name);
    if (f.type === 'boolean') values[f.name] = el.checked;
    else if (el.value !== '' || f.required) {
      if (f.type === 'json') {
        try { values[f.name] = JSON.parse(el.value); } catch { throw new Error(`${f.label} is not valid JSON`); }
      } else values[f.name] = f.type === 'number' ? Number(el.value) : el.value;
    }
  }
  const peers = [...form.querySelectorAll('[data-concurrent]:checked')].map(box => ({ node: box.dataset.concurrent, action: peerAction(box.dataset.concurrent, schema.label) })).filter(p => p.action);
  if (peers.length) {
    await mutate({ key: inputOperation(node, action), path: `/api/runs/${run.id}/commands`, body: { batch: [{ node, action, values }, ...peers.map(p => ({ ...p, values }))] },
      success: (result, current) => {
        if (!current) return;
        const failed = result.commands.filter(c => c.error);
        notify(failed.length ? `Some concurrent operations failed: ${failed.map(c => `${c.node} ${c.error}`).join('; ')}` : `Ran concurrently on ${result.commands.map(c => c.node).join(', ')}`);
      } });
    return;
  }
  await mutate({ key: inputOperation(node, action), path: `/api/runs/${run.id}/commands`, body: { node, action, values },
    success: (result, current) => { if (current) notify(`Ran on ${node} · #${result.commandSeq}`); } });
}));
function faultBlockReason() {
  return run.status !== 'running' ? 'The experiment is not running, so faults cannot be injected.' : faultBusy() ? 'A previous fault operation is still in progress…' : '';
}
function renderNodePopover(state) {
  const node = selected, online = liveOnline(node);
  $('#popover-title').textContent = node;
  $('#popover-status').textContent = online ? 'Online' : 'Offline';
  $('#popover-status').className = `badge ${online ? '' : 'failed'}`;
  $('#popover-status').title = 'Live status';
  renderApplicationInput();
  const toggle = $('#node-toggle'), reason = faultBlockReason(), label = online ? 'Simulate crash' : 'Recover node';
  toggle.dataset.nodeAction = online ? 'crash' : 'recover';
  if (toggle.dataset.label !== label) { toggle.dataset.label = label; toggle.innerHTML = icon('bolt') + label; }
  toggle.disabled = !!reason || pending.has(toggleOperation(node));
  $('#node-toggle-note').textContent = reason || (live ? 'Acts on the live experiment.' : 'You are viewing a replay; this action still acts on the live experiment.');
  const peers = nodes().filter(p => p !== node).map(p => [p, liveState.links[pairKey(node, p)], liveState.links[pairKey(p, node)]]);
  const list = $('#peer-links'), peerStamp = JSON.stringify([run.id, node, peers]);
  if (list.dataset.stamp !== peerStamp) {
    const focusedPeer = list.contains(document.activeElement) ? document.activeElement.dataset.peer : null;
    list.dataset.stamp = peerStamp;
    list.innerHTML = peers.map(([p, out, back]) => `<button class="peer-link ${out?.blocked || back?.blocked ? 'blocked' : ''}" data-peer="${p}" aria-label="${esc(`Configure the link between ${node} and ${p}: outgoing ${ruleText(out)}, incoming ${ruleText(back)}`)}"><strong>${p}</strong><span>→ ${ruleText(out)}</span><span>← ${ruleText(back)}</span></button>`).join('');
    if (focusedPeer) list.querySelector(`[data-peer="${focusedPeer}"]`)?.focus();
  }
  // State rows follow the playback cursor and say so, unlike the live controls above them.
  const st = state.states[node] || {}, replayOnline = state.online[node], panel = $('#node-panel');
  const stamp = JSON.stringify([run.id, node, st, replayOnline, state.sentBy[node], state.receivedBy[node], live, live ? 0 : Math.floor(playTime / 1000)]);
  if (panel.dataset.stamp !== stamp) {
    const rawOpen = panel.dataset.node === node && panel.querySelector('.state-raw')?.open, hadFocus = panel.contains(document.activeElement);
    panel.dataset.node = node; panel.dataset.stamp = stamp;
    panel.innerHTML = `<div class="inspector-section-title">${live ? 'Current state' : `Replay @ ${time(playTime)}`}<span>${!live && replayOnline === false ? 'offline at replay time · ' : ''}Sent ${state.sentBy[node] || 0} / Received ${state.receivedBy[node] || 0}</span></div><div class="node-key-state">${fieldRows(st)}</div><details class="state-raw" ${rawOpen ? 'open' : ''}><summary>Full state JSON</summary><pre class="json-state">${esc(JSON.stringify(st, null, 2))}</pre></details>`;
    if (hadFocus) panel.querySelector('.state-raw summary')?.focus();
  }
}
function linkRule(from, to) { return liveState.links[pairKey(from, to)]; }
function linkEnds() { return linkSel.dir === 'ba' ? [linkSel.b, linkSel.a] : [linkSel.a, linkSel.b]; }
// A two-way edit overwrites both directions, so a change to either one must be noticed.
function linkBase() {
  const { a, b, dir } = linkSel;
  return JSON.stringify(dir === 'both' ? [linkRule(a, b) || null, linkRule(b, a) || null] : linkRule(...linkEnds()) || null);
}
function fillLinkForm(values) { $('#link-latency').value = values.latency; $('#link-bandwidth').value = values.bandwidth; $('#link-blocked').checked = values.blocked; $('#link-model').value = values.delayModel || 'fixed'; $('#link-jitter').value = values.jitter ?? 0; $('#link-jitter').disabled = $('#link-model').value !== 'exponential'; }
function liveLinkValues() {
  const rule = linkRule(...linkEnds());
  const model = rule?.delayModel ?? run.config.delayModel ?? 'fixed';
  return { latency: String(rule?.latency ?? run.config.latency), bandwidth: String(rule?.bandwidth ?? run.config.bandwidth), blocked: !!rule?.blocked, delayModel: model, jitter: String(model === 'fixed' ? 0 : rule?.delayModel ? rule.jitter : run.config.jitter ?? 0) };
}
function renderLinkPopover() {
  const { a, b, dir } = linkSel, ab = linkRule(a, b), ba = linkRule(b, a);
  $('#popover-title').textContent = `${a} ↔ ${b}`;
  $('#popover-status').textContent = 'Link'; $('#popover-status').className = 'badge neutral'; $('#popover-status').title = '';
  const rules = JSON.stringify([a, b, ab, ba]);
  if ($('#link-rules').dataset.stamp !== rules) {
    $('#link-rules').dataset.stamp = rules;
    $('#link-rules').innerHTML = `<div class="detail-row" data-direction="ab"><span>${a} → ${b}</span><strong>${ruleText(ab)}</strong></div><div class="detail-row" data-direction="ba"><span>${b} → ${a}</span><strong>${ruleText(ba)}</strong></div>`;
  }
  $('#dir-ab').textContent = `${a} → ${b}`; $('#dir-ba').textContent = `${b} → ${a}`;
  $$('.direction-choice button').forEach(button => button.setAttribute('aria-pressed', String(button.dataset.dir === dir)));
  // A dirty draft is never overwritten by live updates; the user decides whether to take the new rule.
  const key = linkOperation(), base = linkBase(), draft = linkDrafts.get(key), form = $('#link-form');
  if (draft) { if (form.dataset.key !== key) fillLinkForm(draft); }
  else if (form.dataset.key !== key || form.dataset.base !== base) fillLinkForm(liveLinkValues());
  form.dataset.key = key; form.dataset.base = base;
  $('#link-changed').hidden = !draft || draft.base === base;
  $('#link-notice').hidden = dir !== 'both';
  $('#link-notice').textContent = JSON.stringify(ab || null) !== JSON.stringify(ba || null) ? 'The two directions currently have different rules; applying overwrites both with the same settings below.' : 'Applies to both directions at once.';
  const reason = faultBlockReason();
  $('#apply-link').disabled = !!reason || pending.has(linkOperation());
  $('#link-note').textContent = reason || (live ? 'Acts on the live experiment.' : 'You are viewing a replay; this setting still acts on the live experiment.');
}
function renderPopover(state) {
  const pop = $('#element-popover');
  pop.hidden = !popoverOpen || !run;
  if (pop.hidden) return;
  $('#popover-node').hidden = !!linkSel; $('#popover-link').hidden = !linkSel; $('#popover-back').hidden = !linkSel;
  const error = visibleOperations().includes(popoverError?.target) ? popoverError.message : '';
  $('#popover-error').hidden = !error; $('#popover-error').textContent = error;
  if (linkSel) renderLinkPopover(); else renderNodePopover(state);
}
const phoneQuery = typeof matchMedia === 'function' ? matchMedia('(max-width: 700px)') : null;
phoneQuery?.addEventListener?.('change', () => { anchorDirty = true; });
function sheetMode() { return !!phoneQuery?.matches; }
function popoverAnchor() {
  if (linkSel?.origin.kind === 'graph' && graphView !== 'spacetime') return $(`#graph [data-from="${linkSel.a}"][data-to="${linkSel.b}"]`);
  return $(`${graphView === 'spacetime' ? '#spacetime' : '#graph'} [data-node="${linkSel ? linkSel.origin.node : selected}"]`);
}
// The popover lives outside the SVGs (they are rebuilt every frame) and is re-anchored after each
// paint: beside the element when there is room, otherwise above or below it, inside the graph area.
function anchorPopover() {
  const pop = $('#element-popover');
  if (pop.hidden) return;
  if (sheetMode()) { pop.style.left = ''; pop.style.top = ''; pop.style.maxHeight = ''; pop.style.width = ''; return; }
  // Stay inside the drawing itself so the view toolbar and space-time filters remain usable.
  const space = graphView === 'spacetime', anchor = popoverAnchor(), host = $('.lab-grid'), area = $(space ? '.spacetime-scroll' : '.graph-area');
  if (typeof anchor?.getBoundingClientRect !== 'function' || !Number.isFinite(pop.offsetWidth)) return;
  const hostBox = host.getBoundingClientRect(), areaBox = area.getBoundingClientRect(), toolbar = space ? 0 : $('.graph-toolbar').getBoundingClientRect().bottom - areaBox.top;
  const box = { left: areaBox.left, right: areaBox.right, top: areaBox.top + Math.max(0, toolbar), bottom: areaBox.bottom, height: areaBox.height - Math.max(0, toolbar) };
  let r = anchor.getBoundingClientRect();
  if (anchor.dataset.from) { const x = r.left + r.width / 2, y = r.top + r.height / 2; r = { left: x - 8, right: x + 8, top: y - 8, bottom: y + 8, width: 16, height: 16 }; }
  const gap = 12, edge = 8, fullHeight = Math.max(220, Math.min(560, Math.floor(box.height - 16)));
  pop.style.width = ''; pop.style.maxHeight = `${fullHeight}px`;
  let w = pop.offsetWidth, h = pop.offsetHeight, left, top;
  const right = box.right - edge - (r.right + gap), leftSpace = r.left - gap - (box.left + edge);
  const below = box.bottom - edge - (r.bottom + gap), above = r.top - gap - (box.top + edge);
  // Prefer a side; then a narrower side; then above/below with a shorter panel. The anchor stays visible.
  if (right >= w || leftSpace >= w || Math.max(right, leftSpace) >= 240) {
    const useRight = right >= w || (leftSpace < w && right >= leftSpace);
    if (Math.max(right, leftSpace) < w) { w = Math.floor(Math.max(right, leftSpace)); pop.style.width = `${w}px`; h = pop.offsetHeight; }
    left = useRight ? r.right + gap : r.left - gap - w;
    top = Math.min(Math.max(r.top + r.height / 2 - h / 2, box.top + edge), Math.max(box.top + edge, box.bottom - edge - h));
  } else {
    const useBelow = below >= above, room = Math.max(160, Math.floor(useBelow ? below : above));
    pop.style.maxHeight = `${Math.min(fullHeight, room)}px`; h = pop.offsetHeight;
    left = Math.min(Math.max(box.left + edge, r.left + r.width / 2 - w / 2), box.right - edge - w);
    top = useBelow ? r.bottom + gap : r.top - gap - h;
  }
  pop.style.left = `${Math.round(left - hostBox.left)}px`; pop.style.top = `${Math.round(top - hostBox.top)}px`;
}
function resetPopover() { linkSel = null; popoverOpen = false; popoverError = null; }
function backToOriginNode() { openNodePopover(linkSel.origin.node, `[data-node="${linkSel.origin.node}"]`); }
function nodeIndex(node) { return Number(node.split('-')[1]); }
function openNodePopover(node, origin, focusInput = false) {
  selected = node; linkSel = null; popoverOpen = true; popoverError = null;
  if (origin) popoverOrigin = origin;
  renderDirty = true; render();
  const form = [...$('#application-panel').querySelectorAll('form')].find(f => !f.hidden);
  const field = focusInput && form?.querySelector('input, select, textarea');
  (field || $('#popover-title')).focus?.();
}
function openLinkPopover(from, to, origin) {
  const [a, b] = nodeIndex(from) < nodeIndex(to) ? [from, to] : [to, from];
  linkSel = { a, b, dir: from === a ? 'ab' : 'ba', origin: { kind: origin, node: origin === 'graph' ? a : selected } };
  popoverOpen = true; popoverError = null;
  renderDirty = true; render();
  $('#popover-title').focus?.();
}
function closePopover() {
  if (!popoverOpen) return;
  resetPopover();
  renderDirty = true; render();
  // Return focus to where the popover was opened; a redraw may have replaced that element.
  const view = graphView === 'spacetime' ? '#spacetime' : '#graph';
  const origin = (popoverOrigin && $(`${view} ${popoverOrigin}`)) || $(`${view} [data-node="${selected}"]`);
  (origin || $('.graph-area')).focus?.();
}
function renderEvents() {
  const filter = $('#event-filter').value, query = $('#event-search').value.toLowerCase();
  const subset = events.slice(0, cursor).filter(e => (filter === 'all' || e.type === filter) && (!query || JSON.stringify(e).toLowerCase().includes(query))).slice(-100).reverse();
  $('#event-rows').innerHTML = subset.map(e => `<tr data-event="${e.seq}" class="${e.seq === cursor ? 'current-event' : ''}"><td><span class="event-time">#${String(e.seq).padStart(3, '0')}</span>${time(e.time, true)}</td><td><span class="event-tag ${e.type}">${labels[e.type] || esc(e.type)}</span></td><td>${esc(e.from ? `${e.from} → ${e.to}` : e.node || 'coordinator')}</td><td title="${esc(eventContent(e))}">${esc(eventContent(e))}</td></tr>`).join('') || '<tr><td colspan="4" class="empty-table">No matching events at the current position.</td></tr>';
  $('#event-count').textContent = cursor.toLocaleString();
}
// The run shown on screen and the globally active run can differ (history replay, another
// experiment running). The primary button never targets a run the user is not looking at.
function runControlState() {
  const active = experiments.find(e => ['starting', 'running'].includes(e.latestRun?.status));
  if (run && ['starting', 'running'].includes(run.status)) return { mode: run.status === 'starting' ? 'starting' : 'stop' };
  if (active) return { mode: 'goto', experimentId: active.id, name: active.name };
  if (!run) return { mode: 'run' };
  return { mode: run.status === 'failed' ? 'failed' : 'rerun' };
}
function renderControls() {
  const state = runControlState(), start = $('#new-run'), stop = $('#stop-run');
  stop.hidden = !['stop', 'starting'].includes(state.mode); start.hidden = !stop.hidden;
  if (!stop.hidden) {
    const label = state.mode === 'starting' ? 'Starting…' : stopPending() ? 'Stopping…' : 'Stop run';
    if (stop.dataset.label !== label) { stop.dataset.label = label; stop.innerHTML = icon('stop') + label; }
    stop.disabled = state.mode === 'starting' || stopPending();
    stop.title = state.mode === 'starting' ? 'Stop the run once the nodes finish building' : '';
  } else {
    const label = { run: 'Run experiment', rerun: 'Run again', failed: 'Run again', goto: 'Go to the running experiment' }[state.mode];
    if (start.dataset.label !== label) { start.dataset.label = label; start.innerHTML = icon(state.mode === 'goto' ? 'network' : 'play') + label; }
    start.dataset.mode = state.mode;
    start.title = state.mode === 'goto' ? `"${state.name}" is running` : '';
  }
  $('#view-error').hidden = state.mode !== 'failed';
  // The run history offers the same entry point; while a run is active it leads back to it.
  const historyLabel = stop.hidden ? start.dataset.label : 'View the running experiment';
  if ($('#history-run').textContent !== historyLabel) $('#history-run').textContent = historyLabel;
}
// Everything the drawing depends on about the popover selection, including a link's direction.
function selectionStamp() {
  if (!popoverOpen || !run) return 'null';
  return JSON.stringify(linkSel ? { kind: 'link', a: linkSel.a, b: linkSel.b, dir: linkSel.dir } : { kind: 'node', node: selected });
}
function render() {
  const state = snapshot();
  $('#empty-graph').hidden = Boolean(run);
  const stamp = `${run?.id}:${graphView}:${cursor}:${playTime}:${selected}:${selectionStamp()}:${schemaRev}:${graphBox.width}x${graphBox.height}:${spaceWindow}:${spaceStart}:${spaceFollow}:${spaceNode}:${hideHeartbeats}`;
  if (stamp !== graphStamp) {
    graphStamp = stamp;
    if (graphView === 'spacetime') drawSpaceTime(state);
    else drawGraph(state);
    anchorDirty = true;
  }
  if (renderDirty) { renderDirty = false; renderPanels(state); anchorDirty = true; }
  // Re-anchoring reads layout, so it runs only after something could have moved the popover.
  if (anchorDirty) { anchorDirty = false; anchorPopover(); }
}
// Generic: the text comes from the protocol's `// distvis:start` comment, never from the UI itself.
function renderStartHint() {
  const s = runStart(), quiet = !!s && !!run && run.status === 'running' && live && !(liveState?.sends > 0);
  $('#start-hint').hidden = !quiet;
  if (!quiet) return;
  const ready = !!startAction(s.node);
  $('#start-hint-text').textContent = `No messages yet. ${startText(s)}`;
  $('#start-hint-open').hidden = !ready;
  $('#start-hint-open').textContent = `Prepare to run on ${s.node}`;
}
function renderPanels(state) {
  if (run) {
    $('#experiment-name').textContent = run.config.name;
    $('#experiment-meta').textContent = `${run.config.nodeCount} nodes · ${ { simulation: 'Built-in reference model', docker: 'Docker / real Go', kubernetes: 'K8s / real Go' }[run.config.runtime]} · ${run.config.latency}${jitterText(run.config.delayModel, run.config.jitter)} ms latency${networkText(run.config.network)}${run.config.runtime==='simulation'?` · Seed ${run.config.seed}`:''}`;
    $('#run-status').textContent = statuses[run.status];
    $('#run-status').className = `badge ${run.status === 'running' ? '' : run.status === 'failed' ? 'failed' : 'neutral'}`;
    $('#mode-label').textContent = live ? '● Live' : playing ? `▶ ${speed}× replay` : 'Ⅱ Replay paused';
    // Startup diagnostics are run-level information, also visible at replay position 0.
    $('#run-error').hidden = run.status !== 'failed';
    if (run.status === 'failed') {
      const failure = events.find(e => e.level === 'error' && e.phase === 'startup')
        || events.find(e => e.type === 'runtime' && /失败|[Ee]rror|[Ff]ailed/.test(e.message || ''));
      const message = failure?.message || 'Check the runtime logs in the event stream.';
      $('#run-error-hint').textContent = message.split('\n')[0];
      $('#run-error-log').textContent = message;
    }
  }
  if(!run){$('#experiment-name').textContent='Not run yet';$('#experiment-meta').textContent='Start a run using the code of the owning protocol and the settings of this experiment';$('#run-status').textContent='Ready';$('#run-status').className='badge neutral';$('#run-error').hidden=true;$('#mode-label').textContent='Ready';}
  renderControls();
  $('#view-run-source').disabled=!run;$('#export-run').disabled=!run;
  $('#metric-nodes').innerHTML = run ? `${Object.values(state.online).filter(Boolean).length}<span>/ ${run.config.nodeCount}</span>` : '—';
  $('#metric-messages').textContent = state.sends.toLocaleString();
  renderStartHint();
  $('#metric-faults').textContent = state.faults.length;
  $('#timecode').textContent = time(playTime, true);
  $('#timeline').max = Math.max(1, events.length); $('#timeline').value = cursor;
  $('#timeline-end').textContent = time(latestTime);
  $('#fault-markers').innerHTML = events.filter(e => e.type === 'fault').map(e => `<i style="left:${e.seq / Math.max(1, events.length) * 100}%" title="${esc(faultText(e.fault))}"></i>`).join('');
  $('#play').innerHTML = icon(live || playing ? 'pause' : 'play');
  // Heal and the fault history act on and describe the live run, whatever the popover shows.
  $('#heal-links').disabled = !run || !!faultBlockReason() || pending.has(`${run.id}:heal`);
  const faultStamp = run ? `${run.id}:${liveState.faults.length}` : '';
  if ($('#fault-history').dataset.stamp !== faultStamp) {
    $('#fault-history').dataset.stamp = faultStamp;
    $('#fault-history').innerHTML = run ? liveState.faults.slice(-6).reverse().map(e => `<div class="fault-item"><span>${esc(faultText(e.fault))}</span><span>${time(e.time)}</span></div>`).join('') || 'No faults yet' : 'No faults yet';
  }
  if (run) renderPopover(state); else $('#element-popover').hidden = true;
  renderEvents();
}
function seek(count) {
  live = false; playing = false; cursor = Math.max(0, Math.min(events.length, Number(count)));
  spaceFollow = true;
  playTime = cursor ? events[cursor - 1].time : 0; renderDirty = true; render();
}
function eventDetail(seq, { locate = true } = {}) {
  const event = events.find(e => e.seq === Number(seq));
  if (!event) return;
  if (locate) seek(event.seq);
  else { live = false; playing = false; renderDirty = true; render(); }
  const message = ['send', 'receive', 'deliver', 'drop'].includes(event.type);
  $('#event-title').textContent = message ? 'Message details' : event.type === 'state' ? `${event.node} · state report` : 'Event details';
  let summary = '';
  if (message) {
    const send = sends.get(event.id) || event;
    const end = observedEnd(send);
    const status = end?.type === 'drop' ? `Dropped · ${end.reason}` : end ? 'Received' : 'Not yet received at the current position';
    summary = `<div class="message-route"><span>${esc(event.from)}</span><b>→</b><span>${esc(event.to)}</span></div><p class="message-kind">${esc(event.payload?.method || event.payload?.type || 'Message')}</p><div class="detail-row"><span>Message ID</span><strong>${esc(event.id)}</strong></div><div class="detail-row"><span>Sent at</span><strong>${time(send.time, true)} · #${send.seq}</strong></div><div class="detail-row"><span>Status at the current position</span><strong>${esc(status)}</strong></div>${end ? `<div class="detail-row"><span>${end.type === 'drop' ? 'Dropped' : 'Received'} at</span><strong>${time(end.time, true)} · #${end.seq}</strong></div>` : ''}<div class="inspector-section-title">Message content <span>PAYLOAD</span></div>${event.payload?.rpcId ? `<div class="detail-row"><span>RPC / phase</span><strong>${esc(event.payload.rpcId)} · ${esc(event.payload.type)}</strong></div>${event.payload.error ? `<p class="form-help">${esc(event.payload.error)}</p>` : ''}${fieldRows(event.payload.body)}` : fieldRows(event.payload, { omit: ['type'] })}`;
  } else if (event.type === 'command_result') {
    summary = `<p class="message-kind">${esc(event.node)} · ${esc(event.commandId)}</p>${event.error ? `<p>${esc(event.error)}</p>` : fieldRows(event.result)}`;
  } else if (event.type === 'command') {
    summary = `<p class="message-kind">${esc(event.node)} · ${esc(event.action || 'write')}</p>${fieldRows(event.values || {key:event.key,value:event.value})}`;
  } else if (event.type === 'state') {
    summary = `<p class="form-help">${time(event.time, true)} · #${event.seq}</p>${fieldRows(event.state)}`;
  }
  $('#event-summary').innerHTML = summary;
  $('#event-raw').open = false;
  $('#event-json').textContent = JSON.stringify(event, null, 2);
  $('#event-dialog').showModal();
}
function graphClick(e) {
  const badge = e.target.closest('[data-input-node]'), node = e.target.closest('[data-node]'), link = e.target.closest('[data-from]'), event = e.target.closest('[data-event]');
  if (badge) openNodePopover(badge.dataset.inputNode, elementSelector(badge), true);
  else if (node) openNodePopover(node.dataset.node, elementSelector(node));
  else if (event) eventDetail(event.dataset.event, { locate: false });
  else if (link) { popoverOrigin = elementSelector(link); openLinkPopover(link.dataset.from, link.dataset.to, 'graph'); }
  else if (popoverOpen) closePopover();
}
for (const svg of [$('#graph'), $('#spacetime')]) {
  svg.addEventListener('click', graphClick);
  svg.addEventListener('keydown', e => {
    if (e.key === 'Enter' || e.key === ' ') {
      e.preventDefault();
      e.target.closest('[data-input-node], [data-node], [data-event], [data-from]')?.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    }
  });
}
$$('.view-switch button').forEach(button => { button.onclick = () => switchView(button.dataset.view); });
$('#spacetime-window').onchange = e => zoomSpace(Number(e.target.value));
const spaceScales = [100, 250, 500, 1000, 2000, 5000, 10000, 30000];
$('#spacetime-zoom-in').onclick = () => zoomSpace(spaceScales[Math.max(0, spaceScales.indexOf(spaceWindow) - 1)]);
$('#spacetime-zoom-out').onclick = () => zoomSpace(spaceScales[Math.min(spaceScales.length - 1, spaceScales.indexOf(spaceWindow) + 1)]);
$('#spacetime-follow').onclick = followSpace;
$('#spacetime-back').onclick = () => panSpace(spaceStart - spaceWindow / 2);
$('#spacetime-forward').onclick = () => panSpace(spaceStart + spaceWindow / 2);
$('#spacetime-pan').oninput = e => panSpace(Number(e.target.value));
$('#spacetime-node').onchange = e => { spaceNode = e.target.value; graphStamp = ''; render(); };
$('#spacetime-heartbeats').onchange = e => { hideHeartbeats = e.target.checked; graphStamp = ''; render(); };
const spaceCanvas = $('.spacetime-scroll');
spaceCanvas.addEventListener('pointerdown', e => {
  if (e.button !== 0) return;
  suppressSpaceClick = false;
  spaceDrag = { id: e.pointerId, x: e.clientX, y: e.clientY, start: spaceStart, moved: false };
});
spaceCanvas.addEventListener('pointermove', e => {
  if (!spaceDrag || spaceDrag.id !== e.pointerId) return;
  const dx = e.clientX - spaceDrag.x, dy = e.clientY - spaceDrag.y;
  if (!spaceDrag.moved) {
    if (Math.abs(dx) < 5 || Math.abs(dx) < Math.abs(dy)) return;
    spaceDrag.moved = true;
    spaceCanvas.setPointerCapture(e.pointerId);
    spaceCanvas.classList.add('dragging');
  }
  e.preventDefault();
  suppressSpaceClick = true;
  panSpace(spaceDrag.start - dx / spaceMetrics().plotWidth * spaceWindow);
});
function endSpaceDrag(e) {
  if (!spaceDrag || e.pointerId !== spaceDrag.id) return;
  if (spaceCanvas.hasPointerCapture(e.pointerId)) spaceCanvas.releasePointerCapture(e.pointerId);
  spaceDrag = null; spaceCanvas.classList.remove('dragging');
}
spaceCanvas.addEventListener('pointerup', endSpaceDrag);
spaceCanvas.addEventListener('pointercancel', endSpaceDrag);
spaceCanvas.addEventListener('click', e => {
  // Dragging can start on a message: the release must not open its details.
  if (suppressSpaceClick && e.detail !== 0) { e.preventDefault(); e.stopPropagation(); }
}, true);
spaceCanvas.addEventListener('wheel', e => {
  const dx = e.shiftKey ? (e.deltaY || e.deltaX) : e.deltaX;
  if (!dx) return; // Keep vertical wheel/trackpad scrolling available for many nodes.
  e.preventDefault();
  const pixels = dx * (e.deltaMode === 1 ? 16 : e.deltaMode === 2 ? spaceMetrics().plotWidth : 1);
  panSpace(spaceStart + pixels / spaceMetrics().plotWidth * spaceWindow);
}, { passive: false });
spaceCanvas.addEventListener('keydown', e => {
  if (e.target !== spaceCanvas || !['ArrowLeft', 'ArrowRight'].includes(e.key)) return;
  e.preventDefault();
  panSpace(spaceStart + (e.key === 'ArrowLeft' ? -1 : 1) * spaceWindow / 4);
});
if (typeof ResizeObserver !== 'undefined') {
  new ResizeObserver(() => {
    if (graphView === 'spacetime') { graphStamp = ''; render(); }
  }).observe(spaceCanvas);
  // Topology geometry follows its container; the callback is coalesced into one frame and the
  // SVG's size never feeds back into the observed box (the area scrolls instead).
  let resizeFrame=0;
  new ResizeObserver(entries => {
    const box=entries[0].contentRect, next={ width: Math.floor(box.width), height: Math.floor(box.height) };
    if (next.width === graphBox.width && next.height === graphBox.height) return;
    cancelAnimationFrame(resizeFrame);
    resizeFrame=requestAnimationFrame(() => { graphBox=next; graphStamp=''; renderDirty=true; render(); });
  }).observe($('.graph-area'));
}
$('#event-rows').onclick = e => { const row = e.target.closest('[data-event]'); if (row) eventDetail(row.dataset.event); };
$('#popover-close').onclick = closePopover;
for (const scroller of ['.graph-area', '.spacetime-scroll']) $(scroller).addEventListener('scroll', () => { anchorDirty = true; }, { passive: true });
$('#popover-back').onclick = () => { if (linkSel) backToOriginNode(); };
$('#node-toggle').onclick = guard(async () => {
  if (!run || linkSel) return;
  const node = selected, action = $('#node-toggle').dataset.nodeAction;
  await mutate({ key: toggleOperation(node), fault: true, path: `/api/runs/${run.id}/faults`, body: { kind: action, node },
    success: (result, current) => { if (current) notify(action === 'crash' ? `${node} crashed · live experiment` : `${node} recovered · live experiment`); } });
});
$('#peer-links').onclick = e => { const peer = e.target.closest('[data-peer]'); if (peer) openLinkPopover(selected, peer.dataset.peer, 'node'); };
$('.direction-choice').onclick = e => {
  const button = e.target.closest('[data-dir]');
  if (button && linkSel) { linkSel.dir = button.dataset.dir; renderDirty = true; render(); }
};
function saveLinkDraft() {
  if (!linkSel || !run) return;
  // The baseline is the live rule when editing began, so a later live change is still reported.
  const key = linkOperation(), base = linkDrafts.get(key)?.base ?? $('#link-form').dataset.base;
  linkDrafts.set(key, { latency: $('#link-latency').value, bandwidth: $('#link-bandwidth').value, blocked: $('#link-blocked').checked, delayModel: $('#link-model').value, jitter: $('#link-jitter').value, base });
  renderDirty = true;
}
$('#link-form').addEventListener('input', saveLinkDraft);
$('#link-form').addEventListener('change', saveLinkDraft);
$('#link-use-latest').onclick = () => { if (!linkSel) return; linkDrafts.delete(linkOperation()); $('#link-form').dataset.key = ''; renderDirty = true; render(); };
$('#link-form').addEventListener('submit', guard(async e => {
  e.preventDefault();
  if (!linkSel || !run) return;
  const { dir } = linkSel, [from, to] = linkEnds(), key = linkOperation(), submitted = linkDrafts.get(key);
  const latency = String($('#link-latency').value).trim(), bandwidth = String($('#link-bandwidth').value).trim();
  const delayModel = $('#link-model').value, jitter = delayModel === 'fixed' ? '0' : String($('#link-jitter').value).trim();
  const valid = (text, min, max) => /^\d+$/.test(text) && Number(text) >= min && Number(text) <= max;
  if (!valid(latency, 0, 30000) || !valid(bandwidth, 1, 100000) || !valid(jitter, delayModel === 'fixed' ? 0 : 1, 30000)) {
    popoverError = { target: linkOperation(), message: 'Latency must be whole milliseconds in 0–30000, bandwidth must be whole KiB/s in 1–100000, and the exponential jitter mean must be 1–30000 ms.' };
    renderDirty = true; render(); return;
  }
  await mutate({ key: linkOperation(), fault: true, path: `/api/runs/${run.id}/faults`,
    body: { kind: 'link', from, to, latency: Number(latency), bandwidth: Number(bandwidth), blocked: $('#link-blocked').checked, bidirectional: dir === 'both', delayModel, jitter: Number(jitter) },
    // Edits typed while the request was in flight are newer than what was sent; keep them.
    // Only a reply the user is still looking at retires the draft, and only the exact draft that was sent.
    success: (result, current) => { if (!current) return; if (linkDrafts.get(key) === submitted) { linkDrafts.delete(key); $('#link-form').dataset.key = ''; } notify('Link settings applied and recorded'); } });
}));
$('#heal-links').onclick = guard(async () => {
  if (!run) return;
  await mutate({ key: `${run.id}:heal`, scope: 'run', fault: true, path: `/api/runs/${run.id}/faults`, body: { kind: 'heal' },
    success: (result, current) => { if (current) notify('All links restored to their default settings'); } });
});
$('#view-error').onclick = () => { $('#run-error-details').open = true; $('#run-error').scrollIntoView?.({ block: 'nearest' }); $('#run-error-details summary')?.focus?.(); };
$('#play').onclick = () => {
  if (!run) return;
  if (live) { live = false; playing = false; }
  else { if (cursor === events.length && run.status !== 'running') seek(0); playing = !playing; }
  renderDirty = true; render();
};
$('#rewind').onclick = () => seek(0);
$('#step').onclick = () => seek(cursor + 1);
$('#timeline').oninput = e => seek(e.target.value);
$('#speed').onchange = e => {
  speed = Number(e.target.value);
  // Selecting a playback rate while observing live must actually activate that rate.
  if (live && run) { live = false; playing = true; }
  renderDirty = true; render();
};
$('#go-live').onclick = () => { live = true; playing = false; cursor = events.length; playTime = latestTime; spaceFollow = true; renderDirty = true; render(); };
$('#start-hint-open').onclick = () => {
  const s = runStart(), action = s && startAction(s.node);
  if (!action) return;
  // Pre-fill the form from the comment's arguments; sending stays the user's click.
  const key = inputDraftKey(s.node), draft = inputDrafts.get(key) || {}, fields = nodeInputSchema(s.node).find(a => a.action === action)?.fields || [];
  draft[action] = { ...(draft[action] || {}) };
  for (const f of fields) if (Object.hasOwn(s.values, f.name)) {
    const v = s.values[f.name];
    draft[action][f.name] = { type: f.type, value: f.type === 'boolean' ? !!v : f.type === 'json' ? JSON.stringify(v) : f.type === 'nodes' ? (Array.isArray(v) ? v : [v]).map(String) : String(v) };
  }
  inputDrafts.set(key, draft); inputActions.set(key, action);
  delete $('#application-panel').dataset.stamp;
  openNodePopover(s.node, `[data-node="${s.node}"]`, true);
};
$('#reset-view').onclick = () => { closePopover(); selected = 'node-1'; spaceFollow = true; graphStamp = ''; renderDirty = true; render(); };
$('#event-filter').onchange = () => { renderDirty = true; render(); };
$('#event-search').oninput = () => { renderDirty = true; render(); };
$('#stop-run').onclick = guard(async () => {
  if (!run || stopPending() || run.status !== 'running') return;
  const token = { loadGen: loadingRun, runId: run.id, scope: 'run' };
  stopsInFlight.add(token.runId); renderDirty = true; render();
  try {
    const info = await api(`/api/runs/${token.runId}/stop`, {});
    setRunStatus(token.runId, info.status);
    if (isCurrent(token)) notify('The run has ended; the full record is saved');
    await refreshRuns();
  } catch (error) {
    // A failure for a run the user has already left is not reported on the page they are now on.
    if (isCurrent(token)) notify(error.message);
  } finally { stopsInFlight.delete(token.runId); renderDirty = true; }
});
$('#export-run').onclick = () => { if (run) location.href = `/api/runs/${run.id}/export`; else notify('Create an experiment first'); };
function protocolChanged() {
  const form=$('#project-form'), protocol=form.elements.protocol.value;
  $('#protocol-description').textContent=directoryProjects.find(p=>p.id===protocol)?.description || '';
}
async function refreshProtocols() {
  const [catalog,library,templates]=await Promise.all([api('/api/protocols'),api('/api/library'),api('/api/examples')]);
  protocols=catalog;
  const select=$('#project-form').elements.protocol, current=select.value;
  // Known example paths retain their optional reference model, but every visible
  // choice comes from the selected directory, with no separate template catalog.
  directoryProjects=library.entries.map(p=>({...p,id:templates.find(t=>t.directory===p.directory)?.id || p.id}));
  select.innerHTML=directoryProjects.length?directoryProjects.map(p=>`<option value="${esc(p.id)}">${esc(p.name)} · ${esc(p.folder)}</option>`).join(''):'<option value="">No usable Go projects in the directory</option>';
  select.value=directoryProjects.some(p=>p.id===current)?current:directoryProjects.find(p=>p.id==='raft')?.id || directoryProjects[0]?.id || '';
  select.disabled=!directoryProjects.length;
  $('#project-form [type="submit"]').disabled=!directoryProjects.length;
  $('#project-form .protocol-library').open=!directoryProjects.length;
  $('#protocol-root').value=library.root;
  $('#library-status').textContent=`Found ${library.entries.length} projects${library.errors.length?' · '+library.errors.join('; '):''}`;
}
function scopedRuns() { return runs.filter(r=>r.config.experimentId===experiment?.id); }
function experimentCard(item) {
  const active=['running','starting'].includes(item.latestRun?.status);
  return `<button class="experiment-card" data-experiment="${esc(item.id)}"><span class="item-icon">${icon('flask')}</span><span class="item-main"><strong>${esc(item.name)}</strong><small>${item.settings.nodeCount} nodes · ${item.settings.latency}${jitterText(item.settings.delayModel, item.settings.jitter)} ms${esc(networkText(item.settings.network))} · ${esc(item.settings.runtime)} · ${item.runCount} runs</small></span>${active?`<span class="badge">${statuses[item.latestRun.status]}</span>`:''}<span class="item-arrow" aria-hidden="true">→</span></button>`;
}
function renderLibrary() {
  const query=$('#experiment-search').value.toLowerCase();
  const matches=protocolProjects.filter(p=>p.name.toLowerCase().includes(query));
  $('#experiment-cards').innerHTML=matches.filter(p=>!p.archived).map(parentCard).join('') || '<div class="library-empty"><h3>Create or open a protocol</h3><p>Protocols hold the Go code; experiments hold parameters and execution records.</p></div>';
  const imported=matches.filter(p=>p.archived);$('#imported-cards').innerHTML=imported.map(parentCard).join('');$('#imported-experiments').hidden=!imported.length;$('#imported-count').textContent=imported.length;
  if(query && imported.length)$('#imported-experiments').open=true;
  $('#recent-experiments').innerHTML=protocolProjects.filter(p=>!p.archived).slice(0,7).map(p=>`<button class="recent-experiment ${p.id===protocolProject?.id?'selected':''}" data-protocol-project="${esc(p.id)}"><span class="protocol-dot teal"></span><span>${esc(p.name)}</span></button>`).join('');
  if(protocolProject)renderChildren();
  const active=experiments.find(e=>['starting','running'].includes(e.latestRun?.status));
  for(const selector of ['#active-experiment-notice','#running-elsewhere']){
    const el=$(selector);el.hidden=!active || (selector==='#running-elsewhere' && active.id===experiment?.id && run?.id===active.latestRun?.id);
    el.innerHTML=active?`<span><i class="status-dot"></i><strong>${esc(active.name)}</strong> is ${active.latestRun.status==='starting'?'starting':'running'}</span><button class="button small" data-experiment="${esc(active.id)}" data-active-run="true">Open visualization →</button>`:'';
  }
  renderDirty=true;
}
async function refreshRuns() {
  const seq=++refreshSeq,id=experiment?.id;
  const [items,parents,history]=await Promise.all([api('/api/experiments'),api('/api/protocol-projects'),id?api(`/api/experiments/${id}/runs`):Promise.resolve([])]);
  // Polls overlap with explicit refreshes; a response older than one already applied is dropped.
  if(seq<refreshApplied)return;
  refreshApplied=seq;
  protocolProjects=parents;
  experiments=items;
  // A poll that started before a lifecycle event must not bring an ended run back to life.
  for(const item of experiments)if(item.latestRun)item.latestRun.status=noteStatus(item.latestRun.id,item.latestRun.status);
  renderLibrary();$('#connection-status').textContent='Connected';
  if(id!==experiment?.id)return;
  for(const item of history)item.status=noteStatus(item.id,item.status);
  runs=history;$('#history-count').textContent=scopedRuns().length;
  if(run){const current=runs.find(r=>r.id===run.id);if(current){setRunStatus(run.id,current.status);latestTime=Math.max(latestTime,current.time);renderDirty=true;}}
  if(workspaceView==='history')renderHistory();
}
function clearRun() {
  $('#run-details').open=false;
  ++loadingRun;stream?.close();stream=null;run=null;events=[];ends.clear();sends.clear();
  live=false;playing=false;cursor=0;playTime=0;latestTime=0;graphStamp='';resetCache();liveState=null;populateNodes();
  resetPopover();$('#element-popover').hidden=true;
  for(const id of ['#node-panel','#application-panel','#peer-links','#link-rules']){$(id).innerHTML='';delete $(id).dataset.stamp;}
  $('#link-form').dataset.key='';
  $('#fault-history').textContent='No faults yet';
  $('#event-dialog').close();renderDirty=true;render();
}
function viewWorkspace(view) {
  if(!experiment)return;
  $('#protocol-experiments-panel').hidden=true;
  workspaceView=view;setVisualMode(view==='visual');
  $('#code-dialog').hidden=view!=='code';$('#visual-panel').hidden=view!=='visual';$('#history-panel').hidden=view!=='history';
  $$('[data-workspace-view]').forEach(b=>{b.classList.toggle('selected',b.dataset.workspaceView===view);b.setAttribute('aria-current',b.dataset.workspaceView===view?'page':'false');});
  localStorage.setItem(`distvis-view:${experiment.id}`,view);
  window.history?.replaceState(null,'',`#experiment/${experiment.id}/${view}`);
  if(view==='code')renderCode();
  if(view==='history')renderHistory();
  if(view==='visual'){graphStamp='';renderDirty=true;render();}
}
async function openExperiment(id,preferredView) {
  stashDraft();const ticket=++loadingExperiment;
  const [item,history]=await Promise.all([api(`/api/experiments/${id}`),api(`/api/experiments/${id}/runs`)]);
  const parent=await api(`/api/protocol-projects/${item.protocolId}`);
  if(ticket!==loadingExperiment)return;
  clearRun();experiment=item;runs=history;loadProtocolCode(parent);
  renderProtocolShell();renderLibrary();localStorage.setItem('distvis-last-experiment',id);localStorage.setItem('distvis-last-protocol',parent.id);
  const active=history.find(r=>['running','starting'].includes(r.status));
  const last=history.find(r=>r.id===localStorage.getItem(`distvis-run:${id}`)) || history[0];
  if(active || last)await openRun((active || last).id,!!active);
  if(ticket!==loadingExperiment)return;
  viewWorkspace(preferredView==='code'?'visual':preferredView || 'visual');
}
async function showLibrary() {
  stashDraft();++loadingExperiment;clearRun();experiment=null;protocolProject=null;runs=[];codeArchive=null;codeDrafts={};codeDirty=false;workspaceView='library';
  $('#experiment-workbench').hidden=true;$('#experiment-library').hidden=false;$('#experiment-tabs').hidden=true;setVisualMode(false);
  $('#breadcrumb-protocol').hidden=true;$('#breadcrumb-child-separator').hidden=true;$('#breadcrumb-experiment').textContent='';$('#breadcrumb-separator').hidden=true;$('#nav-library').classList.add('active');
  localStorage.setItem('distvis-last-experiment','');localStorage.setItem('distvis-last-protocol','');window.history?.replaceState(null,'','#library');
  await refreshRuns();
}
async function openNew() {
  await refreshProtocols();const form=$('#project-form');
  form.elements.name.value=directoryProjects.find(p=>p.id===form.elements.protocol.value)?.name?.split(' · ')[0] || 'My protocol';
  protocolChanged();$('#project-dialog').showModal();
}
async function configureRun() {
  runFormMode='run';
  $('#run-dialog-title').textContent='Run this experiment';$('#run-name-label').textContent='Run name';$('#run-submit').textContent='Start run →';$('#run-dialog-note').textContent='Opens the visualization after starting';$('#save-experiment-settings').hidden=false;
  if(!experiment)throw new Error('Open an experiment first');
  if(codeArchive){viewWorkspace('code');throw new Error('You are viewing historical code; go back to the current code or continue editing from this version');}
  await refreshRuns();
  const active=experiments.find(e=>['starting','running'].includes(e.latestRun?.status));
  if(active)throw new Error(`"${active.name}" is still running; stop that run first`);
  const form=$('#new-form');
  form.elements.delayModel.value='fixed';form.elements.jitter.value=0;
  for(const [key,value] of Object.entries(experiment.settings))if(form.elements[key])form.elements[key].value=value;
  fillNetworkForm(form,experiment.settings.network);
  syncJitterField(form.elements.delayModel,form.elements.jitter);
  form.elements.name.value=`${experiment.name} · Run ${scopedRuns().length+1}`.slice(0,100);
  const model=form.elements.runtime.querySelector('[value="simulation"]');model.disabled=!['raft','token','gossip'].includes(experiment.protocol);
  if(model.disabled && form.elements.runtime.value==='simulation')form.elements.runtime.value='docker';
  $('#run-project-name').textContent=`${protocolProject.name} / ${experiment.name} · protocol v${protocolProject.revision}`;
  $('#new-dialog').showModal();
}
$('#scan-protocols').onclick=guard(async()=>{await api('/api/library',{root:$('#protocol-root').value});await refreshProtocols();protocolChanged();notify('Protocol directory updated');});
$('#create-experiment').onclick=$('#create-experiment-side').onclick=guard(()=>openNew());
for(const selector of ['#experiment-cards','#imported-cards','#recent-experiments','#active-experiment-notice','#running-elsewhere'])$(selector).onclick=guard(e=>{const p=e.target.closest('[data-protocol-project]');if(p)return openProtocol(p.dataset.protocolProject);const b=e.target.closest('[data-experiment]');if(b)return openExperiment(b.dataset.experiment,b.dataset.activeRun?'visual':undefined);});
$('#experiment-search').oninput=renderLibrary;
$('#nav-library').onclick=$('#breadcrumb-home').onclick=guard(showLibrary);
$('#new-run').onclick=$('#history-run').onclick=guard(()=>{
  const state=runControlState();
  if(['stop','starting'].includes(state.mode))return viewWorkspace('visual');
  return state.mode==='goto'?openExperiment(state.experimentId,'visual'):configureRun();
});
$('#project-form').elements.protocol.onchange=protocolChanged;
$('#project-form').onsubmit=guard(async e=>{
  e.preventDefault();const button=e.target.querySelector('[type="submit"]');button.disabled=true;
  try{const item=await api('/api/protocol-projects',Object.fromEntries(new FormData(e.target)));$('#project-dialog').close();await refreshRuns();await openProtocol(item.id,'code');notify('Protocol created; you can edit the code and add experiments');}finally{button.disabled=false;}
});
$('#new-form').onsubmit=guard(async e=>{
  e.preventDefault();const button=e.target.querySelector('[type="submit"]');button.disabled=true;
  try{
    const input=Object.fromEntries(new FormData(e.target));
    const settings={...Object.fromEntries(['runtime','nodeCount','seed','latency','bandwidth','delayModel','jitter'].map(k=>[k,input[k]])),network:networkFromForm(e.target)};
    if(runFormMode==='create'){
      const item=await api('/api/experiments',{name:input.name,protocolId:protocolProject.id,settings});
      $('#new-dialog').close();await refreshRuns();await openExperiment(item.id);return;
    }
    const owner=experiment.id;
    if(codeDirty)throw new Error('The protocol has unsaved edits; save them on the protocol code page first, or reopen the saved version');
    experiment=await api(`/api/experiments/${owner}`,{revision:experiment.revision,settings});
    if(experiment?.id!==owner)throw new Error('The experiment changed underneath; no run was started');
    const result=await api('/api/runs',{...input,experimentId:owner});
    $('#new-dialog').close();await openRun(result.id,true);viewWorkspace('visual');await refreshRuns();
    notify(input.runtime==='simulation'?'The reference model has started':'Building and starting the Go nodes for this experiment');
  }finally{button.disabled=false;}
});
function renderHistory() {
  const expanded=new Set($$('#history-list details[open]').map(d=>d.dataset.runInfo));
  const history=scopedRuns();
  $('#history-count').textContent=history.length;
  $('#history-list').innerHTML=history.map(r=>`<article class="history-row"><span class="metric-icon teal-bg">${icon('history')}</span><div><h3>${esc(r.config.name)} <span class="badge neutral">${statuses[r.status]}</span></h3><p>${r.config.nodeCount} nodes · ${esc(r.config.runtime)} · ${new Date(r.createdAt).toLocaleString('en-US')} · ${r.eventCount} events</p><details class="history-config" data-run-info="${r.id}" ${expanded.has(r.id)?'open':''}><summary>Settings and code</summary><p>Latency ${r.config.latency}${jitterText(r.config.delayModel, r.config.jitter)} ms · bandwidth ${r.config.bandwidth} KiB/s · protocol v${r.config.protocolRevision || 'legacy'} · code ${esc(r.config.project?.sha256?.slice(0,8) || 'legacy snapshot')}</p><div class="workspace-buttons"><button class="button small" data-source-run="${r.id}">View code</button><button class="button small" data-export="${r.id}">Export record</button></div></details></div><button class="button" data-history="${r.id}">${['running','starting'].includes(r.status)?'Open run':'Replay'} →</button></article>`).join('') || '<div class="library-empty"><h3>This experiment has no runs yet</h3><p>Click \"Run experiment\" above. Every run, with its faults and code, is saved here.</p></div>';
}
async function showHistory(){viewWorkspace('history');await refreshRuns();}
$('#history-list').onclick=guard(async e=>{
  const button=e.target.closest('[data-history]'),source=e.target.closest('[data-source-run]'),exp=e.target.closest('[data-export]');
  if(button){const r=scopedRuns().find(r=>r.id===button.dataset.history);if(!r)return;await openRun(r.id,['running','starting'].includes(r.status));viewWorkspace('visual');}
  if(source)await showRunCode(source.dataset.sourceRun);
  if(exp && scopedRuns().some(r=>r.id===exp.dataset.export))location.href=`/api/runs/${exp.dataset.export}/export`;
});
$('#nav-history').onclick=$('#refresh-history').onclick=guard(showHistory);
$('#nav-lab').onclick=()=>viewWorkspace('visual');
function openDocumentation(e) {
  e?.preventDefault();
  stashDraft();
  try{sessionStorage.setItem('distvis-docs-return',location.hash || '#library');}catch{}
  location.href='/docs/go';
}
$('#nav-guide').onclick=openDocumentation;
$('.mobile-docs').onclick=openDocumentation;
document.addEventListener('click',e=>{for(const menu of $$('.more-menu[open], .legend-menu[open]'))if(!menu.contains(e.target))menu.open=false;});

$$('.close-dialog').forEach(b=>{b.onclick=()=>b.closest('dialog').close();});
$$('dialog').forEach(d=>{d.addEventListener('click',e=>{if(e.target===d&&(e.clientX<d.getBoundingClientRect().left||e.clientX>d.getBoundingClientRect().right||e.clientY<d.getBoundingClientRect().top||e.clientY>d.getBoundingClientRect().bottom))d.close();});});
let codeFile = 'main.go', codeArchive = null;
function legacyProject(source, name = 'Migrated Go project') {
  return {name, entry:'.', files:[
    {path:'main.go', content:source, encoding:'utf8'},
    {path:'go.mod', content:'module lesson/custom\n\ngo 1.24\n\nrequire distvis v0.0.0\n', encoding:'utf8'},
  ]};
}
function editableProject(project) {
  return {name:project.name || 'My Go protocol', entry:project.entry || '.', files:project.files.map(file => {
    if ((file.encoding || project.encoding) !== 'base64') return {...file,encoding:'utf8'};
    try {
      const bytes=Uint8Array.from(atob(file.content), c=>c.charCodeAt(0));
      const text=new TextDecoder('utf-8',{fatal:true}).decode(bytes);
      if (text.includes('\0')) throw new Error('binary');
      return {path:file.path,content:text,encoding:'utf8'};
    } catch { return {path:file.path,content:file.content,encoding:'base64'}; }
  })};
}
function currentCode() { const projects=codeArchive?.projects || codeDrafts; return projects[codeTarget] || projects.shared; }
function codeWritable() { return !!protocolProject && !codeArchive && !!codeDrafts[codeTarget]; }
function updateSaveState() {
  $('#code-status').hidden=!codeDirty && !codeArchive;
  $('#workspace-save-state').textContent=codeDirty?'Unsaved changes':'Saved';
  $('#workspace-save-state').classList.toggle('unsaved',codeDirty);
  $('#editor-version').textContent=codeArchive?'Historical code · read-only':`Current code · v${protocolProject?.revision || 1}`;
}
function stashDraft() {
  if(!protocolProject || !codeDirty || !codeDrafts.shared)return;
  try{localStorage.setItem(`distvis-protocol-draft:${protocolProject.id}`,JSON.stringify({revision:protocolProject.revision,projects:codeDrafts}));}
  catch{notify('Browser draft storage is full; click Save code to store your edits in the protocol');}
}
function markCodeDirty() {codeDirty=true;codeEdits++;stashDraft();updateSaveState();renderLibrary();$('#code-status').textContent='Unsaved changes · used by later runs of every experiment under this protocol';}
function renderCode() {
  const project=currentCode();if(!project)return;
  const file=project.files.find(f=>f.path===codeFile) || project.files.find(f=>f.path==='main.go') || project.files[0];
  codeFile=file?.path || '';
  const writable=codeWritable(), inherited=codeTarget!=='shared' && !(codeArchive?.projects || codeDrafts)[codeTarget];
  $('#code-target').value=codeTarget;$('#code-entry').value=project.entry;$('#code-entry').disabled=!writable;
  $('#file-count').textContent=project.files.length;
  $('#code-files').innerHTML=[...project.files].sort((a,b)=>a.path.localeCompare(b.path)).map(f=>`<button class="code-file ${f.path===codeFile?'selected':''}" data-file="${esc(f.path)}" aria-current="${f.path===codeFile?'page':'false'}" title="${esc(f.path)}"><small>${f.path.endsWith('.go')?'GO':f.path.endsWith('.proto')?'PB':'◇'}</small><span>${esc(f.path)}</span></button>`).join('');
  $('#code-file-path').textContent=codeFile || 'No files yet';
  $('#code-file-kind').textContent=file?.encoding==='base64'?'Binary · kept as-is':codeArchive?'Historical snapshot':inherited?'Inherits the default project':'Editable';
  $('#source-editor').value=file?.encoding==='base64'?`Binary file: ${file.path}\nThe file is kept and archived unchanged.`:file?.content || '';
  $('#source-editor').readOnly=!writable || !file || file.encoding==='base64';
  $('#code-file-name').value=codeFile;$('#code-file-name').disabled=!writable;
  for(const id of ['add-code-file','rename-code-file','delete-code-file'])$('#'+id).disabled=!writable;
  $('#node-code-copy').hidden=!!codeArchive || codeTarget==='shared' || !inherited;
  $('#node-code-reset').hidden=!!codeArchive || codeTarget==='shared' || inherited;
  for(const id of ['return-code-draft','copy-run-code'])$('#'+id).hidden=!codeArchive;
  $('#save-code').hidden=!!codeArchive;
  $('#workspace-note').textContent=protocolProject.name;
  $('#code-status').textContent=codeArchive?`${codeArchive.simulation?'Go reference code (the model ran this time)':'Historical code'} · ${codeArchive.name} · does not change the current code`:
    inherited?'This node inherits the default code; create a node copy to edit it separately.':codeDirty?'Unsaved changes · used by later runs of every experiment under this protocol':'Saved to the current protocol · every run keeps its own code snapshot';
  updateSaveState();
}
function openCode() { return openProtocol(protocolProject.id,'code'); }
function validFilePath(path) {return path.length<=240 && /^[a-zA-Z0-9_.-]+(?:\/[a-zA-Z0-9_.-]+)*$/.test(path) && !path.split('/').some(p=>['.','..','.git','.codegraph','node_modules','.DS_Store'].includes(p));}
async function saveCode() {
  if(!protocolProject)throw new Error('Open a protocol first');
  if(codeArchive)throw new Error('Go back to the current code first, or continue editing from this version');
  const id=protocolProject.id, edits=codeEdits;
  $('#save-code').disabled=true;
  try{
    const saved=await api(`/api/protocol-projects/${id}`,{revision:protocolProject.revision,projects:codeDrafts});
    if(protocolProject?.id!==id)return;
    protocolProject=saved;
    if(edits===codeEdits){codeDirty=false;localStorage.removeItem(`distvis-protocol-draft:${id}`);}else stashDraft();
    updateSaveState();renderLibrary();$('#code-status').textContent=codeDirty?'Saved the previous version; newer edits remain':'Saved to the current protocol; the next run will use this version';
  }finally{$('#save-code').disabled=false;}
}
$('#code-target').innerHTML += Array.from({length:12},(_,i)=>`<option value="node-${i+1}">node-${i+1} · node project</option>`).join('');
$('#nav-code').onclick=guard(openCode);
$('#code-target').onchange=e=>{codeTarget=e.target.value;codeFile='main.go';renderCode();};
$('#code-files').onclick=e=>{const b=e.target.closest('[data-file]');if(b){codeFile=b.dataset.file;renderCode();}};
$('#source-editor').oninput=e=>{if(!codeWritable())return;const file=currentCode().files.find(f=>f.path===codeFile);if(file?.encoding==='utf8'){file.content=e.target.value;markCodeDirty();}};
$('#source-editor').onkeydown=e=>{if(e.key==='Tab'&&!e.target.readOnly){e.preventDefault();const el=e.target;el.setRangeText('    ',el.selectionStart,el.selectionEnd,'end');el.dispatchEvent(new Event('input',{bubbles:true}));}};
$('#code-entry').oninput=e=>{if(codeWritable()){currentCode().entry=e.target.value;markCodeDirty();}};
$('#node-code-copy').onclick=()=>{codeDrafts[codeTarget]=structuredClone(codeDrafts.shared);markCodeDirty();renderCode();};
$('#node-code-reset').onclick=()=>{delete codeDrafts[codeTarget];markCodeDirty();renderCode();};
$('#add-code-file').onclick=guard(()=>{
  const project=currentCode(),path=$('#code-file-name').value.trim();if(!codeWritable())return;
  if(!validFilePath(path)||project.files.some(f=>f.path===path||f.path.startsWith(path+'/')||path.startsWith(f.path+'/')))throw new Error('The file path is invalid, already exists, or conflicts with a directory');
  project.files.push({path,content:path.endsWith('.go')?'package main\n':'',encoding:'utf8'});codeFile=path;markCodeDirty();renderCode();
});
$('#rename-code-file').onclick=guard(()=>{
  const project=currentCode(),path=$('#code-file-name').value.trim();if(!codeWritable()||!codeFile)return;
  if(!validFilePath(path)||project.files.some(f=>f.path!==codeFile&&(f.path===path||f.path.startsWith(path+'/')||path.startsWith(f.path+'/'))))throw new Error('The file path is invalid or already exists');
  project.files.find(f=>f.path===codeFile).path=path;codeFile=path;markCodeDirty();renderCode();
});
$('#delete-code-file').onclick=()=>{if(codeWritable()){const project=currentCode();project.files=project.files.filter(f=>f.path!==codeFile);markCodeDirty();renderCode();}};
$('#save-code').onclick=guard(async()=>{await saveCode();notify('Protocol code saved');});
$('#return-code-draft').onclick=guard(()=>openProtocol(protocolProject.id,'code'));
$('#copy-run-code').onclick=()=>{codeDrafts=structuredClone(codeArchive.projects);codeArchive=null;markCodeDirty();clearRun();experiment=null;showProtocolView('code');notify('Copied into the protocol draft; once saved, later runs of every experiment below will use it');};
async function showRunCode(id) {
  const record=scopedRuns().find(r=>r.id===id) || (run?.id===id?run:null);
  if(!record || record.config.experimentId!==experiment?.id)throw new Error('This run does not belong to the current experiment');
  const owner=experiment.id, result=await api(`/api/runs/${id}/source`);
  if(experiment?.id!==owner)return;
  const projects=result.project?{shared:editableProject(result.project),...Object.fromEntries(Object.entries(result.nodeProjects || {}).map(([node,p])=>[node,editableProject(p)]))}:
    {shared:legacyProject(result.source || '', 'Legacy single-file project'),...Object.fromEntries(Object.entries(result.nodeSources || {}).map(([node,source])=>[node,legacyProject(source)]))};
  codeArchive={projects,name:record.config.name,simulation:record.config.runtime==='simulation'};codeTarget='shared';codeFile='main.go';viewWorkspace('code');
}
$('#view-run-source').onclick=guard(()=>{if(!run)throw new Error('This experiment has no runs yet');return showRunCode(run.id);});
async function migrateBrowserDraft() {
  if(localStorage.getItem('distvis-draft-imported'))return;
  let projects;
  try{projects=JSON.parse(localStorage.getItem('distvis-projects-v1'));if(!projects?.shared?.files){const old=JSON.parse(localStorage.getItem('distvis-code'));if(old?.shared)projects=Object.fromEntries(Object.entries(old).filter(([,s])=>typeof s==='string'&&s.trim()).map(([node,s])=>[node,legacyProject(s)]));}}catch{return;}
  if(!projects?.shared)return;
  const item=await api('/api/protocol-projects',{name:'Imported code draft',protocol:'custom',projects});
  localStorage.setItem('distvis-draft-imported',item.id);
}
async function migrateExperimentDrafts() {
  // Preserve old per-experiment edits separately: siblings may have conflicting drafts.
  for(const child of experiments){
    const marker=`distvis-draft-migrated:${child.id}`;
    if(localStorage.getItem(marker))continue;
    let draft;
    try{draft=JSON.parse(localStorage.getItem(`distvis-draft:${child.id}`));}catch{continue;}
    if(!draft?.projects?.shared?.files)continue;
    const recovered=await api('/api/protocol-projects',{name:`Recovered draft · ${child.name}`.slice(0,100),protocol:child.protocol,projects:draft.projects});
    localStorage.setItem(marker,recovered.id);
  }
  for(const parent of protocolProjects)for(const oldId of parent.mergedIds || []){
    const marker=`distvis-merged-draft:${oldId}`;
    if(localStorage.getItem(marker))continue;
    let draft;
    try{draft=JSON.parse(localStorage.getItem(`distvis-protocol-draft:${oldId}`));}catch{continue;}
    if(!draft?.projects?.shared?.files)continue;
    const recovered=await api('/api/protocol-projects',{name:`Recovered draft · ${parent.name}`.slice(0,100),protocol:parent.protocol,projects:draft.projects});
    localStorage.setItem(marker,recovered.id);
  }
}
function parentCard(p){return `<button class="experiment-card" data-protocol-project="${esc(p.id)}"><span class="item-icon">${icon('code')}</span><span class="item-main"><strong>${esc(p.name)}</strong><small>${p.experimentCount} experiments · ${p.runCount} runs</small></span><span class="item-arrow" aria-hidden="true">→</span></button>`;}
function renderProtocolShell(){
  const child=!!experiment;
  $('#workspace-more').open=false;
  $('#archive-workspace').textContent=`${(experiment || protocolProject).archived?'Restore':'Archive'}${child?' experiment':' protocol'}`;
  $('#experiment-library').hidden=true;$('#experiment-workbench').hidden=false;
  $('#protocol-tabs').hidden=child;$('#experiment-tabs').hidden=!child;
  if(child)$('#history-count').textContent=scopedRuns().length;
  $('#workspace-title').textContent=child?experiment.name:protocolProject.name;
  $('#workspace-description').textContent=child?`${experiment.settings.nodeCount} nodes · ${experiment.settings.latency}${jitterText(experiment.settings.delayModel, experiment.settings.jitter)} ms · ${experiment.settings.runtime}`:'';
  $('#breadcrumb-protocol').hidden=false;$('#breadcrumb-protocol').textContent=protocolProject.name;
  $('#breadcrumb-separator').hidden=false;$('#breadcrumb-child-separator').hidden=!child;$('#breadcrumb-experiment').textContent=child?experiment.name:'';
  $('#workspace-new-experiment').hidden=child;
  $('#workspace-save-state').hidden=child;$('#nav-library').classList.remove('active');
  $('#protocol-experiment-count').textContent=experiments.filter(e=>e.protocolId===protocolProject.id).length;
}
function renderChildren(){
  const all=experiments.filter(e=>e.protocolId===protocolProject?.id);
  const children=all.filter(e=>!e.archived || protocolProject.archived), archived=all.filter(e=>e.archived && !protocolProject.archived);
  $('#archived-child-experiments').hidden=!archived.length;
  $('#archived-child-count').textContent=archived.length;
  $('#archived-child-cards').innerHTML=archived.map(experimentCard).join('');
  $('#protocol-experiment-count').textContent=children.length;
  $('#child-experiment-cards').innerHTML=children.map(experimentCard).join('') || '<div class="library-empty"><h3>Create the first experiment for this protocol</h3><p>For example: healthy network, slow network, different node counts. Each experiment keeps its own execution records.</p></div>';
}
function showProtocolView(view){
  if(!protocolProject)return;
  workspaceView=view;setVisualMode(false);renderProtocolShell();
  $('#code-dialog').hidden=view!=='code';$('#protocol-experiments-panel').hidden=view!=='experiments';$('#visual-panel').hidden=true;$('#history-panel').hidden=true;
  $('#protocol-code-tab').classList.toggle('selected',view==='code');$('#protocol-experiments-tab').classList.toggle('selected',view==='experiments');
  window.history?.replaceState(null,'',`#protocol/${protocolProject.id}/${view}`);
  localStorage.setItem('distvis-last-protocol',protocolProject.id);localStorage.setItem('distvis-last-experiment','');
  if(view==='code')renderCode();else renderChildren();
}
function loadProtocolCode(parent){
  protocolProject=parent;codeDrafts=Object.fromEntries(Object.entries(parent.projects).map(([node,p])=>[node,editableProject(p)]));
  codeDirty=false;codeArchive=null;codeTarget='shared';codeFile='main.go';
  try{const draft=JSON.parse(localStorage.getItem(`distvis-protocol-draft:${parent.id}`));if(draft?.revision===parent.revision){codeDrafts=draft.projects;codeDirty=true;}}catch{}
  updateSaveState();
}
async function openProtocol(id,view='experiments'){
  stashDraft();const ticket=++loadingExperiment;
  const parent=await api(`/api/protocol-projects/${id}`);if(ticket!==loadingExperiment)return;
  clearRun();experiment=null;runs=[];loadProtocolCode(parent);await refreshRuns();
  if(ticket!==loadingExperiment)return;showProtocolView(view);renderLibrary();
}
$('#archive-workspace').onclick=guard(async()=>{
  const item=experiment || protocolProject, archived=!item.archived;
  const kind=experiment?'experiments':'protocol-projects';
  const saved=await api(`/api/${kind}/${item.id}`,{revision:item.revision,archived});
  if(kind==='protocol-projects'){protocolProject=saved;stashDraft();}
  if(kind==='experiments')await openExperiment(item.id);else await openProtocol(item.id);
  notify(archived?'Archived; code and history are kept and can be restored anytime':'Restored to the normal list');
});
// The mean only applies to the exponential model; a fixed model keeps it at 0.
function syncJitterField(model,jitter,changed=false){
  const exp=model.value==='exponential';
  jitter.disabled=!exp;
  if(!exp)jitter.value=0;else if(changed && !(Number(jitter.value)>0))jitter.value=50;
}
$('#new-form').elements.delayModel.addEventListener('change',e=>syncJitterField(e.target,$('#new-form').elements.jitter,true));
$('#link-model').addEventListener('change',e=>{syncJitterField(e.target,$('#link-jitter'),true);saveLinkDraft();});
async function newChild(){
  if(!protocolProject)return;
  runFormMode='create';const form=$('#new-form');
  for(const [k,v] of Object.entries({name:`Experiment ${experiments.filter(e=>e.protocolId===protocolProject.id).length+1}`,runtime:'docker',nodeCount:5,latency:80,bandwidth:128,delayModel:'fixed',jitter:0,seed:42}))form.elements[k].value=v;
  fillNetworkForm(form);
  $('#run-dialog-title').textContent='New experiment under this protocol';$('#run-name-label').textContent='Experiment name';$('#run-project-name').textContent=protocolProject.name;
  $('#run-submit').textContent='Create experiment →';$('#run-dialog-note').textContent='Saves the settings; run it and inject faults afterwards';$('#save-experiment-settings').hidden=true;
  form.elements.runtime.querySelector('[value="simulation"]').disabled=!['raft','token','gossip'].includes(protocolProject.protocol);
  syncJitterField(form.elements.delayModel,form.elements.jitter);
  $('#new-dialog').showModal();
}
$('#protocol-code-tab').onclick=()=>{codeArchive=null;showProtocolView('code');};
$('#protocol-experiments-tab').onclick=()=>showProtocolView('experiments');
$('#workspace-new-experiment').onclick=guard(newChild);
$('#breadcrumb-protocol').onclick=guard(()=>openProtocol(protocolProject.id));
$('#archived-child-cards').onclick=$('#child-experiment-cards').onclick=guard(e=>{const b=e.target.closest('[data-experiment]');if(b)return openExperiment(b.dataset.experiment);});
$('#save-experiment-settings').onclick=guard(async()=>{
  const form=$('#new-form'),settings={...Object.fromEntries(['runtime','nodeCount','latency','bandwidth','seed','delayModel','jitter'].map(k=>[k,form.elements[k].value])),network:networkFromForm(form)};
  experiment=await api(`/api/experiments/${experiment.id}`,{revision:experiment.revision,settings});renderProtocolShell();$('#new-dialog').close();await refreshRuns();notify("Saved this experiment's settings");
});
function frame(now) {
  const elapsed = Math.min(now - (lastFrame || now), 200); lastFrame = now;
  if (playing && !$('#event-dialog').open) {
    playTime = Math.min(latestTime, playTime + elapsed * speed);
    while (cursor < events.length && events[cursor].time <= playTime) cursor++;
    if (playTime >= latestTime && run?.status !== 'running') playing = false;
    renderDirty = true;
  } else if (live && run) {
    cursor = events.length; playTime = Math.max(playTime, latestTime);
  }
  if (now - lastPaint >= 33) { render(); lastPaint = now; }
  requestAnimationFrame(frame);
}
async function restoreRoute() {
  const parentLink=(location.hash || '').match(/^#protocol\/([a-f0-9-]+)\/(code|experiments)$/);
  const link=(location.hash || '').match(/^#experiment\/([a-f0-9-]+)\/(code|visual|history)$/);
  const id=link?.[1] || (parentLink || location.hash==='#library'?null:localStorage.getItem('distvis-last-experiment'));
  if(id && experiments.some(e=>e.id===id))await openExperiment(id,link?.[2]);
  else if(parentLink || (location.hash!=='#library' && localStorage.getItem('distvis-last-protocol'))){const pid=parentLink?.[1] || localStorage.getItem('distvis-last-protocol');try{await openProtocol(pid,parentLink?.[2]);}catch{await showLibrary();}}
  else await showLibrary();
  render();
}
window.addEventListener('hashchange',guard(restoreRoute));
// The visualization hides navigation into a temporary drawer; the saved sidebar preference
// only governs the other views and is never written from here.
function updateSidebarToggle(){
  const visual=document.body.classList.contains('visual-mode');
  const expanded=visual?drawerOpen:!document.body.classList.contains('sidebar-collapsed');
  const label=visual?(drawerOpen?'Hide navigation':'Show navigation'):(expanded?'Collapse sidebar':'Expand sidebar');
  $('#toggle-sidebar').setAttribute('aria-expanded',String(expanded));
  $('#toggle-sidebar').setAttribute('aria-label',label);$('#toggle-sidebar').title=label;
}
function setSidebar(collapsed){document.body.classList.toggle('sidebar-collapsed',collapsed);updateSidebarToggle();}
function setDrawer(open){drawerOpen=open;document.body.classList.toggle('drawer-open',open);updateSidebarToggle();}
function setVisualMode(on){document.body.classList.toggle('visual-mode',on);setDrawer(false);}
setSidebar(localStorage.getItem('distvis-sidebar-collapsed')==='true');
$('#toggle-sidebar').onclick=()=>{
  if(document.body.classList.contains('visual-mode')){setDrawer(!drawerOpen);if(drawerOpen)$('#nav-library').focus();return;}
  const collapsed=!document.body.classList.contains('sidebar-collapsed');setSidebar(collapsed);localStorage.setItem('distvis-sidebar-collapsed',String(collapsed));
};
document.addEventListener('click',e=>{if(drawerOpen&&!e.target.closest('.sidebar, #toggle-sidebar'))setDrawer(false);});
// Escape closes the innermost layer: menus, then the drawer, then link mode, then the popover.
// Open modal dialogs handle Escape themselves.
document.addEventListener('keydown',e=>{
  if(e.key!=='Escape'||$$('dialog').some(d=>d.open))return;
  const menus=$$('.more-menu[open], .legend-menu[open]');
  if(menus.length){menus.forEach(menu=>{menu.open=false;});return;}
  if(drawerOpen){setDrawer(false);$('#toggle-sidebar').focus();return;}
  if(!popoverOpen)return;
  if(linkSel)backToOriginNode();else closePopover();
});
async function init() {
  await refreshProtocols();await migrateBrowserDraft();await refreshRuns();
  await migrateExperimentDrafts();await refreshRuns();await restoreRoute();
  setInterval(()=>refreshRuns().catch(()=>{$('#connection-status').textContent='Disconnected';}),1500);
}
guard(init)();
requestAnimationFrame(frame);
