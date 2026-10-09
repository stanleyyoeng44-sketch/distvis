// Transport policy shared by every protocol and execution backend. Rates are
// probabilities; time is milliseconds. FIFO is the compatibility default.
export const networkDefaults = Object.freeze({lossRate:0,failureMode:'timeout',ordering:'fifo',reorderRate:0,reorderMin:200,reorderMax:2200});
export function networkPolicy(input={}, base=networkDefaults) {
  if(!input || typeof input!=='object' || Array.isArray(input))throw new Error('network 应为对象');
  for(const key of Object.keys(input))if(!(key in networkDefaults))throw new Error(`未知网络参数 ${key}`);
  const value={...base,...input};
  if(!['timeout','notify'].includes(value.failureMode))throw new Error('failureMode 应为 timeout 或 notify');
  for(const k of ['lossRate','reorderRate'])if(typeof value[k]!=='number'||!Number.isFinite(value[k])||value[k]<0||value[k]>1)throw new Error(`${k} 应为 0–1`);
  if(!['fifo','unordered'].includes(value.ordering))throw new Error('ordering 应为 fifo 或 unordered');
  for(const k of ['reorderMin','reorderMax'])if(!Number.isInteger(value[k])||value[k]<0||value[k]>30000)throw new Error(`${k} 应为 0–30000 的整数`);
  if(value.reorderMin>value.reorderMax)throw new Error('reorderMin 大于 reorderMax');
  return value;
}
export function networkProfiles(input={}) {
  if(!input || typeof input!=='object'||Array.isArray(input))throw new Error('networkProfiles 应为对象');
  for(const k of Object.keys(input))if(!['request','response'].includes(k))throw new Error('网络类别应为 request 或 response');
  return Object.fromEntries(Object.entries(input).map(([k,v])=>[k,networkPolicy(v)]));
}
