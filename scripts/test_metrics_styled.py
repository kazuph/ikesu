import importlib.util
import tempfile
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location('metrics_runtime', Path(__file__).with_name('metrics-runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)


class StyledMetricsTest(unittest.TestCase):
    def test_six_stages_and_evidence(self):
        with tempfile.TemporaryDirectory() as root:
            session = runtime.StyledMemorySession('/bin/true', '/bin/true', 'ikesu', root)
            steps = session.steps.read_text()
            for index, size in enumerate((2, 2, 4, 8, 16, 32)):
                self.assertIn(f'DONE_m{size}_{index}', steps)
            self.assertEqual(steps.count('sleep 2s'), 6)
            self.assertIn('ペイン', (session.root / 'styled-emit.py').read_text())

    def test_median_requires_three_successes(self):
        ledger = runtime.new_ledger({})
        metric = next(m for m in ledger['metrics'] if m['key'] == 'rss.styled.increment')
        for index, output in enumerate((30, 50, 40)):
            result = {'product': 'ikesu', 'status': 'ok',
                      'idle': {'rss_kib': {'total': 10}},
                      'output': {'rss_kib': {'total': output}}}
            runtime.store_styled_memory_result(ledger, index, result)
        self.assertEqual(metric['median']['ikesu'], 30)
        runtime.store_styled_memory_result(ledger, 2, {'product': 'ikesu', 'status': 'failed',
                                                       'error': 'output timed out'})
        self.assertIsNone(metric['median']['ikesu'])


if __name__ == '__main__':
    unittest.main()
