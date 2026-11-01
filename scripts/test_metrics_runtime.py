import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location('metrics_runtime', Path(__file__).with_name('metrics-runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class RuntimeTest(unittest.TestCase):
    def test_ledger_keeps_unfinished_runs_and_three_raw_values(self):
        ledger = runtime.new_ledger({'host': 'mini'})
        metric = next(m for m in ledger['metrics'] if m['key'] == 'startup')
        self.assertEqual(len(metric['samples']['ikesu']), 3)
        self.assertEqual(metric['samples']['ikesu'][0]['status'], 'not_started')
        for i, value in enumerate((3, 1, 2)):
            runtime.record_sample(ledger, 'server', 'startup', 'ikesu', i,
                                  {'status': 'ok', 'value': value})
        self.assertEqual(metric['median']['ikesu'], 2)
        self.assertEqual([s['value'] for s in metric['samples']['ikesu']], [3, 1, 2])
        self.assertIn('rss_slope.output.10.20.total', [m['key'] for m in ledger['metrics']])
        self.assertIn('heap.output.20', [m['key'] for m in ledger['metrics']])

    def test_cpu_interval_and_per_pane_increase(self):
        start = {'ps_start_ns': 0, 'ps_end_ns': 0,
                 'processes': [{'pid': 4, 'cpu_seconds': 10}, {'pid': 5, 'cpu_seconds': 3}]}
        end = {'ps_start_ns': 60_000_000_000, 'ps_end_ns': 60_000_000_000,
               'processes': [{'pid': 4, 'cpu_seconds': 11.2}, {'pid': 5, 'cpu_seconds': 3.6}]}
        result = runtime.cpu_between(start, end)
        self.assertAlmostEqual(result['percent_one_core'], 3.0)
        self.assertEqual(runtime.pane_increment(100, 190, 1, 10), 10)
        end['processes'].pop()
        with self.assertRaises(ValueError):
            runtime.cpu_between(start, end)

    def test_latency_matches_first_later_frame_and_marks_missing_or_outside_flood(self):
        events = [{'type': 'capacity.reply', 'token': 'CAPRX000!', 'clock_ns': 5},
                  {'type': 'capacity.input', 'data': 'CAPRX000!', 'clock_ns': 10},
                  {'type': 'capacity.reply', 'token': 'CAPRX000!', 'clock_ns': 15},
                  {'type': 'capacity.reply', 'token': 'CAPRX000!', 'clock_ns': 16},
                  {'type': 'capacity.input', 'data': 'CAPRX001!', 'clock_ns': 20}]
        samples = runtime.latency_from_events(events, 3, flood_window=(8, 18))
        self.assertEqual([s['status'] for s in samples], ['ok', 'timeout', 'not_sent'])
        self.assertEqual(samples[0]['observed_ns'], 15)
        events.append({'type': 'capacity.reply', 'token': 'CAPRX001!', 'clock_ns': 25})
        self.assertEqual(runtime.latency_from_events(events, 3, (8, 18))[1]['status'],
                         'outside_flood')

    def test_process_accounting_excludes_shell_and_counts_all_clients(self):
        rows = runtime.parse_ps('''100 1 1000 00:01.00 /tmp/mux --session bench
101 100 2000 00:02.50 /tmp/mux server
102 101 3000 00:00.20 /bin/zsh
103 101 4000 01:02.00 /tmp/mux --session bench
104 1 9999 00:03.00 /tmp/mux server
''')
        found = runtime.product_processes(rows, 100, '/tmp/mux', 'server')
        self.assertEqual([(p['pid'], p['role']) for p in found],
                         [(100, 'client'), (101, 'server'), (103, 'client')])
        self.assertEqual(sum(p['rss_kib'] for p in found), 7000)
        self.assertEqual(found[1]['cpu_seconds'], 2.5)
        self.assertEqual(runtime.parse_cpu_time('1-02:03:04.50'), 93784.5)

    def test_latencies_keep_failures_and_do_not_report_partial_percentiles(self):
        samples = [{'status': 'ok', 'seconds': i / 1000} for i in range(1, 101)]
        summary = runtime.latency_summary(samples)
        self.assertEqual((summary['sent'], summary['observed'], summary['failed']), (100, 100, 0))
        self.assertEqual((summary['p50_seconds'], summary['p99_seconds']), (.05, .099))
        samples[-1] = {'status': 'timeout'}
        summary = runtime.latency_summary(samples)
        self.assertEqual((summary['sent'], summary['observed'], summary['failed']), (100, 99, 1))
        self.assertNotIn('p99_seconds', summary)

    def test_record_survives_json_round_trip(self):
        samples = [{'status': 'ok', 'value': n} for n in (9, 1, 5)]
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'run.json'
            runtime.save_json(path, {'samples': samples, 'median': runtime.median_three(samples)})
            self.assertEqual(json.loads(path.read_text())['median'], 5)
            self.assertEqual(json.loads(path.read_text())['samples'], samples)
        self.assertIsNone(runtime.median_three(samples[:2]))
        self.assertIsNone(runtime.median_three(samples[:2] + [{'status': 'failed'}]))


if __name__ == '__main__':
    unittest.main()
