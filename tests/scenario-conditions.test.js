import test from 'node:test';
import assert from 'node:assert/strict';
import { resolveValues, validateResult } from '../scripts/scenario-conditions.mjs';

test('comparisons use actual node timestamps, not hard-coded expected values', () => {
  const states = { 'node-1': { events: [{ timestamp: { lamport: 7, vector: [2, 3, 0] } }] } };
  const value = resolveValues({ left: { $state: { node: 'node-1', path: 'events.0.timestamp' } } }, states);
  assert.equal(value.left.lamport, 7);
  value.left.vector[0] = 99;
  assert.equal(states['node-1'].events[0].timestamp.vector[0], 2);
  assert.throws(() => resolveValues({ $state: { node: 'node-2', path: 'clock' } }, states), /Missing/);
});

test('timeout expectation rejects both successful and failed early responses', () => {
  assert.throws(() => validateResult('timeout', { result: 1 }), /response arrived/);
  assert.throws(() => validateResult('timeout', { error: 'failed' }), /response arrived/);
  assert.throws(() => validateResult(undefined, { error: 'failed' }), /Expected success/);
  assert.throws(() => validateResult('error', {}), /Expected an error/);
  validateResult(undefined, {});
  validateResult('error', { error: 'expected' });
});
