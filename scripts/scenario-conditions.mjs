// Pure helpers shared by the runner and its regression tests.
export function get(obj, path) {
  if (!path) return obj;
  return path.split('.').reduce((v, k) => v == null ? undefined : v[k], obj);
}

export function resolveValues(value, states) {
  if (Array.isArray(value)) return value.map(v => resolveValues(v, states));
  if (!value || typeof value !== 'object') return value;
  if (Object.hasOwn(value, '$state')) {
    const { node, path } = value.$state;
    const actual = get(states[node], path);
    if (actual === undefined) throw new Error(`Missing input reference: ${node}.${path}`);
    return structuredClone(actual);
  }
  return Object.fromEntries(Object.entries(value).map(([k, v]) => [k, resolveValues(v, states)]));
}

export function validateResult(expect = 'ok', result) {
  if (!['ok', 'error', 'timeout'].includes(expect)) throw new Error(`Unknown expectation: ${expect}`);
  if (expect === 'timeout') throw new Error('Expected no response before timeout, but a response arrived');
  if (expect === 'ok' && result.error) throw new Error(`Expected success: ${result.error}`);
  if (expect === 'error' && !result.error) throw new Error('Expected an error, but operation succeeded');
}
