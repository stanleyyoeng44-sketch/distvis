import { appendFileSync } from 'node:fs';

// Large automated lab traces may produce hundreds of thousands of records.
// Preserve every event and its order, but amortize filesystem calls. A normal
// interactive run keeps its existing synchronous publication semantics.
export function bufferedEventLog(path, { interval = 50, batchSize = 512 } = {}) {
  let records = [];
  let closed = false;
  const flush = () => {
    if (!records.length) return;
    appendFileSync(path, records.join(''));
    records = [];
  };
  const timer = setInterval(flush, interval);
  timer.unref();
  return {
    record(event) { records.push(JSON.stringify(event) + '\n'); if (closed || records.length >= batchSize) flush(); },
    flush,
    close() { closed = true; clearInterval(timer); flush(); },
  };
}
