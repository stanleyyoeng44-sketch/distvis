"""Package one selected complete export per experiment, with measured READMEs."""
import importlib.util
import json
import shutil
import sys
from collections import Counter
from pathlib import Path
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('verify',ROOT/'scripts/verify-lab-evidence.py');verifier=importlib.util.module_from_spec(spec);spec.loader.exec_module(verifier)
content=json.loads((ROOT/'scripts/lab-report-content.json').read_text())

def compact(v):return json.dumps(v,ensure_ascii=False,separators=(',',':'))
def references(value):
 if isinstance(value,dict):
  if '$state' in value:
   ref=value['$state'];return [f"`{ref['node']}.{ref['path']}`"]
  return [ref for item in value.values() for ref in references(item)]
 if isinstance(value,list):return [ref for item in value for ref in references(item)]
 return []
def describe(step):
 if 'input' in step:
  i=step['input'];line=f"On **{i['node']}**, run `{i['method']}` with `{compact(i.get('values',{}))}`."
  refs=references(i.get('values',{}))
  if refs:line+=' In the manual form, replace each `$state` reference with the actual current value from '+', '.join(refs)+'. The runner resolves these references automatically; do not type `$state` into the application form.'
  if step.get('expect')=='error':line+=' Expect an error [the required failure observation], not successful completion.'
  if step.get('await') is False:line+=' Do not wait for this result before continuing.'
  return line
 if 'concurrent' in step:return 'Use simultaneous input for '+ '; '.join(f"**{i['node']}**: `{i['method']}({compact(i.get('values',{}))})`" for i in step['concurrent'])+'.'
 if 'fault' in step:
  f=step['fault'];k=f['kind']
  if k in ['crash','recover']:return f"{k.capitalize()} **{f['node']}** using the node fault control."
  if k=='heal':return 'Restore all links. This does not recover crashed processes or replay dropped messages.'
  return f"Apply network fault `{compact(f)}`. Omitted link delay/bandwidth use this experiment's defaults."
 if 'wait' in step:return f"Wait {step['wait']/1000:g} seconds while the real nodes continue running."
 if 'until' in step:return f"Wait up to {step.get('timeout',20000)/1000:g} seconds for `{compact(step['until'])}`."
 if 'check' in step:return f"Verify `{compact(step['check'])}`"+(f" — {step['label']}" if 'label' in step else '')+'.'
 return step.get('note','Observe the current state.')

