// Optional project metadata lives in one Go file's leading comments.
// Only comments before "package main" are inspected: strings and function bodies
// cannot accidentally change the build entry or the protocol description.
function header(source) {
  let rest=source.replace(/^\uFEFF/,'');
  const comments=[];
  for(;;){
    rest=rest.trimStart();
    if(rest.startsWith('//')){
      const end=rest.indexOf('\n');
      comments.push(rest.slice(2,end<0?undefined:end));
      rest=end<0?'':rest.slice(end+1);
    }else if(rest.startsWith('/*')){
      const end=rest.indexOf('*/',2);
      if(end<0)throw new Error('Unclosed comment in a Go file header');
      comments.push(rest.slice(2,end));rest=rest.slice(end+2);
    }else break;
  }
  return /^\s*package\s+main\b/.test(rest)?comments:null;
}

// distvis:start tells a protocol's users where message traffic begins when nodes stay silent
// after start: "node-2 Service.Method {JSON arguments} note". The UI shows it without knowing the protocol.
function parseStart(text){
  const m=text.match(/^(\S+)\s+(\S+)\s*([\s\S]*)$/);
  if(!m)throw new Error('distvis:start format: node-2 Service.Method {"arg":value} description');
  let rest=m[3].trim(), values={};
  if(rest.startsWith('{')){
    let depth=0, quoted=false, escaped=false, end=-1;
    for(let i=0;i<rest.length && end<0;i++){
      const c=rest[i];
      if(quoted){if(escaped)escaped=false;else if(c==='\\')escaped=true;else if(c==='"')quoted=false;}
      else if(c==='"')quoted=true;
      else if(c==='{')depth++;
      else if(c==='}' && --depth===0)end=i;
    }
    try{if(end<0)throw new Error();values=JSON.parse(rest.slice(0,end+1));}catch{throw new Error('distvis:start arguments are not a valid JSON object');}
    rest=rest.slice(end+1).trim();
  }
  return {node:m[1],method:m[2],values,note:rest.replace(/^[—–:：-]\s*/,'')};
}
function validStart(start){
  const values=start?.values ?? {};
  const ok=start && typeof start==='object' && !Array.isArray(start)
    && /^node-([1-9]|1[0-2])$/.test(start.node)
    && /^(?:[A-Za-z_]\w*\.[A-Za-z_]\w*|\/[A-Za-z_][\w.]*\/[A-Za-z_]\w*)$/.test(start.method)
    && values && typeof values==='object' && !Array.isArray(values) && JSON.stringify(values).length<=2048
    && (start.note===undefined || (typeof start.note==='string' && start.note.length<=300));
  if(!ok)throw new Error('Invalid distvis:start: the node must be node-1…node-12, the method must be Service.Method or /pkg.Service/Method, the arguments must be a JSON object, and the description is limited to 300 characters');
  return {node:start.node,method:start.method,values,...(start.note?{note:start.note}:{})};
}

export function protocolMetadata(files, legacy={}, explicitEntry) {
  if(!legacy || typeof legacy!=='object' || Array.isArray(legacy))throw new Error('Protocol metadata must be an object');
  const annotations={}, mainDirs=new Set();
  for(const file of files){
    if(!file.path.endsWith('.go') || file.path.endsWith('_test.go'))continue;
    const comments=header(file.content);
    if(!comments)continue;
    const slash=file.path.lastIndexOf('/');
    mainDirs.add(slash<0?'.':'./'+file.path.slice(0,slash));
    for(const block of comments)for(const line of block.split('\n')){
      const text=line.trim().replace(/^\*\s?/,'');
      const match=text.match(/^distvis:(name|description|entry|start)(?:\s+(.*))?$/);
      if(!match){
        if(text.startsWith('distvis:'))throw new Error(`${file.path}: invalid distvis annotation (supported: name, description, entry, start)`);
        continue;
      }
      if(Object.hasOwn(annotations,match[1]))throw new Error(`Duplicate distvis:${match[1]}; declare it once in a single Go file header`);
      const value=(match[2] || '').trim();
      if(!value)throw new Error(`distvis:${match[1]} is empty`);
      annotations[match[1]]=match[1]==='start'?parseStart(value):value;
    }
  }
  const metadata={...legacy,...annotations};
  for(const [key,limit] of [['name',100],['description',1000]]){
    if(metadata[key]!==undefined && (typeof metadata[key]!=='string' || !metadata[key].trim() || metadata[key].length>limit))throw new Error(`${key} is invalid`);
  }
  if(metadata.start!==undefined)metadata.start=validStart(metadata.start);
  const entry=explicitEntry || metadata.entry || (mainDirs.has('.')?'.':mainDirs.has('./cmd/node')?'./cmd/node':mainDirs.size===1?[...mainDirs][0]:'');
  if(typeof entry!=='string' || !/^\.(?:\/[a-zA-Z0-9_.-]+)*$/.test(entry) || entry.split('/').includes('..'))throw new Error('No unique main package; declare the entry with // distvis:entry ./cmd/node in a Go file header');
  if(!mainDirs.has(entry))throw new Error('The entry directory has no package main');
  return {...metadata,entry};
}
