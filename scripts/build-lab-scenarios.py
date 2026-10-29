"""Generate explicit, reviewable LAB1 scenarios; does not contact the platform."""
import json
from pathlib import Path
ROOT=Path('/home/stanley/distvis/protocols')
def inp(n,m,v=None,**kw):return {'input':{'node':f'node-{n}','method':'Application.'+m,'values':v or {}},'expect':'ok',**kw}
def until(n,p,v,op='==',timeout=20000):return {'until':{'node':f'node-{n}' if isinstance(n,int) else n,'path':p,'op':op,'value':v},'timeout':timeout}
def check(n,p,v,op='=='):return {'check':{'node':f'node-{n}' if isinstance(n,int) else n,'path':p,'op':op,'value':v}}
def fault(k,n=None,**kw):return {'fault':{'kind':k,**({'node':f'node-{n}'} if n else {}),**kw}}
def block(a,b):return fault('link',**{'from':f'node-{a}','to':f'node-{b}','blocked':True,'bidirectional':True})
def batch(nodes,m,v):return {'concurrent':[inp(n,m,v)['input'] for n in nodes],'expect':'ok'}
def scenario(i,name,steps,n=3,**settings):return {'id':i,'name':name,'settings':{'nodeCount':n,'latency':80,**settings},'steps':steps}
def save(folder,scenarios):
 p=ROOT/folder/'scenarios.json'
 if p.exists():raise RuntimeError(f'refusing to overwrite {p}')
 p.write_text(json.dumps({'scenarios':scenarios},indent=2)+'\n')
# Berkeley: retain the fixed-master restart boundary; do not claim failover.
off=inp(1,'Auto',{'enabled':False})
save('berkeley',[
 scenario('normal-periodic','Berkeley normal: periodic convergence',[until(1,'lastAppliedRound',2,'>='),{'wait':9000},*[inp(n,'Read') for n in [1,2,3]],*[check(n,'errorMs',850,'<=') for n in [1,2,3]],*[check(n,'errorMs',650,'>=') for n in [1,2,3]]]),
 scenario('outlier','Berkeley: discard outlier vote',[off,inp(3,'SetClock',{'offsetMS':10000}),inp(1,'Sync'),check(1,'lastRound.samples.2.included',False),check(1,'lastRound.meanMS',500,'<'),check(3,'lastAdjMs',-9000,'<')]),
 scenario('crash-master','Berkeley crash: no coordinator election',[off,inp(1,'Sync'),fault('crash',1),inp(2,'Sync',expect='error'),{'wait':4000},check(2,'lastAppliedRound',1)]),
 scenario('partition','Berkeley partition: partial round',[off,block(1,3),inp(1,'Sync'),check(1,'lastRound.samples.2.scheduled',False),check(1,'lastRound.samples.1.scheduled',True),fault('heal'),{'wait':5000},inp(1,'Sync'),check(1,'lastRound.samples.2.scheduled',True)]),
 scenario('variant-jump','Berkeley variant: jump versus slew',[off,inp(1,'Sync'),check(3,'slewMsLeft',-500,'<'),{'wait':9000},*[inp(n,'ConfigureClock',{'offsetMS':(n-1)*750}) for n in [1,2,3]],*[inp(n,'SetMode',{'step':True}) for n in [1,2,3]],inp(1,'Sync'),check(3,'slewMsLeft',0),check(3,'lastAdjMs',-600,'<')])])
