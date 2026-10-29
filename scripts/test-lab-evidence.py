"""Regression checks proving the auditor rejects altered evidence; exports stay untouched."""
import copy
import importlib.util
import json
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
spec = importlib.util.spec_from_file_location('verifier', ROOT/'scripts/verify-lab-evidence.py')
verifier = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verifier)


class EvidenceChecks(unittest.TestCase):
    def check_mutation(self, protocol, scenario, mutate, expected):
        original = json.loads((ROOT/'protocols'/protocol/'experiments'/scenario/'run.json').read_text())
        mutated = copy.deepcopy(original)
        mutate(mutated)
        with tempfile.TemporaryDirectory() as temp:
            directory = Path(temp)/protocol/'experiments'/scenario
            directory.mkdir(parents=True)
            path = directory/'run.json'
            path.write_text(json.dumps(mutated))
            result = verifier.verify(path)
        self.assertFalse(result['passed'])
        self.assertTrue(any(expected in e for e in result['errors']), result)

    def test_packaged_layout_really_checks_mutex(self):
        def mutate(run):
            for e in run['events']:
                if e['type'] == 'state':
                    e['state']['role'] = 'critical'
        self.check_mutation('lamport_mutex', 'normal', mutate, 'overlapping')

    def test_vector_equivalence_uses_real_graph(self):
        def mutate(run):
            for e in run['events']:
                if e['type'] == 'state':
                    for event in e['state'].get('events', []):
                        if event['label'] == 'e':
                            event['timestamp']['vector'] = [3, 0, 1]
        self.check_mutation('logical-clocks', 'normal-six-events', mutate, 'causal graph')

    def test_paxos_prefix_is_reexecuted(self):
        def mutate(run):
            for e in run['events']:
                if e['type'] == 'state' and e['state']['applied']:
                    e['state']['values']['invented'] = 'not in the log'
        self.check_mutation('paxos-kv', 'normal', mutate, 'decided prefix')

    def test_missing_rm_does_not_pass_zero_balance(self):
        def mutate(run):
            for e in run['events']:
                if e['type'] == 'state' and e['state']['role'] == 'RM':
                    e['state']['role'] = 'missing'
        self.check_mutation('two_phase_commit', 'normal-two-transfers', mutate, 'missing participant')


if __name__ == '__main__':
    unittest.main()
