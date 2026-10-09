import { readdirSync, readFileSync, writeFileSync, mkdirSync, existsSync, realpathSync, lstatSync } from 'node:fs';
import { join, resolve, basename } from 'node:path';
import { createHash } from 'node:crypto';
import { protocolMetadata } from './metadata.js';

const ignored = new Set(['.git', '.codegraph', 'node_modules', '.DS_Store', 'vendor']);
export function readProtocolMetadata(directory) {
  const files=[];
  let bytes=0;
  function visit(path, prefix=''){
    for(const item of readdirSync(path,{withFileTypes:true})){
      if(ignored.has(item.name) || item.isSymbolicLink())continue;
      const rel=prefix+item.name, full=join(path,item.name);
      if(item.isDirectory()){visit(full,rel+'/');continue;}
      if(!item.isFile() || !item.name.endsWith('.go') || item.name.endsWith('_test.go'))continue;
      bytes+=lstatSync(full).size;
      if(bytes>32*1024*1024 || files.length>=5000)throw new Error('协议 Go 源码超过 32 MiB 或 5000 文件');
      files.push({path:rel,content:readFileSync(full,'utf8')});
    }
  }
  visit(directory);
  const manifest=join(directory,'distvis.json');
  return protocolMetadata(files,existsSync(manifest)?JSON.parse(readFileSync(manifest,'utf8')):{});
}
export class ProtocolLibrary {
  constructor(dataDir, defaultRoot) {
    this.settings = join(dataDir, 'library.json');
    let saved;
    try { saved = JSON.parse(readFileSync(this.settings, 'utf8')).root; } catch {}
    this.root = resolve(process.env.DISTVIS_PROTOCOL_DIR || saved || defaultRoot);
  }
  setRoot(path) {
    if (typeof path !== 'string' || !path.trim()) throw new Error('请输入协议根目录的绝对路径');
    const root = realpathSync(resolve(path));
    if (!lstatSync(root).isDirectory()) throw new Error('协议根目录不是目录');
    writeFileSync(this.settings, JSON.stringify({ root }, null, 2));
    this.root = root;
    return this.discover();
  }
  discover() {
    const entries = [], errors = [];
    if (!existsSync(this.root)) return { root: this.root, entries, errors: ['协议根目录不存在，请设置已有目录'] };
    for (const dir of readdirSync(this.root, { withFileTypes: true }).sort((a,b)=>a.name.localeCompare(b.name))) {
      if (!dir.isDirectory() || dir.name.startsWith('.')) continue;
      const directory = join(this.root, dir.name), id = `directory:${encodeURIComponent(dir.name)}`;
      try {
        if (!existsSync(join(directory, 'go.mod'))) throw new Error('缺少 go.mod');
        const manifest = readProtocolMetadata(directory), entry=manifest.entry;
        entries.push({ id, directory, folder: dir.name, entry, name: manifest.name || dir.name, description: manifest.description || '本地 Go 协议项目 · 自动发现 RPC 应用接口', ...(manifest.start ? { start: manifest.start } : {}), source: 'directory' });
      } catch (e) { errors.push(`${dir.name}: ${e.message}`); }
    }
    return { root: this.root, entries, errors };
  }
  select(id) {
    const project = this.discover().entries.find(p => p.id === id);
    if (!project) throw new Error('协议项目不存在或无效，请刷新协议目录');
    return project;
  }
}

// Freeze precisely the files compiled in this run; later edits cannot mutate history.
// Both original root and this snapshot are mounted read-only in project nodes.
export function snapshotProject(project, destination) {
  let bytes = 0;
  const files = [];
  const visit = (path, rel = '') => {
    for (const entry of readdirSync(path, { withFileTypes: true }).sort((a,b)=>a.name.localeCompare(b.name))) {
      if (ignored.has(entry.name)) continue;
      const name = rel ? `${rel}/${entry.name}` : entry.name, full = join(path, entry.name);
      if (entry.isSymbolicLink()) throw new Error(`协议目录包含符号链接 ${name}，请将依赖文件放入项目内或使用 Go module`);
      if (entry.isDirectory()) { visit(full, name); continue; }
      if (!entry.isFile()) throw new Error(`不支持的项目文件 ${name}`);
      const size = lstatSync(full).size;
      bytes += size;
      if (bytes > 32 * 1024 * 1024 || files.length >= 5000) throw new Error('协议项目超过 32 MiB 或 5000 文件');
      const content = readFileSync(full);
      files.push({ path: name, sha256: createHash('sha256').update(content).digest('hex'), size:content.length, content:content.toString('base64') });
    }
  };
  visit(project.directory);
  const sha256 = createHash('sha256').update(JSON.stringify(files.map(({path,sha256})=>({path,sha256})))).digest('hex');
  if (destination) for (const file of files) {
    const path = join(destination, file.path);
    mkdirSync(resolve(path, '..'), { recursive: true });
    writeFileSync(path, Buffer.from(file.content, 'base64'));
  }
  return { ...project, sha256, files, totalBytes:bytes, encoding:'base64' };
}

