"""Refresh only the existing audited ODT section; preserve historical XML/images."""
import html
import json
import re
import shutil
import zipfile
from pathlib import Path
from xml.etree import ElementTree as E

ROOT = Path(__file__).resolve().parents[1]
path = ROOT/'report/report.odt'
with zipfile.ZipFile(path) as archive:
    entries = [(info, archive.read(info.filename)) for info in archive.infolist()]
xml = next(data for info, data in entries if info.filename == 'content.xml').decode()
marker = 'Historical draft — superseded where inconsistent'
if xml.count(marker) != 1:
    raise RuntimeError('expected one historical boundary')
audit, historical = xml.split(marker, 1)
content = json.loads((ROOT/'scripts/lab-report-content.json').read_text())
index = json.loads((ROOT/'protocols/lab-evidence-index.json').read_text())
lookup = {(row['protocol'], row['scenario']): row['runId'] for row in index}
# Each scenario observation immediately precedes its unique evidence paragraph.
pattern = r'(<text:p>)([^<]*)(</text:p><text:p>Evidence: protocols/([^/]+)/experiments/([^/]+)/run\.json; run )([a-f0-9-]{36})'
count = 0

def replace(match):
    global count
    folder, scenario = match[4], match[5]
    count += 1
    return match[1] + html.escape(content[folder][scenario], quote=False) + match[3] + lookup[(folder, scenario)]

audit = re.sub(pattern, replace, audit)
if count != len(index):
    raise RuntimeError(f'expected {len(index)} scenario paragraphs, matched {count}')
old = 'With the fixed coordinator restart guard, recovery requires a fresh run; ordinary participant recovery must not be described as coordinator election.'
new = 'The second audit also checked the Chinese requirements: the same coordinator now restores its durable identity and round sequence after restart, and automatic synchronization resumes. The crash experiment measures drift while it is down and renewed convergence after recovery; no replacement election is claimed.'
if old not in audit and new not in audit:
    raise RuntimeError('missing Berkeley correction paragraph')
audit = audit.replace(old, new)
old = 'Fifty-three real Docker scenarios passed their scenario assertions and the additional independent checks applicable to each protocol.'
new = 'All fifty-three real Docker scenarios were rerun during the second audit and passed their scenario assertions and strengthened independent checks. Both language versions of the assignment were checked; saved pass flags alone are not accepted as verification.'
audit = audit.replace(old, new)
updated = audit + marker + historical
E.fromstring(updated)
backup = ROOT/'.lab-backups/report-before-second-audit.odt'
if not backup.exists():
    shutil.copy2(path, backup)
temporary = path.with_suffix('.second-audit.odt')
with zipfile.ZipFile(temporary, 'w') as archive:
    for info, data in entries:
        archive.writestr(info, updated.encode() if info.filename == 'content.xml' else data)
with zipfile.ZipFile(temporary) as archive:
    if archive.testzip():
        raise RuntimeError('ODT archive validation failed')
    after = archive.read('content.xml').decode()
    if after.split(marker, 1)[1] != historical:
        raise RuntimeError('historical text was changed')
    for info, data in entries:
        if info.filename != 'content.xml' and archive.read(info.filename) != data:
            raise RuntimeError('non-text ODT member changed')
temporary.replace(path)
print(f'Reconciled {count} observations and run references; historical XML and all other archive members preserved.')
