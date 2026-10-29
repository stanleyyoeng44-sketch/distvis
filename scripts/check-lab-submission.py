"""Read-only final audit of the nine selected modules and 53 evidence folders."""
import base64
import json
import re
import sys
import zipfile
from pathlib import Path
from urllib.parse import unquote
from xml.etree import ElementTree as E
ROOT=Path(__file__).resolve().parents[1]
import importlib.util
spec=importlib.util.spec_from_file_location('evidence_verifier',ROOT/'scripts/verify-lab-evidence.py')
verifier=importlib.util.module_from_spec(spec);spec.loader.exec_module(verifier)
rows=json.loads((ROOT/'protocols/lab-evidence-index.json').read_text())
errors=[];folders=sorted(set(r['protocol'] for r in rows));run_ids=set()
for folder in folders:
 module=ROOT/'protocols'/folder
 expected={s['id'] for s in json.loads((module/'scenarios.json').read_text())['scenarios']}
 actual={p.name for p in (module/'experiments').iterdir() if p.is_dir()}
 if expected!=actual:errors.append(f'{folder}: expected {expected}, found {actual}')
 for required in ['main.go','protocol.go','go.mod','README.md','scenarios.json']:
  if not (module/required).is_file():errors.append(f'{folder}: missing {required}')
 for readme in [module/'README.md',*(module/'experiments').glob('*/README.md')]:
  text=readme.read_text()
  if readme.parent.name!='experiments' and readme.parent.parent.name=='experiments' and '## Observation' not in text:errors.append(f'{readme}: no observation')
  for target in re.findall(r'\[[^\]]*\]\(([^)]+)\)',text):
   if '://'in target or target.startswith('#'):continue
   path=unquote(target.split('#',1)[0])
   if path and not (readme.parent/path).exists():errors.append(f'{readme.relative_to(ROOT)}: broken link {target}')
for row in rows:
 folder=ROOT/'protocols'/row['protocol'];directory=folder/'experiments'/row['scenario']
 for required in ['README.md','run.json','scenario.json','checks.json']:
  if not (directory/required).is_file():errors.append(f'{directory}: missing {required}')
 run=json.loads((directory/'run.json').read_text());check=json.loads((directory/'checks.json').read_text())
 if run['id']!=row['runId'] or run['id']in run_ids:errors.append('duplicate/mismatched run ID')
 run_ids.add(run['id'])
 if run['config']['runtime']!='docker':errors.append(f'{directory}: not Docker')
 if not check['scenario']['passed'] or not check['independent']['passed']:errors.append(f'{directory}: failed checks')
 computed=verifier.verify(directory/'run.json')
 if not computed['passed']:errors.extend(f'{directory}: {e}' for e in computed['errors'])
 if computed!=check['independent']:errors.append(f'{directory}: saved independent checks differ from recomputation')
 canonical=next(s for s in json.loads((folder/'scenarios.json').read_text())['scenarios'] if s['id']==row['scenario'])
 if canonical!=json.loads((directory/'scenario.json').read_text()):errors.append(f'{directory}: scenario differs from canonical definition')
 for key,value in canonical.get('settings',{}).items():
  actual=run['config'].get(key)
  if isinstance(value,dict):
   if not isinstance(actual,dict) or any(actual.get(k)!=v for k,v in value.items()):errors.append(f'{directory}: settings differ for {key}')
  elif actual!=value:errors.append(f'{directory}: settings differ for {key}')
 # Check every executable node snapshot, including extra/deleted source files.
 for owner,project in {'shared':run['project'],**run.get('nodeProjects',{})}.items():
  files={f['path']:f for f in project['files']}
  current={p.name for p in folder.glob('*.go') if not p.name.endswith('_test.go')}
  exported={p for p in files if p.endswith('.go') and not p.endswith('_test.go')}
  if current!=exported:errors.append(f'{directory}: {owner} executable file set differs')
  for name in current|{'go.mod','go.sum'}:
   entry=files.get(name)
   if not entry or base64.b64decode(entry['content'])!=(folder/name).read_bytes():errors.append(f'{directory}: {owner} source mismatch {name}')
 if row['eventCount']!=len(run['events']):errors.append(f'{directory}: index event count differs')
 if 'stanleyreport' in (directory/'README.md').read_text():errors.append('excluded folder referenced')
with zipfile.ZipFile(ROOT/'report/report.odt')as z:
 if z.testzip():errors.append('invalid ODT archive')
 document=E.fromstring(z.read('content.xml'))
 text=' '.join(document.itertext())
 if 'Lab 1 — audited submission'not in text:errors.append('missing ODT audit section')
 if 'Historical draft — superseded where inconsistent'not in text:errors.append('historical draft not labeled')
 audit=text.split('Historical draft — superseded where inconsistent')[0]
 for row in rows:
  if row['runId'] not in audit:errors.append(f"ODT: stale/missing run reference for {row['protocol']}/{row['scenario']}")
print(json.dumps({'protocols':len(folders),'experiments':len(rows),'passed':not errors,'errors':errors},indent=2))
sys.exit(bool(errors))