// Browser workspaces use the same immutable archive and compiler as directories.
// Paths and encodings are validated before any file is written.
export function snapshotWorkspace(input, destination) {
  if (!input || !Array.isArray(input.files) || !input.files.length || input.files.length > 5000) throw new Error('项目需要 1–5000 个文件');
  let totalBytes = 0;
  const paths = new Set();
  const files = input.files.map(file => {
    const path = file?.path;
    if (typeof path !== 'string' || path.length > 240 || !/^[a-zA-Z0-9_.-]+(?:\/[a-zA-Z0-9_.-]+)*$/.test(path) ||
        path.split('/').some(p => p === '.' || p === '..' || ignored.has(p)) || paths.has(path)) throw new Error(`无效或重复的文件路径：${path}`);
    paths.add(path);
    if (typeof file.content !== 'string' || !['utf8','base64'].includes(file.encoding || 'utf8')) throw new Error(`文件编码不合法：${path}`);
    // Use a simple O(n) character check instead of the group-repeating regex that
    // catastrophically backtracks on large strings, causing a V8 stack overflow.
    if (file.encoding === 'base64' && !/^[A-Za-z0-9+/]*={0,2}$/.test(file.content)) throw new Error(`无效 base64：${path}`);
    const bytes = Buffer.from(file.content, file.encoding || 'utf8');
    totalBytes += bytes.length;
    if (totalBytes > 32 * 1024 * 1024) throw new Error('项目超过 32 MiB');
    return {path, content:bytes.toString('base64'), size:bytes.length, sha256:createHash('sha256').update(bytes).digest('hex')};
  }).sort((a,b)=>a.path.localeCompare(b.path));
  for (const path of paths) {
    const parts = path.split('/');
    while (parts.length > 1) { parts.pop(); if (paths.has(parts.join('/'))) throw new Error('文件与目录路径冲突'); }
  }
  if (!paths.has('go.mod')) throw new Error('项目缺少 go.mod');
  let manifest = {};
  const meta = files.find(f=>f.path==='distvis.json');
  if (meta) manifest = JSON.parse(Buffer.from(meta.content,'base64').toString('utf8'));
  const metadata=protocolMetadata(files.map(f=>({path:f.path,content:Buffer.from(f.content,'base64').toString('utf8')})),manifest,input.entry);
  const entry=metadata.entry;
  const sha256 = createHash('sha256').update(JSON.stringify(files.map(({path,sha256})=>({path,sha256})))).digest('hex');
  if (destination) for (const file of files) {
    const path = join(destination, file.path);
    mkdirSync(resolve(path, '..'), {recursive:true});
    writeFileSync(path, Buffer.from(file.content,'base64'));
  }
  return {name:String(input.name || metadata.name || '编辑器项目').slice(0,100),description:metadata.description || '', entry, ...(metadata.start?{start:metadata.start}:{}), source:'workspace', encoding:'base64', files, totalBytes, sha256};
}

// Docker Desktop exposes shared macOS files under /host_mnt in its K8s VM.
// For other installations the operator can set a VM-side prefix explicitly.
export function k8sHostPath(path, env = process.env, platform = process.platform) {
  const absolute = resolve(path);
  if (env.DISTVIS_K8S_HOST_PREFIX) return join(env.DISTVIS_K8S_HOST_PREFIX, absolute);
  return platform === 'darwin' ? join('/host_mnt', absolute) : absolute;
}
