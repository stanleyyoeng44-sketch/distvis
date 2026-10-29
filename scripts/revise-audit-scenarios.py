"""Apply the second audit's targeted scenario corrections; no run data is edited."""
import json
from pathlib import Path
ROOT = Path(__file__).resolve().parents[1]

def input(node, method, values=None):
    return {'input': {'node': node, 'method': 'Application.' + method, 'values': values or {}}, 'expect': 'ok'}
def condition(node, path, value, op='=='):
    return {'node': node, 'path': path, 'op': op, 'value': value}
def until(c, timeout=30000):
    return {'until': c, 'timeout': timeout}
def check(c):
    return {'check': c}
def state(node, path):
    return {'$state': {'node': node, 'path': path}}
def comparison(left, right, relation=None):
    steps = [input('node-1', 'CompareEvents', {'left': left, 'right': right})]
    if relation:
        steps.append(check(condition('node-1', 'lastCompare.vector', relation)))
    steps.append(check(condition('node-1', 'lastCompare.agrees', True)))
    return steps

path = ROOT/'protocols/berkeley/scenarios.json'
spec = json.loads(path.read_text())
for s in spec['scenarios']:
    if s['id'] == 'crash-master':
        s['name'] = 'Berkeley crash: drift while down, converge after recovery'
        s['steps'] = [input('node-1', 'Auto', {'enabled': False}), {'wait': 1000}]
        for node, drift in [('node-1', 0), ('node-2', 10000), ('node-3', -10000)]:
            s['steps'].append(input(node, 'SetClock', {'offsetMS': 750, 'driftPPM': drift}))
        s['steps'] += [input('node-1', 'Auto', {'enabled': True}),
            until(condition('*', 'lastAppliedRound', 2, '>=')),
            until({'spread': 'errorMs', 'op': '<', 'value': 100}),
            {'fault': {'kind': 'crash', 'node': 'node-1'}},
            {'wait': 12000},
            check({'spread': 'errorMs', 'nodes': ['node-2','node-3'], 'op': '>', 'value': 150}),
            {'fault': {'kind': 'recover', 'node': 'node-1'}},
            until(condition('*', 'lastAppliedRound', 4, '>='), 45000),
            until({'spread': 'errorMs', 'op': '<', 'value': 120}, 45000)]
    if s['id'] == 'normal-periodic':
        s['steps'].append(check({'spread': 'errorMs', 'op': '<', 'value': 40}))
    if s['id'] == 'variant-jump':
        s['steps'].insert(3, check(condition('node-3', 'clockAfterAdjustmentMS', state('node-3','clockBeforeAdjustmentMS'))))
        s['steps'].append(check(condition('node-3', 'clockAfterAdjustmentMS', state('node-3','clockBeforeAdjustmentMS'), '<')))
path.write_text(json.dumps(spec, indent=2)+'\n')

path = ROOT/'protocols/logical-clocks/scenarios.json'
spec = json.loads(path.read_text())
for s in spec['scenarios']:
    if s['id'] == 'normal-six-events':
        s['steps'] = s['steps'][:4]
        for node, values in [('node-1', [(1,[1,0,0],3),(2,[2,0,0],6)]),
                             ('node-2', [(3,[2,1,0],10),(4,[2,2,0],13)]),
                             ('node-3', [(1,[0,0,1],5),(5,[2,2,2],17)])]:
            for i,(l,v,t) in enumerate(values):
                s['steps'].append(check(condition(node,f'events.{i}.timestamp',{'lamport':l,'vector':v,'total':t})))
        s['steps'] += comparison(state('node-1','events.1.timestamp'),state('node-3','events.0.timestamp'),'concurrent')
        s['steps'].append(check(condition('node-1','lastCompare.lamport','a>b')))
        s['steps'] += comparison(state('node-1','events.1.timestamp'),state('node-3','events.1.timestamp'),'happens-before')
    if s['id'] == 'equal-concurrent':
        s['steps'] = s['steps'][:2]
        for i,j in [(1,2),(1,3),(2,3)]:
            s['steps'] += comparison(state(f'node-{i}','events.0.timestamp'),state(f'node-{j}','events.0.timestamp'),'concurrent')
            s['steps'].append(check(condition('node-1','lastCompare.lamport','equal')))
    if s['id'] == 'auto-invariant':
        # Compare actual sampled events on all three processes after the load.
        for i,j in [(1,2),(1,3),(2,3)]:
            s['steps'] += comparison(state(f'node-{i}','events.0.timestamp'),state(f'node-{j}','clock'))
    if s['id'] == 'variant-no-merge':
        for step in s['steps']:
            if step.get('input',{}).get('method') == 'Application.CompareEvents':
                step['input']['values'] = {'left':state('node-1','events.1.timestamp'),'right':state('node-2','events.0.timestamp')}
path.write_text(json.dumps(spec, indent=2)+'\n')
