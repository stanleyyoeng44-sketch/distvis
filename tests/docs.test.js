import test from 'node:test';
import assert from 'node:assert/strict';
import {renderMarkdown} from '../server/docs.js';

test('documentation escapes HTML and produces safe links and unique navigable headings', () => {
  const {html, toc} = renderMarkdown(`# API\n\n## 方法\n\n<script>alert(1)</script> [bad](javascript:alert) [external](//example.com) [Go](/docs/go)\n\n\`\`\`go\nfmt.Println("<tag>")\n\`\`\`\n\n## 方法\n\n| 参数 | 类型 |\n| --- | --- |\n| \`name\` | string |\n`);
  assert.ok(!html.includes('<script>'));
  assert.ok(!html.includes('href="javascript:'));
  assert.ok(!html.includes('href="//'));
  assert.ok(html.includes('href="/docs/go"'));
  assert.ok(html.includes('&lt;tag&gt;'));
  assert.deepEqual(toc.map(t => t.id), ['方法', '方法-1']);
  for (const {id} of toc) assert.ok(html.includes(`id="${id}"`));
  assert.ok(html.includes('<td><code>name</code></td>'));
});