def rows_for(folder,run,validation):
 events=run['events'];states={e['node']:e['state'] for e in events if e['type']=='state'}
 rows=[]
 if folder=='christian':
  for e in events:
   r=e.get('result',{})
   if e['type']=='command_result' and isinstance(r,dict) and 'rttMs'in r:rows.append((f"seq {e['seq']} {e['node']} mode={r['mode']}",f"RTT {r['rttMs']} ms; correction {r['correctionMs']} ms; error after {r['errorAfterMs']} ms"))
 elif folder=='berkeley':
  for n,s in sorted(states.items()):rows.append((n,f"round {s['lastAppliedRound']}; error {s['errorMs']:.2f} ms; last adjustment {s['lastAdjMs']:.2f} ms; remaining {s['slewMsLeft']:.2f} ms"))
 elif folder=='logical-clocks':
  for n,s in sorted(states.items()):rows.append((n,compact(s['clock'])))
  rows.append(('Independent graph check',f"{validation.get('events',0)} events; {validation.get('causalPairs',0)} vector-causal pairs; broken send/receive edges {compact(validation.get('brokenSendReceiveEdges',[]))}"))
 elif folder in ('lamport_mutex','ricart-agrawala'):
  for n,s in sorted(states.items()):rows.append((n,f"entries {s['entries']}; completed {s['completed']}; wait {s['lastWaitMs']:.1f} ms; algorithm messages {s['messagesSent']}; role {s['role']}"))
  rows.append(('Full history',f"maximum simultaneous critical nodes = {validation['maximumCriticalNodes']}"))
  if 'actualLinkOrderReversals' in validation:rows.append(('Actual delivery-order reversals',str(validation['actualLinkOrderReversals'])))
 elif folder=='bully_election':
  for n,s in sorted(states.items()):rows.append((n,f"last reported coordinator {s['coordinator']}; role {s['role']}; won {s['electionsWon']}; sent counter {s['sent']}"))
  rows.append(('Whole-run algorithm counts',compact(validation['algorithmMessages'])))
 elif folder=='quorum-kv':
  for e in events:
   r=e.get('result',{})
   if e['type']=='command_result' and isinstance(r,dict) and 'waitedMs'in r:rows.append((f"seq {e['seq']} {e['node']}",f"{r['key']}={r['item']['value']!r}; version {compact(r['item']['version'])}; {r['waitedMs']:.1f} ms; stale={r['stale']}"))
 elif folder=='paxos-kv':
  for n,s in sorted(states.items()):rows.append((n,f"applied {s['applied']}; values {compact(s['values'])}; conflicts {s['conflicts']}"))
  rows.append(('Chosen-slot check',f"conflicting slots: {compact(validation['conflictingSlots'])}"))
  for slot,point in validation['chosenPoints'].items():rows.append((f'Slot {slot}: observed majority acceptance',f"seq {point['seq']}, {point['timeMs']:.1f} ms, ballot {point['ballot']}, acceptors {compact(point['acceptors'])}"))
 elif folder=='two_phase_commit':
  for n,s in sorted(states.items()):rows.append((n,f"role {s['role']}; transaction {s['txn']}; phase/state {s['phase'] or s['state']}; decision {s['decision']}; balance {s['balance']}"))
  rows.append(('Participant balance sum',str(validation['totalBalance'])))
 return rows

