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
        sections.append('<section><h2>{}</h2><table><thead><tr><th>指標</th>'
                        '<th>ikesu</th><th>herdr</th></tr></thead><tbody>{}</tbody>'
                        '</table></section>'.format(esc(title), ''.join(rows)))
    metadata = esc(json.dumps(data['metadata'], ensure_ascii=False, indent=2))
    return '''<!doctype html><html lang="ja"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>実行時計測</title>
<style>body{font:16px system-ui,sans-serif;margin:2rem;color:#17202a;background:#f7f8fa}
main{max-width:1200px;margin:auto}table{border-collapse:collapse;width:100%;background:white}
th,td{text-align:left;vertical-align:top;padding:.8rem;border-bottom:1px solid #d9dfe5}
th{width:28%}td{width:36%}small{color:#485563;overflow-wrap:anywhere}
.bar{height:.65rem;background:#e8edf2;margin:.5rem 0}.bar i{display:block;height:100%;background:#326ca3}
td:last-child .bar i{background:#ba6c2c}pre{white-space:pre-wrap;overflow-wrap:anywhere}
@media(max-width:650px){body{margin:.6rem}th,td{padding:.4rem;font-size:.85rem}}
</style></head><body><main><h1>実行時の速度とメモリ</h1>
<p>棒の長さは各指標の最大絶対値に対する中央値の絶対値です。符号は数値で示します。各試行の値も表示し、失敗を含む系列の中央値は表示しません。</p>''' + ''.join(sections) + '''
<h2>計測条件</h2><pre>''' + metadata + '</pre></main></body></html>'


def render_row(metric):
    esc = lambda value: html.escape(str(value), quote=True)
    groups = metric['samples']
    successful = [abs(s['value']) for samples in groups.values() for s in samples
                  if s['status'] == 'ok']
    scale = max(successful, default=0)
    cells = []
    for product in ('ikesu', 'herdr'):
        samples = groups.get(product, [])
        median = summarize(samples)
        raw = ', '.join(str(s['value']) if s['status'] == 'ok'
                        else '失敗: ' + s.get('error', '理由未記録')
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
