"""Recompute properties from immutable exports, not stored pass/fail flags.

Accepts both .lab-evidence/<protocol>/<scenario>/run-<id>.json and the
submitted protocols/<protocol>/experiments/<scenario>/run.json layout.
"""
import json
import sys
from collections import defaultdict
from pathlib import Path


def vector_before(a, b):
    return len(a) == len(b) and all(x <= y for x, y in zip(a, b)) and any(x < y for x, y in zip(a, b))


def verify(path):
    path = Path(path)
    run = json.loads(path.read_text())
    events = run['events']
    folder = path.parent.parent.name
    if folder == 'experiments':
        folder = path.parent.parent.parent.name
    scenario = path.parent.name
    errors, checks = [], {}
    members = [f'node-{i}' for i in range(1, run['config']['nodeCount'] + 1)]
    if run['config']['runtime'] != 'docker':
        errors.append('not a Docker execution')
    schemas = {e['node'] for e in events if e['type'] == 'input_schema'}
    if not set(members) <= schemas:
        errors.append('missing real input schemas for one or more nodes')
    if not any(e['type'] == 'runtime' and e.get('execution') == 'go' for e in events):
        errors.append('no Go runtime evidence')
    if not events or len(events) != run.get('eventCount'):
        errors.append('incomplete export event count')
    if any(a['seq'] >= b['seq'] for a, b in zip(events, events[1:])):
        errors.append('events not in strictly increasing sequence order')
    states = {e['node']: e['state'] for e in events if e['type'] == 'state'}
    requests = {e['payload']['rpcId']: e for e in events if e['type'] == 'send' and e.get('payload', {}).get('type') == 'RPCRequest'}
    responses = [e for e in events if e['type'] == 'send' and e.get('payload', {}).get('type') == 'RPCResponse']

    if folder in ('lamport_mutex', 'ricart-agrawala'):
        critical, maximum, entries, requesters = {}, 0, 0, []
        for e in events:
            if e['type'] == 'node' and not e.get('online', True):
                critical[e['node']] = False
            if e['type'] == 'state':
                s = e['state']
                was = critical.get(e['node'], False)
                critical[e['node']] = s.get('role') == 'critical'
                if critical[e['node']] and not was:
                    entries += 1
                    requesters.append([e['node'], s['myTS']])
                    if s['acksHave'] != len(members) - 1:
                        errors.append('entry without all peer permissions')
            maximum = max(maximum, sum(critical.values()))
        checks.update(maximumCriticalNodes=maximum, observedEntries=entries, entryOrder=requesters)
        if maximum > 1:
            errors.append('overlapping critical-section state reports')
        if scenario in ('normal', 'normal-contention') and len(requesters) == 2:
            priorities = [(ts, int(node.split('-')[1])) for node, ts in requesters]
            if priorities != sorted(priorities):
                errors.append('higher-priority request did not enter first')
        if scenario.startswith('jitter-'):
            sent = {e['id']: e['seq'] for e in events if e['type'] == 'send'}
            previous, reversals = {}, 0
            for e in events:
                if e['type'] != 'deliver':
                    continue
                link = (e['from'], e['to'])
                seq = sent[e['id']]
                reversals += int(seq < previous.get(link, 0))
                previous[link] = max(seq, previous.get(link, 0))
            checks['actualLinkOrderReversals'] = reversals
            if scenario == 'jitter-unordered' and not reversals:
                errors.append('unordered experiment did not actually reorder any messages')
            if scenario == 'jitter-fifo' and reversals:
                errors.append('FIFO experiment reordered messages')

    if folder == 'logical-clocks':
        history = {}
        for e in events:
            if e['type'] == 'state':
                for v in e['state'].get('events', []):
                    if v['id'] in history and history[v['id']] != v:
                        errors.append('an event ID was reused or its timestamp mutated')
                    history[v['id']] = v
        records = list(history.values())
        graph = {e['id']: set() for e in records}
        for node in members:
            local = sorted((e for e in records if e['node'] == node), key=lambda e: int(e['id'].rsplit(':', 1)[1]))
            for a, b in zip(local, local[1:]):
                graph[a['id']].add(b['id'])
        sends = {e['messageID']: e for e in records if e['kind'] == 'send'}
        broken_edges = []
        for b in records:
            if b['kind'] != 'receive':
                continue
            a = sends.get(b.get('messageID'))
            if a is None:
                errors.append('receive has no recorded send')
                continue
            graph[a['id']].add(b['id'])
            if not vector_before(a['timestamp']['vector'], b['timestamp']['vector']) or a['timestamp']['lamport'] >= b['timestamp']['lamport']:
                broken_edges.append([a['id'], b['id']])
        reach = {}
        for key in graph:
            seen, pending = set(), list(graph[key])
            while pending:
                child = pending.pop()
                if child not in seen:
                    seen.add(child)
                    pending.extend(graph[child])
            reach[key] = seen
        violations, mismatches, pairs = [], [], 0
        for a in records:
            stamp = a['timestamp']
            if stamp['total'] != len(members) * stamp['lamport'] + members.index(a['node']):
                errors.append('total-order encoding disagrees with recorded event')
            for b in records:
                causal = b['id'] in reach[a['id']]
                vector = vector_before(stamp['vector'], b['timestamp']['vector'])
                pairs += int(vector)
                if vector != causal:
                    mismatches.append([a['id'], b['id']])
                if causal and stamp['lamport'] >= b['timestamp']['lamport']:
                    violations.append([a['id'], b['id']])
        checks.update(events=len(records), causalPairs=pairs, causalOrderViolations=violations,
                      vectorGraphMismatches=mismatches, brokenSendReceiveEdges=broken_edges)
        if scenario == 'variant-no-merge':
            if not broken_edges:
                errors.append('broken merge variant did not break a causal edge')
        elif violations or broken_edges or mismatches:
            errors.append('timestamps disagree with actual local/message causal graph')

    if folder == 'paxos-kv':
        chosen, conflicts, voters, points = {}, set(), defaultdict(set), {}
        state_slots, applied, values = {}, {}, {}
        for e in events:
            if e['type'] != 'state':
                continue
            node, s = e['node'], e['state']
            for number, slot in s.get('log', {}).items():
                value = json.dumps(slot['value'], sort_keys=True)
                key = (number, slot['accepted'], value)
                # A learner's Decide report is not an acceptor vote. Count only
                # the accept reports before learning the decision for this slot.
                if slot['accepted'] and not slot['decided']:
                    voters[key].add(node)
                    if len(voters[key]) > len(members) // 2:
                        if number not in points:
                            points[number] = {'seq': e['seq'], 'timeMs': e['time'], 'ballot': slot['accepted'], 'acceptors': sorted(voters[key])}
                        if number in chosen and chosen[number] != value:
                            conflicts.add(number)
                        chosen[number] = value
                if slot['decided']:
                    if number in state_slots and state_slots[number] != value:
                        conflicts.add(number)
                    state_slots[number] = value
            model, done = {}, set()
            for number in range(1, s['applied'] + 1):
                slot = s['log'].get(str(number), {})
                if not slot.get('decided'):
                    errors.append('Paxos applied across an undecided hole')
                    break
                op = slot['value']
                if op['id'] not in done and op['kind'] == 'put':
                    model[op['key']] = op['value']
                done.add(op['id'])
            if model != s['values']:
                errors.append('KV state differs from executing the decided prefix')
            last = s.get('lastOperation')
            if s.get('phase') == 'completed' and last and last.get('slot', 0) > s['applied']:
                errors.append('client success before ordered execution')
            applied[node], values[node] = s['applied'], s['values']
        checks.update(decidedSlots=len(state_slots), conflictingSlots=sorted(conflicts), chosenPoints=points,
                      finalApplied=applied, prefixExecutionChecked=True)
        for number, value in state_slots.items():
            if not any(slot == number and candidate == value and len(nodes) > len(members)//2 for (slot, ballot, candidate), nodes in voters.items()):
                errors.append(f'decided slot {number} has no recorded majority of acceptors')
        if scenario == 'variant-amnesia':
            if not conflicts:
                errors.append('amnesia variant did not produce conflicting decisions')
        elif conflicts:
            errors.append('Paxos chose conflicting values')
        if scenario in ('normal', 'competing-writes', 'crash-catchup', 'partition'):
            if len(set(applied.values())) != 1 or len({json.dumps(v, sort_keys=True) for v in values.values()}) != 1:
                errors.append('live replicas did not catch up to the same applied state')

    if folder == 'two_phase_commit':
        outcomes, participant_outcomes = {}, {}
        for e in events:
            if e['type'] != 'state':
                continue
            s = e['state']
            txn = s.get('txn')
            if not txn:
                continue
            if s['role'] == 'TM' and s.get('decision'):
                old = outcomes.get(txn)
                if old and old != s['decision']:
                    errors.append('coordinator changed its transaction decision')
                outcomes[txn] = s['decision']
                if s['decision'] == 'commit' and set(s.get('votes', {}).values()) != {'yes'} and s.get('votes'):
                    errors.append('committed despite a NO vote')
            if s['role'] == 'RM' and s['state'] in ('committed', 'aborted'):
                participant_outcomes[(e['node'], txn)] = s['state']
        for (node, txn), outcome in participant_outcomes.items():
            if outcomes.get(txn) and outcome != {'commit': 'committed', 'abort': 'aborted'}[outcomes[txn]]:
                errors.append(f'{node} disagreed with decision for {txn}')
        balances = {n: s['balance'] for n, s in states.items() if s.get('role') == 'RM'}
        checks.update(balances=balances, totalBalance=sum(balances.values()), transactionDecisions=outcomes)
        if len(balances) != len(members) - 1:
            errors.append('missing participant balance')
        if scenario == 'variant-no-prepare-log':
            if sum(balances.values()) == 100 * len(balances):
                errors.append('unsafe variant did not break conservation')
        elif sum(balances.values()) != 100 * len(balances):
            errors.append('final total balance not conserved')
        if scenario == 'normal-two-transfers':
            if len(outcomes) != 2 or any(v != 'commit' for v in outcomes.values()):
                errors.append('normal report requires two committed transfers')
            for txn in outcomes:
                if any(participant_outcomes.get((n, txn)) != 'committed' for n in members[1:]):
                    errors.append('not every participant committed each normal transfer')

    if folder == 'christian':
        samples = [e['result'] for e in events if e['type'] == 'command_result' and isinstance(e.get('result'), dict) and 'rttMs' in e['result']]
        for s in samples:
            fraction = {'half': .5, 'zero': 0, 'full': 1}[s['mode']]
            if abs(s['assumedOneWayMs'] - fraction * s['rttMs']) > .2 or abs(s['estimateMs'] - s['tsMs'] - s['assumedOneWayMs']) > .3:
                errors.append('Cristian arithmetic disagrees with recorded timestamps')
            if abs(s['correctionMs'] - (s['estimateMs'] - s['t1Ms'])) > .3:
                errors.append('Cristian applied wrong correction')
        checks['timestampCalculationsChecked'] = len(samples)
        if not samples:
            errors.append('no successful synchronization sample')

    if folder == 'berkeley':
        rounds = {}
        for e in events:
            if e['type'] == 'state' and e['state'].get('lastRound'):
                r = e['state']['lastRound']
                rounds[r['number']] = r
        for r in rounds.values():
            included = [s for s in r['samples'] if s['included']]
            if len(included) >= 2:
                mean = sum(s['offsetMS'] for s in included) / len(included)
                if abs(mean - r['meanMS']) > .001:
                    errors.append('Berkeley mean does not match retained samples')
                for sample in r['samples']:
                    if sample['scheduled'] and abs(sample['deltaMS'] - (mean - sample['offsetMS'])) > .001:
                        errors.append('Berkeley adjustment sign or amount incorrect')
        checks['filteredRoundsChecked'] = len(rounds)
        if scenario == 'variant-jump':
            pairs = [(e['state'], e['seq']) for e in events if e['type'] == 'state' and 'clockBeforeAdjustmentMS' in e['state']]
            jumps = [seq for s, seq in pairs if s['stepMode'] and s['clockAfterAdjustmentMS'] < s['clockBeforeAdjustmentMS']]
            checks['backwardAdjustmentReports'] = jumps
            if not jumps:
                errors.append('no measured backward jump; an adjustment amount alone is insufficient')
            if any(s['clockAfterAdjustmentMS'] != s['clockBeforeAdjustmentMS'] for s, _ in pairs if not s['stepMode']):
                errors.append('smooth correction changed time at its scheduling instant')
        if scenario == 'crash-master':
            recovered = [e for e in events if e['type'] == 'fault' and e.get('fault', {}).get('kind') == 'recover']
            if not recovered:
                errors.append('Berkeley Chinese requirement includes coordinator recovery')
            else:
                boundary = recovered[-1]['seq']
                before = max((e['state']['lastAppliedRound'] for e in events if e['type'] == 'state' and e['seq'] < boundary), default=0)
                if any(s['lastAppliedRound'] <= before for s in states.values()):
                    errors.append('periodic rounds did not resume at every node')
                checks['recoveredCoordinatorResumed'] = not any(s['lastAppliedRound'] <= before for s in states.values())

    if folder == 'bully_election':
        counts = defaultdict(int)
        for e in requests.values():
            if e['payload']['method'] in ('ElectionService.Election', 'ElectionService.Coordinator'):
                counts[e['payload']['method'].split('.')[-1]] += 1
        counts['OK'] = sum(e['payload'].get('method') == 'ElectionService.Election' and e['payload'].get('body', {}).get('ok') is True for e in responses)
        checks['algorithmMessages'] = dict(counts)
        checks['algorithmMessageTotal'] = sum(counts.values())
        if scenario == 'normal-startup' and (not all(counts.get(k) for k in ('Election', 'Coordinator', 'OK')) or any(s['coordinator'] != 'node-5' for s in states.values())):
            errors.append('startup did not demonstrate all three messages and highest-node election')

    if folder == 'quorum-kv':
        previous = {}
        for e in events:
            if e['type'] != 'state':
                continue
            for key, item in e['state']['values'].items():
                tag = (item['version']['counter'], item['version']['writer'])
                identity = (e['node'], key)
                if tag < previous.get(identity, (0, '')):
                    errors.append('replica version moved backwards')
                previous[identity] = tag
        checks['monotonicReplicaVersionsChecked'] = len(previous)
        if scenario == 'slow-replica':
            puts = [e['state']['lastPut'] for e in events if e['type'] == 'state' and e['state'].get('lastPut') and e['state']['lastPut']['success']]
            unique = {p['item']['version']['counter']: p for p in puts}
            times = [p['waitedMs'] for p in unique.values()]
            checks['writeTimesMs'] = times
            if len(times) < 2 or max(times) - min(times) < 2000:
                errors.append('slow replica cost not demonstrated')
    checks.update(runId=run['id'], passed=not errors, errors=sorted(set(errors)))
    return checks


if __name__ == '__main__':
    root = Path(sys.argv[1] if len(sys.argv) > 1 else 'protocols')
    paths = sorted(root.glob('*/experiments/*/run.json'))
    if not paths:
        for directory in sorted(p for p in root.glob('*/*') if p.is_dir()):
            candidates = [p for p in directory.glob('run-*.json') if json.loads((directory / ('checks-' + p.name[4:])).read_text())['passed']]
            if candidates:
                paths.append(max(candidates, key=lambda p: p.stat().st_mtime))
    failed = not paths
    for path in paths:
        result = verify(path)
        print(path.parent.parent.name, path.parent.name, json.dumps(result))
        failed |= not result['passed']
    sys.exit(int(failed))
