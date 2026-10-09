import test from 'node:test';
import assert from 'node:assert/strict';
import {Experiment,validateConfig} from '../server/engine.js';
const make=network=>{const r=new Experiment({protocol:'custom',runtime:'docker',nodeCount:2,latency:0,bandwidth:100000,network});r.start();return r};
test('loss is seeded, applied separately to requests and replies, and cannot be bypassed by payload flags',()=>{
 const r=make({lossRate:1});let deliveries=0;r.on('deliver',()=>deliveries++);
 r.send('node-1','node-2',{type:'RPCRequest',rpcId:'a',control:true});r.send('node-2','node-1',{type:'RPCResponse',rpcId:'b'});r.advance(100);
 assert.equal(deliveries,0);assert.equal(r.events.filter(e=>e.type==='drop').length,2);
 r.fault({kind:'network',profiles:{request:{lossRate:1},response:{lossRate:0}}});r.send('node-1','node-2',{type:'RPCRequest',rpcId:'c'});r.send('node-2','node-1',{type:'RPCResponse',rpcId:'d'});r.advance(100);assert.equal(deliveries,1);
 assert.equal(r.config.network.lossRate,1,'initial configuration is immutable');
});
test('FIFO preserves order while unordered links allow a later packet to arrive first',()=>{
 const arrivals=ordering=>{const r=make({ordering,reorderRate:.5,reorderMin:100,reorderMax:100});const order=[];r.on('deliver',m=>order.push(m.payload.n));r.random=()=>0;r.send('node-1','node-2',{n:1});r.random=()=>.9;r.send('node-1','node-2',{n:2});r.advance(200);return order;};
 assert.deepEqual(arrivals('fifo'),[1,2]);assert.deepEqual(arrivals('unordered'),[2,1]);
});
test('directed packet policies do not change reverse traffic; crashed incarnations discard in-flight packets',()=>{
 const r=make({});let delivered=[];r.on('deliver',m=>delivered.push(m));r.fault({kind:'link',from:'node-1',to:'node-2',latency:0,bandwidth:100000,blocked:false,bidirectional:false,network:{lossRate:1}});r.send('node-1','node-2',{});r.send('node-2','node-1',{});r.advance(20);assert.equal(delivered.length,1);assert.equal(delivered[0].from,'node-2');
 r.fault({kind:'heal'});r.send('node-1','node-2',{});r.fault({kind:'crash',node:'node-2'});r.fault({kind:'recover',node:'node-2'});r.advance(20);assert.equal(delivered.length,1);
});
test('failure notification is explicit; timeout mode does not send delivery feedback',()=>{
 for(const mode of ['timeout','notify']){const r=make({lossRate:1,failureMode:mode});let signals=[];r.on('delivery-failed',m=>signals.push(m));r.send('node-2','node-1',{type:'RPCResponse',rpcId:'x'});assert.equal(signals.length,mode==='notify'?1:0);if(signals.length)assert.equal(signals[0].payload.rpcId,'x')}
});
test('channel faults are logged, scoped to the channel and restored by heal',()=>{
 const r=make({});let delivered=0;r.on('deliver',()=>delivered++);r.fault({kind:'channel',id:'peer-a',enabled:false});r.send('node-1','node-2',{channel:'peer-a'});r.send('node-1','node-2',{channel:'peer-b'});r.advance(100);assert.equal(delivered,1);r.fault({kind:'heal'});r.send('node-1','node-2',{channel:'peer-a'});r.advance(100);assert.equal(delivered,2);
});
test('network and execution settings reject invalid values',()=>{
 for(const network of [{lossRate:NaN},{lossRate:2},{ordering:'random'},{reorderMin:20,reorderMax:10},{failureMode:'success'}])assert.throws(()=>validateConfig({network}));
 assert.throws(()=>validateConfig({nodeCount:100}));assert.equal(validateConfig({nodeCount:32}).nodeCount,32);
});
