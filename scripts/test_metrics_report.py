import importlib.util
import unittest
from pathlib import Path


spec = importlib.util.spec_from_file_location('metrics_report', Path(__file__).with_name('metrics-report.py'))
report = importlib.util.module_from_spec(spec)
spec.loader.exec_module(report)


class ReportTest(unittest.TestCase):
    def test_median_and_failed_sample(self):
        values = [{'status': 'ok', 'value': n} for n in (9, 1, 5)]
        self.assertEqual(report.summarize(values), 5)
        self.assertIsNone(report.summarize(values[:2]))
        self.assertIsNone(report.summarize(values[:2] + [{'status': 'failed', 'error': 'timeout'}]))

    def test_report_shows_raw_values_and_escapes_input(self):
        data = {'metadata': {'host': '<mini>'}, 'metrics': [
            {'mode': 'server', 'name': 'RSS', 'unit': 'MiB', 'samples': {
                'ikesu': [{'status': 'ok', 'value': n} for n in (9, 1, 5)],
                'herdr': [{'status': 'failed', 'error': '<timeout>'}]}}],
                'runs': [{'product': 'ikesu', 'metric': 'latency.idle', 'round': 1,
                          'summary': {'scheduled': 100, 'sent': 99, 'observed': 98,
                                      'failed': 2, 'not_sent': 1}}]}
        page = report.render(data)
        self.assertIn('9, 1, 5', page)
        self.assertIn('<strong>5</strong>', page)
        self.assertIn('失敗: &lt;timeout&gt;', page)
        self.assertIn('&lt;mini&gt;', page)
        self.assertIn('server 形式：主な比較', page)
        self.assertIn('単一プロセス形式：補足', page)
        self.assertIn('予定 100・送信 99・観測 98・失敗 2・未送信 1', page)


if __name__ == '__main__':
    unittest.main()
