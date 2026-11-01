import importlib.util
import json
import tempfile
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location('metrics_runtime', Path(__file__).with_name('metrics-runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class RuntimeTest(unittest.TestCase):
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
