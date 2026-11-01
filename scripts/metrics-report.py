#!/usr/bin/env python3
"""Render a self-contained report from a runtime measurement JSON file."""

import argparse
import html
import json
import statistics
from pathlib import Path


def summarize(samples):
    """Return a median only when all three comparison runs succeeded."""
    if len(samples) != 3 or any(sample['status'] != 'ok' for sample in samples):
        return None
    return statistics.median(sample['value'] for sample in samples)


def render(data):
    esc = lambda value: html.escape(str(value), quote=True)
    sections = []
    for mode, title in (('server', 'server 形式：主な比較'),
                        ('no-session', '単一プロセス形式：補足'),
                        ('diagnostic', '診断計測')):
        rows = []
        for metric in (m for m in data['metrics'] if m['mode'] == mode):
            rows.append(render_row(metric))
        sections.append('<section><h2>{}</h2><div class="table-wrap"><table><thead><tr><th>指標</th>'
                        '<th>ikesu</th><th>herdr</th></tr></thead><tbody>{}</tbody>'
                        '</table></div></section>'.format(esc(title), ''.join(rows)))
    metadata = esc(json.dumps(data['metadata'], ensure_ascii=False, indent=2))
    observations = render_observations(data.get('runs', []))
    return '''<!doctype html><html lang="ja"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>実行時計測</title>
<style>body{font:16px system-ui,sans-serif;margin:2rem;color:#17202a;background:#f7f8fa}
main{max-width:1200px;margin:auto}table{border-collapse:collapse;width:100%;background:white}
th,td{text-align:left;vertical-align:top;padding:.8rem;border-bottom:1px solid #d9dfe5}
th{width:28%}td{width:36%}small{color:#485563;overflow-wrap:anywhere}
.table-wrap{overflow-x:auto}
.bar{height:.65rem;background:#e8edf2;margin:.5rem 0}.bar i{display:block;height:100%;background:#326ca3}
td:last-child .bar i{background:#ba6c2c}pre{white-space:pre-wrap;overflow-wrap:anywhere}
@media(max-width:650px){body{margin:.6rem}th,td{padding:.4rem;font-size:.85rem}}
</style></head><body><main><h1>実行時の速度とメモリ</h1>
<p>棒の長さは各指標の最大絶対値に対する中央値の絶対値です。符号は数値で示します。各試行の値も表示し、失敗を含む系列の中央値は表示しません。画面の時刻は端末エミュレーターで観測しています。</p>''' + ''.join(sections) + observations + '''
<h2>計測条件</h2><pre>''' + metadata + '</pre></main></body></html>'


def render_observations(runs):
    esc = lambda value: html.escape(str(value), quote=True)
    rows = []
    for run in runs:
        metric = run.get('metric', '')
        if not (metric.startswith('latency.') or metric == 'heap'):
            continue
        parts = []
        if metric.startswith('latency.'):
            summary = run.get('summary', {})
            if summary:
                parts.append('予定 {}・送信 {}・観測 {}・失敗 {}・未送信 {}'.format(
                    *(summary.get(key, '?') for key in ('scheduled', 'sent', 'observed', 'failed', 'not_sent'))))
            trace = run.get('internal_trace')
            if trace:
                parts.append('内部probe: 送信 {}・対応 {}・未対応送信 {}・未対応応答 {}・上限到達 {}'.format(
                    *(trace.get(key, '?') for key in ('sent', 'matched', 'unmatched_sent',
                                                     'unmatched_replies', 'truncated'))))
            for name, segment in run.get('internal_surface', {}).items():
                if not isinstance(segment, dict):
                    continue
                parts.append('{}: 対応 {}・送信側未対応 {}・受信側未対応 {}・判別不能 {}'.format(
                    name, segment.get('matched', '?'), segment.get('unmatched_first', '?'),
                    segment.get('unmatched_last', '?'), segment.get('ambiguous_keys', '?')))
        else:
            for state, label in (('idle', '出力前'), ('output', '出力後')):
                if state in run:
                    parts.append('{} heap→RSS {:.3f}秒'.format(
                        label, run[state]['rss_after_heap_ns'] / 1e9))
        if run.get('error'):
            parts.append('失敗: ' + run['error'])
        if run.get('cleanup_error'):
            parts.append('後片付け失敗: ' + run['cleanup_error'])
        if run.get('evidence_error'):
            parts.append('証拠の保存失敗: ' + run['evidence_error'])
        if run.get('trace_error'):
            parts.append('トレース失敗: ' + run['trace_error'])
        count = '・{}ペイン'.format(run['panes']) if 'panes' in run else ''
        title = '{} {}{} {}回目{}'.format(run.get('product', ''), metric, count, run.get('round', '?'),
                                          '（診断）' if run.get('trace') else '')
        rows.append('<tr><th scope="row">{}</th><td colspan="2">{}</td></tr>'.format(
            esc(title), esc(' / '.join(parts) or '記録なし')))
    if not rows:
        return ''
    return ('<section><h2>観測数と診断の時刻</h2><div class="table-wrap"><table><tbody>'
            + ''.join(rows) + '</tbody></table></div></section>')


def render_row(metric):
    esc = lambda value: html.escape(str(value), quote=True)
    groups = metric['samples']
    successful = [abs(s['value']) for samples in groups.values() for s in samples
                  if s['status'] == 'ok']
    scale = max(successful, default=0)
    cells = []
    for product in ('ikesu', 'herdr'):
        if product not in groups:
            cells.append('<td>対象外</td>')
            continue
        samples = groups.get(product, [])
        median = summarize(samples)
        raw = ', '.join(str(s['value']) if s['status'] == 'ok'
                        else ('未実行' if s['status'] == 'not_started'
                              else '失敗: ' + s.get('error', '理由未記録'))
                        for s in samples) or '未計測'
        width = 0 if median is None or scale == 0 else 100 * abs(median) / scale
        cells.append('<td><strong>{}</strong><div class="bar"><i style="width:{:.4f}%"></i></div>'
                     '<small>{}</small></td>'.format(
                         esc(median if median is not None else '集計不可'), width, esc(raw)))
    return '<tr><th scope="row">{}<br><small>{}</small></th>{}</tr>'.format(
        esc(metric['name']), esc(metric['unit']), ''.join(cells))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('json', type=Path)
    parser.add_argument('html', type=Path)
    args = parser.parse_args()
    data = json.loads(args.json.read_text(encoding='utf-8'))
    args.html.write_text(render(data), encoding='utf-8')


if __name__ == '__main__':
    main()