# Logical clocks: same modeled data-event graph as lecture.
def local(n,label):return inp(n,'Local',{'label':label})
def send(n,to,label,receive):return inp(n,'Send',{'to':f'node-{to}','label':label,'receiveLabel':receive})
def compare(l,v,lt,r,w,rt):return inp(1,'CompareEvents',{'left':{'lamport':l,'vector':v,'total':lt},'right':{'lamport':r,'vector':w,'total':rt}})
save('logical-clocks',[
 scenario('normal-six-events','Clocks normal: lecture six events',[local(1,'a'),local(3,'e'),send(1,2,'b','c'),send(2,3,'d','f'),compare(2,[2,0,0],6,1,[0,0,1],5),check(1,'lastCompare.vector','concurrent'),check(1,'lastCompare.lamport','a>b'),compare(2,[2,0,0],6,5,[2,2,2],17),check(1,'lastCompare.agrees',True)]),
 scenario('equal-concurrent','Clocks: equal Lamport concurrent events',[batch([1,2,3],'Local',{'label':'first'}),check('*','clock.lamport',1),compare(1,[1,0,0],3,1,[0,1,0],4),check(1,'lastCompare.vector','concurrent')]),
 scenario('auto-invariant','Clocks: automatic random events',[batch([1,2,3],'Auto',{'count':6}),until('*','autoRunning',False,timeout=30000),*[inp(n,'Read') for n in [1,2,3]]]),
 scenario('crash-recovery','Clocks crash: durable counters',[local(2,'before'),fault('crash',2),send(1,2,'lost','not-received'),check(1,'lastDelivery.confirmed',False),fault('recover',2),{'wait':2000},local(2,'after'),check(2,'clock.lamport',2),send(1,2,'retry-new-event','received'),check(1,'lastDelivery.confirmed',True)]),
 scenario('partition','Clocks partition: local events continue',[block(1,3),send(1,3,'lost','absent'),check(1,'lastDelivery.confirmed',False),local(3,'independent'),fault('heal'),send(1,3,'new-send','new-receive'),check(1,'lastDelivery.confirmed',True)]),
 scenario('variant-no-merge','Clocks variant: omit receive merge',[inp(2,'Variant',{'omitReceiveMerge':True}),local(1,'a'),send(1,2,'b','c'),check(2,'clock.lamport',1),compare(2,[2,0,0],6,1,[0,1,0],4),check(1,'lastCompare.vector','concurrent'),check(1,'lastCompare.lamport','a>b')])])
# Separate Ricart submission uses the same interface and safety checks.
safety={'check':{'atMostOne':{'path':'role','value':'critical'}},'label':'No overlapping critical sections in reported history'}
save('ricart-agrawala',[
 scenario('normal-contention','RA normal: simultaneous node-1 and node-3',[batch([1,3],'Acquire',{'holdMs':2000}),until([ 'node-1','node-3'],'completed',1),safety]),
 scenario('crash','RA crash: uninvolved peer blocks',[fault('crash',2),inp(1,'Acquire',{'holdMs':200}),{'wait':4000},check(1,'role','waiting'),check(1,'entries',0),check(1,'missingAcks','node-2','includes'),safety]),
 scenario('partition','RA partition: no replay after healing',[block(1,3),inp(1,'Acquire',{'holdMs':200}),{'wait':4000},check(1,'entries',0),fault('heal'),{'wait':2000},check(1,'entries',0),safety]),
 scenario('jitter-fifo','RA random load: exponential FIFO',[batch([1,2,3,4,5],'Auto',{'count':2}),until('*','completed',2,timeout=60000),safety],n=5,delayModel='exponential',jitter=80),
 scenario('jitter-unordered','RA random load: unordered messages',[batch([1,2,3,4,5],'Auto',{'count':2}),until('*','completed',2,timeout=60000),safety],n=5,delayModel='exponential',jitter=80,network={'ordering':'unordered','reorderRate':0.5,'reorderMin':100,'reorderMax':400}),
 scenario('variant-message-count','RA design: four messages, no Release',[inp(1,'Acquire',{'holdMs':200}),until(1,'completed',1),{'check':{'messages':{'method':'MutexService.Request','op':'==','value':2}}},{'check':{'messages':{'method':'MutexService.Ack','op':'==','value':2}}},{'check':{'messages':{'method':'MutexService.Release','op':'==','value':0}}},safety])])
