"""Recompute stored independent results without changing exported executions."""
import importlib.util
import json
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('verify', ROOT/'scripts/verify-lab-evidence.py')
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)
for row in json.loads((ROOT/'protocols/lab-evidence-index.json').read_text()):
    directory = ROOT/'protocols'/row['protocol']/'experiments'/row['scenario']
    result = verifier.verify(directory/'run.json')
    if not result['passed']:
        raise RuntimeError(result)
    path = directory/'checks.json'
    checks = json.loads(path.read_text())
    checks['independent'] = result
    path.write_text(json.dumps(checks, indent=2)+'\n')
print('Recomputed all 53 independent checks; exported executions unchanged.')