selected = set(sys.argv[1:])
all_rows = [r for r in json.loads((ROOT/'protocols/lab-evidence-index.json').read_text()) if r['protocol'] not in selected] if selected else []
for folder,paragraphs in content.items():
 if selected and folder not in selected: continue
 scenario_spec=json.loads((ROOT/'protocols'/folder/'scenarios.json').read_text())
 for scenario in scenario_spec['scenarios']:
  sid=scenario['id'];origin=ROOT/'.lab-evidence'/folder/sid
  passing=[]
  for p in origin.glob('checks-*.json'):
   c=json.loads(p.read_text())
   if c['passed']:
    candidate=json.loads((origin/f"run-{c['runId']}.json").read_text())
    import base64
    files={f['path']:f for f in candidate['project']['files']}
    code=list((ROOT/'protocols'/folder).glob('*.go'))
    if all(p.name in files and base64.b64decode(files[p.name]['content'])==p.read_bytes() for p in code if not p.name.endswith('_test.go')):
     passing.append((p.stat().st_mtime,c,p))
  if not passing:raise RuntimeError(f'no passing execution: {folder}/{sid}')
  _,checks,checkpath=max(passing);runpath=origin/f"run-{checks['runId']}.json"
  validation=verifier.verify(runpath)
  if not validation['passed']:raise RuntimeError(validation)
  run=json.loads(runpath.read_text());destination=ROOT/'protocols'/folder/'experiments'/sid
  if destination.exists():
   # Inspect the previous selected record before preserving it outside the final folder.
   old = json.loads((destination/'run.json').read_text())
   backup = ROOT/'.lab-backups/packaged-evidence'/folder/(sid+'-'+old['id'])
   backup.parent.mkdir(parents=True,exist_ok=True)
   if backup.exists(): raise RuntimeError('existing evidence backup')
   shutil.move(str(destination),str(backup))
  destination.mkdir(parents=True,exist_ok=False)
  shutil.copyfile(runpath,destination/'run.json')
  (destination/'checks.json').write_text(json.dumps({'scenario':checks,'independent':validation},indent=2)+'\n')
  (destination/'scenario.json').write_text(json.dumps(scenario,indent=2)+'\n')
  counts=Counter(e['payload'].get('method','?') for e in run['events'] if e['type']=='send' and e.get('payload',{}).get('type')=='RPCRequest')
  row={'protocol':folder,'scenario':sid,'runId':run['id'],'experimentId':checks['experimentId'],'passed':True,'eventCount':len(run['events']),'requestCounts':dict(counts)}
  all_rows.append(row)
  text=f"# {scenario['name']}\n\n[Protocol implementation and discussion](../../README.md) · [Complete exported JSON](run.json) · [Checks](checks.json) · [Exact scenario](scenario.json)\n\n"
  text+='## Observation\n\n'+paragraphs[sid]+'\n\n'
  text+=f"## Recorded execution\n\n- Runtime: **Docker** [real Go processes, not the built-in simulation].\n- Run: `{run['id']}`; recorded `{run['createdAt']}`.\n- [Platform replay](http://localhost:3000/#experiment/{checks['experimentId']}/history).\n- Node count: {run['config']['nodeCount']}; base delay: {run['config']['latency']} ms; bandwidth: {run['config']['bandwidth']} KiB/s per directed link.\n- Events: {len(run['events'])}; scenario checks and independent checks passed. An unsafe variant passes only when the intended violation is actually observed.\n\n"
  text+='### Measured values\n\n| Record | Measurement |\n|---|---|\n'
  for a,b in rows_for(folder,run,validation):text+='| '+a.replace('|','\\|')+' | '+b.replace('|','\\|')+' |\n'
  text+='\nThese are actual recorded values. For a crashed node, its final visible state can be the last report before the crash; inspect the exported node-online events before treating it as active.\n\n'
  text+='## Reproduce\n\nFrom the repository root (start `npm start` in another terminal and ensure Docker is available):\n\n```sh\n'+f'DISTVIS_EVIDENCE_DIR=.lab-evidence node scripts/scenarios.mjs run {folder} {sid}\n```\n\n'
  text+='For a manual run, select this protocol, create a fresh Docker experiment with the settings below, wait for every input form, then follow the numbered steps. Pause/replay in the browser only changes what you see; the real processes continue running.\n\n```json\n'+json.dumps({'runtime':'docker','nodeCount':5,'latency':80,'bandwidth':128,'seed':42,**scenario['settings']},indent=2)+'\n```\n\n'
  text+='\n'.join(f'{i}. {describe(step)}' for i,step in enumerate(scenario['steps'],1))+'\n\n'
  text+='## Interpretation and limits\n\nThe observation paragraph distinguishes the intended algorithm result from injected failure or deliberately unsafe code. The [protocol discussion](../../README.md) explains why the design is needed and lists unsupported behavior. The complete JSON includes the exact code snapshot, input events, faults, messages, and node reports, so the result does not depend on trusting a screenshot or this prose alone.\n'
  (destination/'README.md').write_text(text)
 # Existing README was read earlier in this session; append a generated index only.
 readme=ROOT/'protocols'/folder/'README.md'
 text=readme.read_text();marker='\n## Verified experiment evidence\n'
 if marker in text:
  if not selected: raise RuntimeError('evidence index already exists')
  text = text.split(marker,1)[0]
 text+=marker+'\nOne folder per experiment; each contains its report, exact scenario, full export, and check results. These current-code records supersede historical observations elsewhere in the repository.\n\n'
 for s in scenario_spec['scenarios']:text+=f"- [{s['name']}](experiments/{s['id']}/README.md)\n"
 readme.write_text(text)
(ROOT/'protocols'/'lab-evidence-index.json').write_text(json.dumps(all_rows,indent=2)+'\n')
print(f'Packaged {len(all_rows)} verified experiments across {len(content)} protocols.')
