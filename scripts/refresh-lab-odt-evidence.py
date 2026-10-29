"""Update selected run identifiers in the audit section after a verified rerun."""
import json
import re
import zipfile
from pathlib import Path
from xml.etree import ElementTree as E
ROOT=Path('/home/stanley/distvis');path=ROOT/'report/report.odt'
index={(r['protocol'],r['scenario']):r['runId'] for r in json.loads((ROOT/'protocols/lab-evidence-index.json').read_text())}
with zipfile.ZipFile(path)as z:entries=[(i,z.read(i.filename)) for i in z.infolist()]
xml=dict((i.filename,data) for i,data in entries)['content.xml'].decode()
pattern=r'(Evidence: protocols/([^/]+)/experiments/([^/]+)/run\.json; run )([a-f0-9-]{36})'
updated,count=re.subn(pattern,lambda m:m[1]+index[(m[2],m[3])],xml)
if count!=len(index):raise RuntimeError(f'expected {len(index)} references, found {count}')
E.fromstring(updated)
temp=path.with_suffix('.refresh.odt')
with zipfile.ZipFile(temp,'w')as z:
 for info,data in entries:z.writestr(info,updated.encode() if info.filename=='content.xml' else data)
temp.replace(path)
print('Updated',count,'ODT run references.')
