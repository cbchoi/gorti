"""Offline reanalysis of original Table 4/5/6 and the separate SDK comparison."""
from __future__ import annotations

import argparse
import ast
from collections import Counter
import hashlib
import importlib.util
import json
from pathlib import Path
import random
import statistics
import struct
import sys
import xml.etree.ElementTree as ET

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parent
sys.path.insert(0, str(ROOT / 'analysis'))
from retained_estimator_functions import analyze_crossover, analyze_portico_crossover, make_schedule
from verify_package import verify


def read(path):
    return json.loads(path.read_text(encoding='utf-8-sig'))


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def require(ok, message):
    if not ok:
        raise ValueError(message)


def module(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


def report(path):
    rows, starts = [], Counter()
    if path.suffix == '.jsonl':
        for number, line in enumerate(path.read_text(encoding='utf-8').splitlines(), 1):
            event = json.loads(line)
            name = event.get('Test', '')
            identity = event.get('Package', '') + '::' + name
            if event['Action'] == 'run' and name:
                starts[identity] += 1
            if event['Action'] in ('pass', 'fail', 'skip') and name:
                rows.append({'identity': identity, 'status': event['Action'], 'subtest': '/' in name, 'line': number})
        require(starts == Counter(r['identity'] for r in rows), 'Unfinished/duplicate Go invocation')
    else:
        for number, case in enumerate(ET.parse(path).getroot().iter('testcase'), 1):
            status = 'fail' if case.find('failure') is not None or case.find('error') is not None else 'skip' if case.find('skipped') is not None else 'pass'
            rows.append({'identity': case.get('classname', '') + '::' + case.get('name', ''), 'status': status, 'subtest': False, 'testcase_ordinal': number})
    require(len(rows) == len({r['identity'] for r in rows}), 'Duplicate terminal test identity')
    return rows


def table4(out, provenance):
    base = ROOT / 'historical'
    validation = base / 'validation'
    reports = {p.name: report(p) for p in validation.rglob('*') if p.suffix in {'.xml', '.jsonl'}}
    names = {'go_default': ['go.jsonl'], 'go_async': ['go-async.jsonl'], 'python': ['python.xml'],
             'java': sorted(n for n in reports if n.startswith('TEST-')), 'ivct': ['ivct.xml']}
    expected = {'go_default': (3533, 19), 'go_async': (972, 0), 'python': (2127, 0), 'java': (581, 7), 'ivct': (35, 0)}
    results = {}
    for scope, files in names.items():
        rows = [r for n in files for r in reports[n] if not r['subtest']]
        counts = Counter(r['status'] for r in rows)
        require((counts['pass'], counts['skip']) == expected[scope] and counts['fail'] == 0, 'Table 4 mismatch: ' + scope)
        results[scope] = {'pass': counts['pass'], 'skip': counts['skip'], 'fail': counts['fail'],
                          'skipped_identities': [r['identity'] for r in rows if r['status'] == 'skip']}
    default_skips = set(results['go_default']['skipped_identities'])
    async_pass = {r['identity'] for r in reports['go-async.jsonl'] if r['status'] == 'pass' and not r['subtest']}
    require(len(default_skips & async_pass) == 16, 'Async skip resolution mismatch')
    data = read(base / 'traceability/reconciled-input.json')
    classes = read(base / 'source/engineering/verification/requirements2010-evidence-classes-20261023.json')
    exporter = module('historical_exporter', base / 'source/scripts/summarize_requirement_results.py')
    summary, invocations, statuses = exporter.summarize(data, classes)
    summary.update(reconciliation_sha256=provenance['historical/traceability/reconciled-input.json']['original_sha256'],
                   classification_sha256=provenance['historical/source/engineering/verification/requirements2010-evidence-classes-20261023.json']['original_sha256'])
    require(summary == read(base / 'traceability/requirements-summary.json'), 'Historical mapping summary mismatch')
    tables = out / 'historical-mapping'
    tables.mkdir()
    for name, rows in [('requirement-invocations.csv', invocations), ('requirement-status.csv', statuses)]:
        exporter.write_csv(tables / name, rows)
        require((tables / name).read_bytes() == (base / 'traceability' / name).read_bytes(), 'Historical CSV mismatch: ' + name)
    (tables / 'requirements-summary.json').write_text(json.dumps(summary, indent=2) + '\n', encoding='utf-8')
    require(len(data['requirements']) == 712 and len(invocations) == 1048, 'Inventory size mismatch')
    artifact_count = 0
    for artifact in data['execution_artifacts']:
        files = [p for p in validation.rglob(artifact['filename'])
                 if provenance[p.relative_to(ROOT).as_posix()]['original_sha256'] == artifact['sha256']]
        require(len(files) == 1, 'Original report identity unresolved')
        artifact_count += 1
    lookup = {(n, r['identity']): r['status'] for n, rows in reports.items() for r in rows}
    for row in invocations:
        filename = row['artifact'].replace('\\', '/').rsplit('/', 1)[-1]
        require(lookup.get((filename, row['invocation'])) == row['status'], 'Linked invocation differs from raw report')
    links = sum(len(r['tests']) for r in data['requirements'])
    targets = {(t['path'], t['name']) for r in data['requirements'] for t in r['tests']}
    require((links, len(targets), len({p for p, n in targets})) == (877, 464, 205), 'Link/target/file counts mismatch')
    partition = Counter(r['applicability'] for r in data['requirements'])
    return {'suites': results, 'async_skips_resolved': 16,
            'go_passing_subtests_separate': {n: sum(r['status'] == 'pass' and r['subtest'] for r in reports[n]) for n in ('go.jsonl', 'go-async.jsonl')},
            'historical_mapping': {'records': 712, 'links': links, 'targets': len(targets), 'linked_source_files': 205,
                'invocation_occurrences': len(invocations), 'distinct_artifact_invocations': len({(r['artifact'], r['invocation']) for r in invocations}),
                'linked_original_report_hashes': artifact_count, 'partition': dict(partition),
                'regenerated_tables_match_public_historical_copies': True,
                'hash_fields_refer_to_original_bytes_not_redacted_bytes': True}}


def table5(provenance):
    base = ROOT / 'historical/model'
    saved = read(base / 'summary.json')
    rng = random.Random(1516)
    services = {'balanced': [1] * 100, 'bottleneck': [2] * 100, 'variable': [rng.randint(1, 3) for _ in range(100)]}
    expected = {'balanced': (102, 0, 1), 'bottleneck': (202, 49.5, 51.5), 'variable': (206, 49.85, 51.89)}
    rows = []
    for run in saved['results']:
        name = f"{run['profile']}-{run['repeat']}"
        path = base / name
        inputs = read(path / 'inputs.json')
        require(inputs['services'] == services[run['profile']] and inputs['seed'] == 1516 and inputs['transfer_delay'] == 1, 'Model inputs mismatch')
        jobs, available = [], 0
        for sequence, duration in enumerate(inputs['services'], 1):
            arrival = sequence + 1
            start = max(arrival, available)
            finish = start + duration
            jobs.append(dict(sequence=sequence, arrival=arrival, service=duration, start=start, finish=finish, wait=start-arrival))
            available = finish
        go, py = read(path / 'go-result.json'), read(path / 'python-result.json')
        require(jobs == read(path / 'reference.json') == go['jobs'], 'Model trajectory mismatch')
        require(py['departures'] == [dict(sequence=r['sequence'], finish=r['finish']) for r in jobs], 'Completion mismatch')
        require(py['published'] == py['received'] == list(range(1, 101)), 'Model sequence mismatch')
        require(go['grants'] == py['grants'] == list(range(1, 304)), 'Model grants mismatch')
        checks = read(path / 'checks.json')
        require(all(checks.values()) and checks == run['checks'], 'Saved checks mismatch')
        ready = [read(path / 'go-ready.json'), read(path / 'python-ready.json')]
        require(ready == run['federates'], 'Ready identities mismatch')
        require(all(result[k] == meta[k] for result, meta in zip((go, py), ready) for k in ('pid', 'handle')), 'Process identities mismatch')
        require(len({go['pid'], py['pid'], run['server_pid']}) == 3 and go['handle'] != py['handle'], 'Participant separation mismatch')
        metrics = {'jobs': len(jobs), 'final_departure': jobs[-1]['finish'], 'mean_wait': statistics.mean(r['wait'] for r in jobs),
                   'mean_sojourn': statistics.mean(r['finish']-r['arrival'] for r in jobs)}
        require(all(metrics[k] == run[k] for k in metrics), 'Model summary mismatch')
        require(tuple(metrics[k] for k in ('final_departure', 'mean_wait', 'mean_sojourn')) == expected[run['profile']], 'Table 5 mismatch')
        require(run['max_time_error'] == 0, 'Nonzero saved time error')
        rows.append({'run': name, **metrics, 'trajectory_mismatch': 0, 'data_callbacks': len(jobs)+len(py['departures'])})
    for name, expected_hash in saved['source_sha256'].items():
        candidates = [p for p in ('historical/source/' + name, 'source-context/assessed-base/' + name) if p in provenance]
        require(any(provenance[p]['original_sha256'] == expected_hash for p in candidates), 'Model source provenance mismatch')
    require(len(rows) == 9, 'Expected nine model runs')
    return {'runs': rows, 'total_jobs': sum(r['jobs'] for r in rows), 'total_data_callbacks': sum(r['data_callbacks'] for r in rows),
            'matching_source_manifest_files': len(saved['source_sha256']), 'binary_reverification': 'not performed; binaries intentionally excluded'}


def mapping_corrections(provenance):
    root = ROOT / 'mapping-corrections'
    status = read(root / 'STATUS.json')
    if not status['included']:
        return status
    summary = read(root / 'summary.json')
    aliases = read(root / 'path_aliases.json')
    bindings = read(root / 'public-path-aliases.json')['roots_relative_to_package']
    alias_count = 0
    for kind in ('sources', 'reports'):
        for item in aliases[kind]:
            prefix, relative = item['alias'].split('/', 1)
            path = ROOT / bindings[prefix] / relative
            require(path.is_file(), 'Unresolved public mapping alias')
            entry = provenance[path.relative_to(ROOT).as_posix()]
            require(entry['original_sha256'] == item['sha256'] and digest(path) == entry['public_sha256'], 'Mapping alias original/public hash mismatch')
            alias_count += 1
    execution = read(root / 'execution_reuse.json')
    ledger = read(root / 'change_ledger.json')['entries']
    old, new = {}, {}
    for p in sorted((root / 'inventories').glob('*.json')):
        new.update({r['id']: r for r in read(p)['requirements']})
        old.update({r['id']: r for r in read(root / 'history/before_inventories' / p.name)['requirements']})
    require(len(old) == len(new) == 712 and old.keys() == new.keys(), 'Corrected mapping record identity mismatch')
    for identifier in old:
        for field in ('id', 'source_id', 'clause', 'pdf_pages', 'subject', 'applicability', 'statement', 'conditions', 'review_status', 'implementation'):
            require(old[identifier][field] == new[identifier][field], 'Mapping changed preserved field')
        require(bool(new[identifier]['open_items']), 'Mapping closed open items')
    pointers = 0
    for p in sorted((root / 'inventories').glob('*.json')):
        for row in read(p)['requirements']:
            for key, reference in row.get('revision_assessment', {}).items():
                if not key.endswith('_ref'):
                    continue
                filename, pointer = reference.split('#', 1)
                path = (p.parent / filename).resolve()
                require(path.is_relative_to(root.resolve()), 'Mapping reference leaves package subtree')
                value = read(path)
                for token in pointer.lstrip('/').split('/'):
                    token = token.replace('~1', '/').replace('~0', '~')
                    value = value[int(token)] if isinstance(value, list) else value[token]
                require(value is not None, 'Empty mapping reference')
                pointers += 1
    changed = {k for k in old if old[k] != new[k]}
    require(changed == {e['id'] for e in ledger}, 'Mapping ledger incomplete')
    for entry in ledger:
        require(entry['before'] == old[entry['id']] and entry['after'] == new[entry['id']], 'Mapping ledger row mismatch')
    active = execution['active_links']
    invocations = [i for link in active for i in link['invocations']]
    target_links = {(r['id'], t['path'], t['name']) for r in new.values() for t in r['tests']}
    require(target_links == {(l['requirement_id'], l['path'], l['name']) for l in active}, 'Corrected active links differ from inventory')
    reports = {p.name: (p, report(p)) for p in (ROOT / 'historical/validation').rglob('*') if p.suffix in {'.xml', '.jsonl'}}
    original = read(ROOT / 'historical/traceability/reconciled-input.json')
    source_hashes = {t['path']: t['source_sha256_at_review'] for r in original['requirements'] for t in r['test_evidence']}
    verified = 0
    for link in active + execution['related_links']:
        require(source_hashes[link['path']] == link['source_sha256'], 'Corrected link source hash differs from historical source')
        for invocation in link['invocations']:
            path, rows = reports[invocation['artifact']]
            require(provenance[path.relative_to(ROOT).as_posix()]['original_sha256'] == invocation['artifact_sha256'], 'Corrected link artifact identity mismatch')
            locator = invocation['raw_locator']
            require(any(r['identity'] == invocation['identity'] and r['status'] == invocation['status']
                        and all(r.get(k) == locator[k] for k in ('line', 'testcase_ordinal') if k in locator)
                        for r in rows), 'Corrected link raw outcome/locator mismatch')
            verified += 1
    statuses = Counter()
    for identifier, r in new.items():
        if r['applicability'] != 'applicable':
            continue
        links = [l for l in active if l['requirement_id'] == identifier]
        statuses['no-supporting-link' if not links else 'with-skip' if any(i['status'] == 'skip' for l in links for i in l['invocations']) else 'passing-linked-records-only'] += 1
    observed = {'records': len(new), 'active_links': len(active), 'distinct_active_targets': len({(l['path'], l['name']) for l in active}),
                'linked_invocation_occurrences': len(invocations), 'distinct_artifact_invocation_identities': len({(i['artifact'], i['identity']) for i in invocations}),
                'applicable_execution_statuses': dict(statuses), 'invocation_occurrence_statuses': dict(Counter(i['status'] for i in invocations))}
    require(all(summary['corrected'][k] == v for k, v in observed.items()), 'Corrected summary mismatch')
    require(len(changed) == summary['corrected_rows'], 'Changed row count mismatch')
    require(summary['requirements_closed'] == summary['new_test_campaigns'] == summary['human_signoffs_added'] == 0, 'Mapping claims unexpected closure or new runs')
    return {**status, 'corrected_counts': observed, 'changed_rows': len(changed),
            'related_non_supporting_links': len(execution['related_links']), 'active_and_related_raw_invocations_checked': verified,
            'source_and_report_aliases_resolved': alias_count,
            'inventory_json_pointers_resolved': pointers,
            'preserved_statements_conditions_applicability': True, 'historical_table4_unchanged': True}


def plan(path):
    raw = path.read_bytes()
    magic, count, seed, topology = struct.unpack_from('>8sIQ32s', raw)
    require(magic == b'DVSHLA1\0' and len(raw) == 52 + count * 32 and count == 40600, 'Plan format/count mismatch')
    a, i, trace = hashlib.sha256(), hashlib.sha256(), hashlib.sha256()
    for ordinal, (index, event, node, occurrence, av, iv) in enumerate(struct.iter_unpack('>IIII8s8s', raw[52:])):
        require(index == ordinal, 'Plan sequence mismatch')
        prefix = struct.pack('>I', index)
        av, iv = av.hex().encode('ascii'), iv.hex().encode('ascii')
        a.update(prefix + av)
        i.update(prefix + iv)
        trace.update(b'A' + prefix + av + b'I' + prefix + iv)
    return {'sha256': digest(path), 'seed': seed, 'topology_sha256': topology.hex(),
            'attribute_callback_sha256': a.hexdigest(), 'interaction_callback_sha256': i.hexdigest(), 'callback_trace_sha256': trace.hexdigest()}


def local_trial(root, recorded_path):
    suffix = recorded_path.replace('\\', '/').split('/block-', 1)
    require(len(suffix) == 2, 'Cannot locate trial relative to extraction root')
    path = root / ('block-' + suffix[1])
    require(path.resolve().is_relative_to(root.resolve()), 'Trial path escapes campaign')
    return path


def performance():
    extracted = ast.parse((ROOT / 'analysis/retained_estimator_functions.py').read_text())
    functions = {n.name: ast.dump(n, include_attributes=False) for n in extracted.body if isinstance(n, ast.FunctionDef)}
    for filename, names in {'compare.py': ('semantics',), 'crossover.py': ('make_schedule', 'analyze_crossover'),
                            'compare_portico.py': ('analyze_portico_crossover',)}.items():
        source = ROOT / 'source-context/retained/benchmark/callback-reporting' / filename
        original = {n.name: ast.dump(n, include_attributes=False) for n in ast.parse(source.read_text()).body if isinstance(n, ast.FunctionDef)}
        require(all(functions[n] == original[n] for n in names), 'Retained estimator differs from original analysis source')
    campaigns, all_plans = {}, []
    for name in ('session1', 'session2', 'sdk'):
        root = ROOT / 'performance' / name
        rows, schedule, manifest = (read(root / n) for n in ('results.json', 'schedule.json', 'manifest.json'))
        require(len(rows) == 100 and len(schedule) == 25, 'Campaign count mismatch')
        require(schedule == make_schedule(5, 20, manifest['order_seed']), 'Schedule mismatch')
        require(digest(root / 'schedule.json') == manifest['schedule_sha256'], 'Schedule hash mismatch')
        require(digest(root / 'workload.json') == manifest['workload_sha256'], 'Workload hash mismatch')
        fom = ROOT / 'source-context/retained/verification/commercial-rti/fom/CommercialRtiVerifier.xml'
        if 'fom_sha256' in manifest:
            require(digest(fom) == manifest['fom_sha256'], 'FOM hash mismatch')
        plans = {b['seed']: plan(root / f"plan-{b['seed']}.bin") for b in schedule}
        all_plans.append(plans)
        summaries, go_count = 0, 0
        for row in rows:
            data = row if name == 'sdk' else row['result']
            is_go = name == 'sdk' or row['implementation'] == 'gorti'
            p = plans[row['seed']]
            if name == 'sdk':
                sub = row['summaries']['subscriber']
                require(sub['plan_sha256'] == p['sha256'], 'SDK plan hash mismatch')
                for sk, pk in [('attribute_arrival_order_sha256', 'attribute_callback_sha256'), ('interaction_arrival_order_sha256', 'interaction_callback_sha256'), ('callback_trace_sha256', 'callback_trace_sha256')]:
                    require(sub[sk] == p[pk], 'SDK digest mismatch')
            else:
                ev = data['evidence']
                require(data['batch_ns'] / 1e9 == row['seconds'] and ev['workload_instance_sha256'] == p['sha256'], 'Timing/plan mismatch')
                require(ev['delivered_callbacks'] == 81200, 'Callback count mismatch')
                require(all(ev[k] == 0 for k in ('rejected_callbacks', 'dropped_callbacks', 'unexpected_callbacks', 'duplicate_callbacks', 'invalid_callbacks')), 'Callback rejection')
                for key in ('attribute_callback_sha256', 'interaction_callback_sha256', 'callback_trace_sha256'):
                    require(ev[key] == p[key], 'Plan callback digest mismatch')
            if is_go:
                path = next(s.split('=', 1)[1] for s in data['server_arguments'] if s.startswith('--save-dir='))
                trial = local_trial(root, path).parent
                report_data = data['summaries']['subscriber']['callback_report_transport'] if name == 'sdk' else data['callback_report_transport']
                local = data['summaries']['publisher']['local_lrc_transport'] if name == 'sdk' else data['local_lrc_transport']
                require(report_data['LocalEntries'] == report_data['AsyncOutcomes'] == 81456 and report_data['StreamReports'] == 162934, 'Report accounting mismatch')
                require(local['Enqueued'] == local['Sent'] == local['Acked'] == 81456 and local['Failures'] == local['QueueDepth'] == 0 and local['BatchSize'] == 32, 'Local transport mismatch')
                paths = {role: trial / role / (role + '-summary.json') for role in ('publisher', 'subscriber')}
                go_count += 1
            else:
                paths = {role: local_trial(root, cmd[cmd.index('--output')+1]) / (role + '-summary.json') for role, cmd in data['commands'].items()}
            for role, path in paths.items():
                s = read(path)
                require(s['status'] == 'accepted' and s['seed'] == row['seed'], 'Participant acceptance mismatch')
                if name == 'sdk':
                    require(s == row['summaries'][role], 'Embedded/raw SDK summary mismatch')
                if role == 'subscriber':
                    require(s['completed_receive_order_batch_ns'] / 1e9 == row['seconds'], 'Raw duration mismatch')
                    acc = s['callback_accounting']
                    require(acc['delivered'] == 81200, 'Raw callback count mismatch')
                    require(all(acc[k] == 0 for k in ('rejected', 'dropped', 'unexpected', 'duplicates', 'invalid')), 'Raw callback acceptance mismatch')
                    if is_go:
                        require(s['callback_report_transport'] == report_data, 'Raw report accounting mismatch')
                summaries += 1
        comparison = analyze_crossover(rows, schedule) if name == 'sdk' else analyze_portico_crossover(rows, schedule)
        require(comparison == read(root / 'comparison.json'), 'Original estimator comparison mismatch: ' + name)
        if name != 'sdk':
            expected = {'session1': (0.920, 1.162, 88284, 69862, 0.791, 0.581, 1.045),
                        'session2': (2.272, 1.413, 35742, 57462, 1.608, 1.320, 1.924)}[name]
            values = (comparison['baseline']['mean_seconds'], comparison['candidate']['mean_seconds'],
                      comparison['baseline']['aggregate_callbacks_per_second'], comparison['candidate']['aggregate_callbacks_per_second'],
                      comparison['completion_speedup'], *comparison['speedup_seed_block_stratified_bootstrap_ci95'])
            require(tuple(round(v, p) for v, p in zip(values, (3, 3, 0, 0, 3, 3, 3))) == expected, 'Submitted Table 6 precision mismatch')
        else:
            low, high = comparison['speedup_seed_block_stratified_bootstrap_ci95']
            require((round(comparison['throughput_gain_percent'], 2), round(100*(low-1), 2), round(100*(high-1), 2)) == (4.88, 1.38, 8.84), 'Submitted SDK precision mismatch')
        require(summaries == 200, 'Participant summary count mismatch')
        campaigns[name] = {'trials': len(rows), 'warmup_trials': 20, 'measured_trials': 80,
                           'participant_summaries': summaries, 'plans': len(plans), 'go_accounting_trials': go_count,
                           'exact_saved_comparison_match': True, 'submitted_precision_match': True, 'comparison': comparison}
    require(all_plans[0] == all_plans[1] == all_plans[2], 'Cross-campaign plan mismatch')
    return campaigns


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=Path('reanalysis-output'))
    args = parser.parse_args()
    require(sys.version_info >= (3, 12), 'Use Python 3.12+; Python 3.14 was verified. Older float summation can differ.')
    integrity = verify()
    out = args.output.resolve()
    require(not out.exists(), 'Choose a new output directory')
    out.mkdir(parents=True)
    provenance = {r['path']: r for r in read(ROOT / 'provenance/original-to-public.json')}
    results = {'status': 'PASS', 'kind': 'retained-evidence reanalysis; no new experiments',
               'integrity': integrity, 'table4': table4(out, provenance), 'table5': table5(provenance),
               'performance': performance(), 'mapping_corrections': mapping_corrections(provenance)}
    (out / 'results.json').write_text(json.dumps(results, indent=2) + '\n', encoding='utf-8')
    print(json.dumps({'status': 'PASS', 'table4': {k: {c: v[c] for c in ('pass', 'skip', 'fail')} for k, v in results['table4']['suites'].items()},
                      'model_runs': len(results['table5']['runs']), 'model_jobs': results['table5']['total_jobs'],
                      'model_data_callbacks': results['table5']['total_data_callbacks'], 'performance_trials': 300,
                      'participant_summaries': 600, 'exact_comparison_matches': 3, 'manifest': integrity,
                      'sdk_gain_percent': results['performance']['sdk']['comparison']['throughput_gain_percent']}, indent=2))


if __name__ == '__main__':
    main()
