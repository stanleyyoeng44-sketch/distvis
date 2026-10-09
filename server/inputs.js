// Generic input form contract. Protocol names and their actions do not belong here.
const identifier = /^[a-zA-Z][a-zA-Z0-9_-]{0,63}$/;
// node / nodes hold node IDs of the current run; the browser renders them as dropdowns.
const types = ['text', 'number', 'boolean', 'select', 'json', 'node', 'nodes'];
const plain = v => v !== null && typeof v === 'object' && !Array.isArray(v);
export function validateSchema(schema) {
  if (!Array.isArray(schema) || schema.length > 64 || Buffer.byteLength(JSON.stringify(schema)) > 49152) throw new Error('输入声明应为最多 64 个动作的数组（48 KiB 内）');
  const actions = new Set();
  for (const s of schema) {
    if (!plain(s) || typeof s.action !== 'string' || !identifier.test(s.action) || actions.has(s.action) || typeof s.label !== 'string' || s.label.length > 100
      || !Array.isArray(s.fields) || s.fields.length > 16 || (s.description !== undefined && (typeof s.description !== 'string' || s.description.length > 1000))) throw new Error('输入动作声明不合法');
    actions.add(s.action);
    const names = new Set();
    for (const f of s.fields) {
      if (!plain(f) || typeof f.name !== 'string' || !identifier.test(f.name) || ['__proto__', 'constructor', 'prototype'].includes(f.name) || names.has(f.name)
        || typeof f.label !== 'string' || f.label.length > 100 || !types.includes(f.type)
        || (f.required !== undefined && typeof f.required !== 'boolean')
        || (f.integer !== undefined && typeof f.integer !== 'boolean')) throw new Error('输入字段声明不合法');
      names.add(f.name);
      for (const k of ['min', 'max']) if (f[k] !== undefined && (typeof f[k] !== 'number' || !Number.isFinite(f[k]))) throw new Error('数字范围不合法');
      if (f.min !== undefined && f.max !== undefined && f.min > f.max) throw new Error('数字范围不合法');
      if (f.maxLength !== undefined && (!Number.isInteger(f.maxLength) || f.maxLength < 1 || f.maxLength > 8192)) throw new Error('文本长度限制不合法');
      if (f.type === 'select' && (!Array.isArray(f.options) || !f.options.length || f.options.length > 32 || f.options.some(o => typeof o !== 'string' || o.length > 100))) throw new Error('选项声明不合法');
    }
  }
}
export function validateValues(schema, values, nodes = []) {
  if (!plain(values) || Buffer.byteLength(JSON.stringify(values)) > 8192) throw new Error('输入应为 JSON 对象，最多 8 KiB');
  for (const name of Object.keys(values)) if (!schema.fields.some(f => f.name === name)) throw new Error(`未知字段 ${name}`);
  for (const f of schema.fields) {
    const v = values[f.name];
    if (v === undefined) { if (f.required) throw new Error(`请填写 ${f.label}`); continue; }
    if (f.type === 'text' || f.type === 'select') {
      if (typeof v !== 'string' || v.length > (f.maxLength || 8192) || (f.required && !v.length)) throw new Error(`${f.label} 文本不合法`);
      if (f.type === 'select' && !f.options.includes(v)) throw new Error(`${f.label} 选项不合法`);
    }
    if (f.type === 'number' && (typeof v !== 'number' || !Number.isFinite(v) || (f.integer && !Number.isSafeInteger(v)) || (f.min !== undefined && v < f.min) || (f.max !== undefined && v > f.max))) throw new Error(`${f.label} 数字不合法`);
    if (f.type === 'boolean' && typeof v !== 'boolean') throw new Error(`${f.label} 应为布尔值`);
    if (f.type === 'node' && (typeof v !== 'string' || (v === '' ? f.required : !nodes.includes(v)))) throw new Error(`${f.label} 应为本次运行中的节点`);
    if (f.type === 'nodes' && (!Array.isArray(v) || (f.required && !v.length) || new Set(v).size !== v.length || v.some(n => !nodes.includes(n)))) throw new Error(`${f.label} 应为本次运行中不重复的节点列表`);
  }
}
