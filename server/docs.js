import {readFileSync} from 'node:fs';
import {fileURLToPath} from 'node:url';

const pages={go:{title:'Go API',file:'GO-API.md',intro:'编写和运行节点程序'},platform:{title:'平台 HTTP API',file:'PLATFORM-API.md',intro:'自动化实验与读取事件'},agent:{title:'Agent 接入',file:'AGENT.md',intro:'MCP 与协议开发 skill'}};
const escape=s=>String(s).replace(/[&<>"']/g,c=>({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
function inline(s){
  // No embedded HTML. Only known inline forms and local/HTTPS links are allowed.
  return s.split(/(`[^`]+`|\[[^\]]+\]\([^)]+\))/g).map(part=>{
    if(part.startsWith('`'))return `<code>${escape(part.slice(1,-1))}</code>`;
    const link=part.match(/^\[([^\]]+)\]\(([^)]+)\)$/);
    if(link && /^(?:\/(?!\/)|#|https:\/\/)/.test(link[2]))return `<a href="${escape(link[2])}">${escape(link[1])}</a>`;
    return escape(part);
  }).join('');
}
export function renderMarkdown(markdown){
  const lines=markdown.split('\n'),out=[],toc=[],ids=new Map();let section=false;
  for(let i=0;i<lines.length;){
    const line=lines[i];
    if(!line.trim()){i++;continue;}
    const fence=line.match(/^```([\w.-]*)/);
    if(fence){const code=[];i++;while(i<lines.length && !lines[i].startsWith('```'))code.push(lines[i++]);i++;out.push(`<div class="code-block"><div class="code-bar"><span>${escape(fence[1] || 'text')}</span><button type="button" class="copy-code" aria-label="复制代码">复制</button></div><pre><code>${escape(code.join('\n'))}</code></pre></div>`);continue;}
    const heading=line.match(/^(#{1,3}) (.+)$/);
    if(heading){const level=heading[1].length,title=heading[2];
      const base=title.toLowerCase().replace(/[^\p{L}\p{N}]+/gu,'-').replace(/^-|-$/g,''),count=ids.get(base)||0;ids.set(base,count+1);const id=base+(count?'-'+count:'');
      if(level===2){if(section)out.push('</section>');out.push(`<section class="doc-section" data-section="${escape(id)}">`);section=true;toc.push({id,title});}
      out.push(`<h${level} id="${escape(id)}">${inline(title)}</h${level}>`);i++;continue;
    }
    if(line.startsWith('|') && /^\|[\s:|-]+\|\s*$/.test(lines[i+1] || '')){
      const cells=s=>s.trim().slice(1,-1).split('|').map(c=>c.trim());
      const heads=cells(line);i+=2;const rows=[];while(i<lines.length && lines[i].startsWith('|'))rows.push(cells(lines[i++]));
      out.push(`<div class="doc-table" tabindex="0" role="region" aria-label="API 参数表，可横向滚动"><table><thead><tr>${heads.map(c=>`<th>${inline(c)}</th>`).join('')}</tr></thead><tbody>${rows.map(row=>`<tr>${row.map(c=>`<td>${inline(c)}</td>`).join('')}</tr>`).join('')}</tbody></table></div>`);continue;
    }
    if(line.startsWith('- ')){const items=[];while(i<lines.length && lines[i].startsWith('- '))items.push(lines[i++].slice(2));out.push(`<ul>${items.map(v=>`<li>${inline(v)}</li>`).join('')}</ul>`);continue;}
    const paragraph=[line];i++;while(i<lines.length && lines[i].trim() && !/^(?:#|```|\||- )/.test(lines[i]))paragraph.push(lines[i++]);out.push(`<p>${inline(paragraph.join(' '))}</p>`);
  }
  if(section)out.push('</section>');
  return {html:out.join('\n'),toc};
}
export function documentation(path){
  if(path==='/docs/distvis-protocol/SKILL.md')return {type:'text/plain; charset=utf-8',body:readFileSync(fileURLToPath(new URL('../skills/distvis-protocol/SKILL.md',import.meta.url)),'utf8')};
  const raw=path.match(/^\/docs\/(go|platform|agent)\.md$/);
  if(raw)return {type:'text/plain; charset=utf-8',body:readFileSync(fileURLToPath(new URL('../docs/'+pages[raw[1]].file,import.meta.url)),'utf8')};
  const sample=path.match(/^\/docs\/examples\/ping\/(main\.go|go\.mod)$/);
  if(sample)return {type:'text/plain; charset=utf-8',body:readFileSync(fileURLToPath(new URL('../docs/examples/ping/'+sample[1],import.meta.url)),'utf8')};
  const key=path==='/docs' || path==='/docs/'?'go':path.match(/^\/docs\/(go|platform|agent)$/)?.[1];
  if(!key)return null;
  const page=pages[key],markdown=readFileSync(fileURLToPath(new URL('../docs/'+page.file,import.meta.url)),'utf8');
  const {html,toc}=renderMarkdown(markdown);
  return {type:'text/html; charset=utf-8',body:`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><title>${page.title} · DistVis</title><link rel="stylesheet" href="/docs.css"><script src="/docs.js" defer></script></head>
<body><a class="skip-link" href="#doc-main">跳到正文</a><header class="docs-header"><a class="docs-brand" href="/">DistVis</a><span>API 文档</span><a class="back-link" href="/">返回工作台 →</a></header>
<div class="docs-layout"><aside class="docs-sidebar"><nav aria-label="文档类别">${Object.entries(pages).map(([id,p])=>`<a href="/docs/${id}" ${id===key?'aria-current="page"':''}>${p.title}<small>${p.intro}</small></a>`).join('')}</nav><label class="search-label" for="docs-search">搜索本文</label><input type="search" id="docs-search" placeholder="方法名、路径或关键词…"><details id="docs-toc" open><summary>本文目录</summary><nav aria-label="本文目录">${toc.map(t=>`<a href="#${encodeURIComponent(t.id)}" data-section-link="${escape(t.id)}">${escape(t.title)}</a>`).join('')}</nav></details><a class="download-doc" href="/docs/${key}.md" download="${page.file}">下载 Markdown ↓</a></aside>
<main id="doc-main"><div class="doc-meta">DistVis · 开发者参考</div><p id="docs-no-results" role="status" hidden>没有匹配的内容，请尝试其他关键词。</p><article>${html}</article></main></div><div id="docs-status" role="status" aria-live="polite"></div></body></html>`};
}