# Bully uses actual seven nodes, not the historical eight-node substitution.
save('bully_election',[
 scenario('normal-startup','Bully normal: automatic five-node election',[until('*','coordinator','node-5'),check(5,'role','coordinator')],n=5),
 scenario('crash-auto','Bully crash: automatic failure detection',[until('*','coordinator','node-5'),fault('crash',5),until('*','coordinator','node-4',timeout=25000)],n=5),
 scenario('seven-node-lecture','Bully: seven-node lecture trace',[until('*','coordinator','node-7'),fault('crash',7),inp(4,'StartElection'),until('*','coordinator','node-6',timeout=25000)],n=7),
 scenario('two-crashes','Bully: two highest nodes crash',[until('*','coordinator','node-7'),fault('crash',7),fault('crash',6),until('*','coordinator','node-5',timeout=25000)],n=7),
 scenario('partition','Bully partition: two component coordinators',[until('*','coordinator','node-5'),*[block(a,b) for a in [1,2] for b in [3,4,5]],until(2,'coordinator','node-2',timeout=25000),check(5,'coordinator','node-5'),fault('heal'),inp(1,'StartElection'),until('*','coordinator','node-5')],n=5),
 scenario('variant-recovery-takeover','Bully variant: recovered highest node takes over',[until('*','coordinator','node-7'),fault('crash',7),until('*','coordinator','node-6',timeout=25000),fault('recover',7),{'wait':2000},until('*','coordinator','node-7',timeout=25000)],n=7)])
# Quorum includes both nonintersection variant and CP partition.
save('quorum-kv',[
 scenario('normal','Quorum normal: sequential write and read',[inp(1,'Put',{'key':'x','value':'1'}),inp(3,'Get',{'key':'x','minCounter':1}),check(3,'lastGet.item.value','1'),check(3,'lastGet.stale',False)],n=5),
 scenario('slow-replica','Quorum: fastest majority versus all replicas',[fault('link',**{'from':'node-1','to':'node-5','latency':3000}),inp(1,'Put',{'key':'x','value':'1'}),check(1,'lastPut.waitedMs',2000,'<'),inp(1,'Mode',{'w':5,'r':3}),inp(1,'Put',{'key':'x','value':'2'},expect='error'),check(1,'lastPut.success',False)],n=5),
 scenario('crash','Quorum crash: majority still available',[fault('crash',5),inp(1,'Put',{'key':'x','value':'1'}),inp(3,'Get',{'key':'x'}),check(3,'lastGet.item.value','1'),fault('recover',5),{'wait':2000},inp(5,'Get',{'key':'x'}),check(5,'lastGet.item.value','1')],n=5),
 scenario('partition','Quorum partition: minority unavailable',[*[block(a,b) for a in [1,2] for b in [3,4,5]],inp(1,'Put',{'key':'minority','value':'no'},expect='error'),inp(3,'Put',{'key':'x','value':'majority'}),inp(4,'Get',{'key':'x'}),check(4,'lastGet.item.value','majority'),fault('heal'),inp(1,'Get',{'key':'x'}),check(1,'lastGet.item.value','majority')],n=5),
 scenario('variant-nonintersection','Quorum variant: W=R=1 stale read',[*[inp(n,'Mode',{'w':1,'r':1}) for n in [1,2,3,4,5]],*[block(1,b) for b in [2,3,4,5]],inp(1,'Put',{'key':'x','value':'1'}),inp(3,'Get',{'key':'x','minCounter':1}),check(3,'lastGet.stale',True),check(3,'lastGet.item.version.counter',0)],n=5)])
