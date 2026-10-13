// Transport policy shared by every protocol and execution backend. Rates are
// probabilities; time is milliseconds. FIFO is the compatibility default.
export const networkDefaults = Object.freeze({lossRate:0,failureMode:'timeout',ordering:'fifo',reorderRate:0,reorderMin:200,reorderMax:2200});
export function networkPolicy(input={}, base=networkDefaults) {
  if(!input || typeof input!=='object' || Array.isArray(input))throw new Error('network must be an object');
  for(const key of Object.keys(input))if(!(key in networkDefaults))throw new Error(`Unknown network parameter ${key}`);
  const value={...base,...input};
  if(!['timeout','notify'].includes(value.failureMode))throw new Error('failureMode must be timeout or notify');
  for(const k of ['lossRate','reorderRate'])if(typeof value[k]!=='number'||!Number.isFinite(value[k])||value[k]<0||value[k]>1)throw new Error(`${k} must be between 0 and 1`);
  if(!['fifo','unordered'].includes(value.ordering))throw new Error('ordering must be fifo or unordered');
  for(const k of ['reorderMin','reorderMax'])if(!Number.isInteger(value[k])||value[k]<0||value[k]>30000)throw new Error(`${k} must be an integer between 0 and 30000`);
  if(value.reorderMin>value.reorderMax)throw new Error('reorderMin is greater than reorderMax');
  return value;
}
export function networkProfiles(input={}) {
  if(!input || typeof input!=='object'||Array.isArray(input))throw new Error('networkProfiles must be an object');
  for(const k of Object.keys(input))if(!['request','response'].includes(k))throw new Error('Network categories must be request or response');
  return Object.fromEntries(Object.entries(input).map(([k,v])=>[k,networkPolicy(v)]));
}
