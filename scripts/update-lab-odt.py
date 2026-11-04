"""Preserve the original ODT and prepend an audited replacement report.

The historical body and embedded screenshots remain intact and are explicitly
marked superseded, so existing observations are not silently fabricated anew.
"""
import json
import shutil
import zipfile
from pathlib import Path
from xml.etree import ElementTree as E
ROOT=Path('/home/stanley/distvis')
source=ROOT/'report/report.odt'
backup=ROOT/'.lab-backups/report-before-lab-audit.odt'
if backup.exists():raise RuntimeError('backup already exists; refusing to reapply')
backup.parent.mkdir(parents=True,exist_ok=True)
shutil.copy2(source,backup)
with zipfile.ZipFile(source) as z:
 entries=[(i,z.read(i.filename)) for i in z.infolist()]
xml=dict((i.filename,b) for i,b in entries)['content.xml']
# Register source namespace names to retain ODF style references and structure.
import io
for _,pair in E.iterparse(io.BytesIO(xml),events=['start-ns']):
 try:E.register_namespace(*pair)
 except ValueError:pass
root=E.fromstring(xml)
ns={'office':'urn:oasis:names:tc:opendocument:xmlns:office:1.0','text':'urn:oasis:names:tc:opendocument:xmlns:text:1.0','xlink':'http://www.w3.org/1999/xlink'}
body=root.find('office:body/office:text',ns)
if body is None:raise RuntimeError('missing document text')
paragraphs=[]
def paragraph(text,level=None):
 tag='h' if level else 'p';el=E.Element('{'+ns['text']+'}'+tag)
 if level:el.set('{'+ns['text']+'}outline-level',str(level))
 el.text=text;paragraphs.append(el);return el
paragraph('Lab 1 — audited submission',1)
paragraph('This section replaces the conclusions of the historical draft below. The canonical submission is organized in protocols/: one standalone module per selected protocol and one experiments/<scenario>/ folder per observation, each with README.md, scenario.json, run.json, and checks.json. The original draft and its screenshots are retained below for provenance [the record of where earlier observations came from], not presented as evidence for revised code.')
paragraph('Scope and verification',2)
paragraph('The nine selected protocols satisfy the category choices: three clock protocols (Cristian, Berkeley, Lamport plus vector clocks), two mutual-exclusion protocols (Lamport and Ricart–Agrawala), required Bully election, Quorum KV replication, and two consensus/commit choices (Paxos KV and two-phase commit). Fifty-three real Docker scenarios passed their scenario assertions and the additional independent checks applicable to each protocol. Each protocol has normal, crash, network-failure, and design-variant evidence; additional protocol-specific scenarios are included.')
paragraph('Corrections to the earlier report',2)
for text in [
 'Berkeley: the old text mixed manual synchronization with the required periodic-round observation. The submitted implementation now has automatic rounds and state refreshes, plus an explicit jump/slew comparison. A large outlier correction being scheduled does not mean it has completed. With the fixed coordinator restart guard, recovery requires a fresh run; ordinary participant recovery must not be described as coordinator election.',
 'Bully: the old implementation and report relied on manual startup and recovery elections. The submitted implementation elects automatically at startup and after coordinator failure/recovery. The lecture example now uses exactly seven nodes; the two-failure case crashes only the two highest nodes. A partition can produce two component coordinators, so a single global leader is not claimed under partition.',
 'Lamport mutex: accepted application input is distinct from entering or completing the critical section. Current traces include the full waiting and completion state, a blocked-link scenario, and six-versus-four algorithm-message controls. Empty RPC transport responses are excluded from the lecture message count.',
 'Logical clocks and Ricart–Agrawala: complete scenario reports were missing from the combined draft. They are now included under their own protocol folders, including automatic events, unordered network load, crash/network behavior, and design comparisons.',
 '2PC: the submission uses three nodes and performs two normal transfers, so the conserved participant balance is 200. The older four-node experiment correctly totaled 300 for three RMs, but did not itself demonstrate the requested two-transfer three-node case. Transfer now returns acceptance promptly; transaction completion is phase=finished. The missing-prepare-log variant has an explicit safe persistence control.',
 'Quorum KV and Paxos KV: these were not in the completed list. Both now have standalone implementations, tests, Docker runs, and reports. The Quorum store uses version queries and read repair, so its fast path has additional rounds compared with the homework’s simplified latency estimate. Its stale flag compares with caller-supplied knowledge rather than claiming access to an unreachable global latest value.'
]:paragraph(text)
index=json.loads((ROOT/'protocols/lab-evidence-index.json').read_text())
content=json.loads((ROOT/'scripts/lab-report-content.json').read_text())
for folder,reports in content.items():
 paragraph(folder,2)
 paragraph('Implementation, state fields, inputs, discussion answers, and limitations: protocols/'+folder+'/README.md')
 for row in [r for r in index if r['protocol']==folder]:
  sid=row['scenario'];paragraph(sid,3);paragraph(reports[sid])
  paragraph('Evidence: protocols/'+folder+'/experiments/'+sid+'/run.json; run '+row['runId']+'. Exact reproduction steps and measured values: the README.md in the same folder.')
paragraph('Assessment boundary',2)
paragraph('Passing tests establish the listed observed executions, not correctness for every possible schedule or a guaranteed grade. The protocol READMEs identify unsupported behavior, including crash/recovery for the mutex algorithms, partition-safe global leadership for Bully, and unbounded production operation. Deliberately unsafe variants are labeled and are not counted as safe executions.')
paragraph('Historical draft — superseded where inconsistent',1)
paragraph('The remainder is the original draft with its original embedded screenshots. Its historical measurements are preserved, but the audited protocol reports above are the authoritative submission for the current code.')
# ODF declarations must remain before ordinary content.
position=0
while position<len(body) and body[position].tag.rsplit('}',1)[-1] in {'sequence-decls','variable-decls','user-field-decls','dde-connection-decls'}:position+=1
for el in paragraphs:body.insert(position,el);position+=1
updated=E.tostring(root,encoding='utf-8',xml_declaration=True)
temp=source.with_suffix('.audit-tmp.odt')
with zipfile.ZipFile(temp,'w') as z:
 for info,data in entries:z.writestr(info,updated if info.filename=='content.xml' else data)
with zipfile.ZipFile(temp) as z:
 if z.testzip() is not None:raise RuntimeError('invalid ODT archive')
 E.fromstring(z.read('content.xml'))
temp.replace(source)
print('Updated ODT; preserved original at',backup)