# Paxos: a design variant with one forgetful intersection acceptor.
save('paxos-kv',[
 scenario('normal','Paxos KV normal: reads also occupy slots',[inp(1,'Put',{'key':'x','value':'1'}),inp(2,'Get',{'key':'x'}),inp(3,'Put',{'key':'x','value':'2'}),inp(4,'Get',{'key':'x'}),until('*','applied',4),check(4,'lastOperation.value','2')],n=5),
 scenario('competing-writes','Paxos KV: simultaneous slot-one proposals',[{'concurrent':[inp(1,'Put',{'key':'a','value':'1','pauseMs':500})['input'],inp(2,'Put',{'key':'b','value':'2','pauseMs':500})['input']],'expect':'ok'},until('*','applied',2),check('*','values.a','1'),check('*','values.b','2'),check('any','conflicts',1,'>=')],n=5),
 scenario('crash-catchup','Paxos KV crash: no-op hole recovery',[inp(1,'Put',{'key':'x','value':'1'}),until('*','applied',1),fault('crash',5),*[inp(1,'Put',{'key':'x','value':str(v)}) for v in [2,3,4]],fault('recover',5),{'wait':2000},inp(1,'Put',{'key':'y','value':'5'}),until(5,'applied',5,timeout=30000),check(5,'values.x','4')],n=5),
 scenario('partition','Paxos KV partition: minority cannot commit',[*[block(a,b) for a in [1,2,3] for b in [4,5]],inp(4,'Put',{'key':'x','value':'minority'},expect='error'),inp(1,'Put',{'key':'x','value':'majority'}),fault('heal'),inp(1,'Get',{'key':'x'}),until('*','applied',2,timeout=30000),check('*','values.x','majority')],n=5),
 scenario('variant-amnesia','Paxos KV variant: forgetful intersection accepts conflicting slot',[inp(3,'Amnesia',{'enabled':True}),*[block(a,b) for a in [1,2,3] for b in [4,5]],inp(1,'Put',{'key':'x','value':'first'}),until(3,'applied',1),fault('crash',3),fault('heal'),*[block(a,b) for a in [1,2] for b in [3,4,5]],fault('recover',3),{'wait':2000},inp(4,'Put',{'key':'x','value':'second'}),check(1,'values.x','first'),check(4,'values.x','second')],n=5),
 scenario('persistence-control','Paxos KV control: intersection remembers chosen value',[*[block(a,b) for a in [1,2,3] for b in [4,5]],inp(1,'Put',{'key':'x','value':'first'}),until(3,'applied',1),fault('crash',3),fault('heal'),*[block(a,b) for a in [1,2] for b in [3,4,5]],fault('recover',3),{'wait':2000},inp(4,'Put',{'key':'x','value':'second'}),check(4,'log.1.value.value','first'),check(4,'log.2.value.value','second')],n=5)])
# 2PC: asynchronous Transfer acceptance; wait for actual phase/decision.
def transfer(**kw):return inp(1,'Transfer',{'from':'node-2','to':'node-3','amount':30,**kw})
save('two_phase_commit',[
 scenario('normal-two-transfers','2PC normal: two transfers conserve 200',[transfer(),until(1,'phase','finished'),check(2,'balance',70),check(3,'balance',130),transfer(),until(1,'phase','finished'),check(2,'balance',40),check(3,'balance',160)]),
 scenario('veto','2PC: insufficient balance veto',[transfer(amount=150),until(1,'phase','finished'),check(1,'decision','abort'),check(2,'balance',100),check(3,'balance',100),check(3,'state','aborted')]),
 scenario('crash-before-decision','2PC crash: before durable decision aborts',[transfer(pauseBeforeDecisionMs=30000),until(1,'votes.node-3','yes'),fault('crash',1),until(2,'state','uncertain'),fault('recover',1),{'wait':2000},until(1,'phase','finished'),check(1,'decision','abort'),check(2,'balance',100),check(3,'balance',100)]),
 scenario('crash-after-decision','2PC crash: durable commit completes',[transfer(pauseAfterDecisionMs=30000),until(1,'decision','commit'),fault('crash',1),until(2,'state','uncertain'),check(2,'balance',100),fault('recover',1),{'wait':2000},until(1,'phase','finished'),check(2,'balance',70),check(3,'balance',130)]),
 scenario('partition','2PC partition: notification retries after heal',[transfer(pauseAfterDecisionMs=2000),until(1,'decision','commit'),block(1,2),until(3,'state','committed'),until(2,'state','uncertain'),check(1,'phase','decided'),fault('heal'),until(1,'phase','finished'),check(2,'balance',70)]),
 scenario('variant-no-prepare-log','2PC variant: YES without durable prepared record',[inp(2,'Variant',{'skipPrepareSave':True}),transfer(pauseAfterDecisionMs=4000),until(1,'decision','commit'),fault('crash',2),fault('recover',2),{'wait':2000},until(3,'state','committed'),check(2,'balance',100),check(3,'balance',130),check(1,'phase','decided')]),
 scenario('participant-recovery-control','2PC control: prepared log survives participant crash',[transfer(pauseAfterDecisionMs=4000),until(1,'decision','commit'),fault('crash',2),fault('recover',2),{'wait':2000},until(1,'phase','finished'),check(2,'balance',70),check(3,'balance',130)])])
