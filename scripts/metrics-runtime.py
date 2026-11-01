#!/usr/bin/env python3
"""Runtime measurement helpers for the isolated mini benchmark."""

import json
import math
import os
import statistics
import subprocess
import time
from pathlib import Path


def new_ledger(metadata):
    """Declare the full comparison before running so interruptions remain visible."""
    metrics = []

    def add(mode, key, name, unit, products=('ikesu', 'herdr')):
        metrics.append({'mode': mode, 'key': key, 'name': name, 'unit': unit,
                        'samples': {product: [{'status': 'not_started', 'error': '未実行'}
                                              for _ in range(3)]
                                    for product in products}, 'median': {}})

    for state, label in (('idle', '出力前'), ('output', '各ペイン2万行出力後')):
        for count in (1, 10, 20):
            for role, role_label in (('server', 'server'), ('client', '全client'), ('total', '合計')):
                add('server', f'rss.{state}.{count}.{role}',
                    f'{label}・{count}ペイン・{role_label} RSS', 'KiB')
            add('no-session', f'rss.{state}.{count}.total',
                f'{label}・{count}ペイン・合計 RSS', 'KiB')
            for key, title in (('heap', 'server heap'), ('rss_minus_heap', 'server RSS − heap')):
                add('diagnostic', f'{key}.{state}.{count}', f'{label}・{count}ペイン・{title}',
                    'KiB', ('ikesu',))
        for low, high in ((1, 10), (10, 20)):
            for mode, roles in (('server', ('server', 'client', 'total')),
                                ('no-session', ('total',))):
                for role in roles:
                    add(mode, f'rss_slope.{state}.{low}.{high}.{role}',
                        f'{label}・{low}→{high}ペイン・{role} RSS の増分', 'KiB/ペイン')
    for key, title, unit in (
        ('startup', '起動からプロンプト表示', '秒'),
        ('idle_cpu', '20ペイン待機中の CPU（1コアを100%）', '%'),
        ('flood.write', '200万行の書き終わり', '秒'),
        ('flood.screen', '200万行の最終行を画面で確認', '秒'),
        ('flood.lag', '200万行の書き終わりから画面まで', '秒'),
        ('latency.idle.p50', '通常入力遅延 p50', '秒'),
        ('latency.idle.p99', '通常入力遅延 p99', '秒'),
        ('latency.flood.p50', '出力中の入力遅延 p50', '秒'),
        ('latency.flood.p99', '出力中の入力遅延 p99', '秒'),
    ):
        add('server', key, title, unit)
    return {'metadata': metadata, 'metrics': metrics, 'runs': []}


def record_sample(ledger, mode, key, product, index, sample):
    match = [m for m in ledger['metrics'] if m['mode'] == mode and m['key'] == key]
    if len(match) != 1:
        raise ValueError(f'unknown or duplicate metric: {mode} {key}')
    metric = match[0]
    metric['samples'][product][index] = sample
    metric['median'][product] = median_three(metric['samples'][product])


def latency_summary(samples):
    """Nearest-rank percentiles; an unsuccessful observation invalidates them."""
    observed = [s['seconds'] for s in samples if s['status'] == 'ok']
    failed = len(samples) - len(observed)
    result = {'sent': len(samples), 'observed': len(observed), 'failed': failed,
              'percentile_method': 'nearest-rank'}
    if not failed and observed:
        ordered = sorted(observed)
        result.update(p50_seconds=ordered[math.ceil(0.50 * len(ordered)) - 1],
                      p99_seconds=ordered[math.ceil(0.99 * len(ordered)) - 1])
    return result


def latency_from_events(events, count, flood_window=None):
    """Match unique inputs to their first subsequent completed screen frame."""
    inputs = {}
    replies = {}
    for event in events:
        if event.get('type') == 'capacity.input':
            token = event['data']
            if token in inputs:
                raise ValueError('duplicate input token: ' + token)
            inputs[token] = event['clock_ns']
        elif event.get('type') == 'capacity.reply':
            replies.setdefault(event['token'], []).append(event['clock_ns'])
    samples = []
    for i in range(count):
        token = f'CAPRX{i:03d}!'
        if token not in inputs:
            samples.append({'token': token, 'status': 'not_sent'})
            continue
        start = inputs[token]
        matches = [at for at in replies.get(token, ()) if at >= start]
        if not matches:
            samples.append({'token': token, 'status': 'timeout', 'sent_ns': start})
            continue
        end = min(matches)
        if flood_window is not None and not (flood_window[0] <= start <= end <= flood_window[1]):
            samples.append({'token': token, 'status': 'outside_flood', 'sent_ns': start,
                            'observed_ns': end})
            continue
        samples.append({'token': token, 'status': 'ok', 'sent_ns': start,
                        'observed_ns': end, 'seconds': (end - start) / 1e9})
    return samples


def median_three(samples):
    if len(samples) != 3 or any(s['status'] != 'ok' for s in samples):
        return None
    return statistics.median(s['value'] for s in samples)


def parse_cpu_time(text):
    """Parse ps CPU time (MM:SS or HH:MM:SS, optionally with days)."""
    days = 0
    if '-' in text:
        day_text, text = text.split('-', 1)
        days = int(day_text)
    parts = [float(part) for part in text.split(':')]
    if len(parts) not in (2, 3):
        raise ValueError('unexpected ps CPU time')
    seconds = 0.0
    for part in parts:
        seconds = seconds * 60 + part
    return days * 86400 + seconds


def parse_ps(output):
    rows = []
    for line in output.splitlines():
        fields = line.strip().split(None, 4)
        if len(fields) != 5:
            raise ValueError('incomplete ps row')
        pid, ppid, rss, cpu, command = fields
        rows.append({'pid': int(pid), 'ppid': int(ppid), 'rss_kib': int(rss),
                     'cpu_seconds': parse_cpu_time(cpu), 'command': command})
    return rows


def product_processes(rows, root_pid, executable, mode):
    """Select only the measured executable in the client's descendant tree."""
    descendants = {root_pid}
    while True:
        expanded = descendants | {p['pid'] for p in rows if p['ppid'] in descendants}
        if expanded == descendants:
            break
        descendants = expanded
    target = os.path.realpath(executable)
    selected = []
    for row in rows:
        if row['pid'] not in descendants:
            continue
        args = row['command'].split()
        if not args or os.path.realpath(args[0]) != target:
            continue
        role = 'monolith' if mode == 'no-session' else (
            'server' if 'server' in args[1:] else 'client')
        selected.append({**row, 'role': role})
    if not any(p['pid'] == root_pid for p in selected):
        raise RuntimeError('measured client process missing from ps snapshot')
    return selected


def process_snapshot(root_pid, executable, mode):
    before = time.monotonic_ns()
    output = subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss=,time=,command='], text=True)
    after = time.monotonic_ns()
    processes = product_processes(parse_ps(output), root_pid, executable, mode)
    totals = {role: sum(p['rss_kib'] for p in processes if p['role'] == role)
              for role in ('server', 'client', 'monolith')}
    if mode == 'server' and not any(p['role'] == 'server' for p in processes):
        totals['server'] = totals['client'] = None
    return {'ps_start_ns': before, 'ps_end_ns': after, 'processes': processes,
            'rss_kib': {**totals, 'total': sum(p['rss_kib'] for p in processes)}}


def cpu_between(start, end):
    """CPU seconds divided by elapsed seconds; 100% denotes one full core."""
    first = {p['pid']: p for p in start['processes']}
    last = {p['pid']: p for p in end['processes']}
    if first.keys() != last.keys():
        raise ValueError('product process set changed during CPU observation')
    interval = ((end['ps_start_ns'] + end['ps_end_ns']) -
                (start['ps_start_ns'] + start['ps_end_ns'])) / 2e9
    if interval <= 0:
        raise ValueError('nonpositive CPU observation interval')
    seconds = sum(last[pid]['cpu_seconds'] - first[pid]['cpu_seconds'] for pid in first)
    if seconds < 0 or any(last[pid]['cpu_seconds'] < first[pid]['cpu_seconds'] for pid in first):
        raise ValueError('CPU counter decreased')
    return {'wall_seconds': interval, 'cpu_seconds': seconds,
            'percent_one_core': seconds / interval * 100}


def pane_increment(first, last, first_count, last_count):
    if last_count <= first_count:
        raise ValueError('pane counts must increase')
    return (last - first) / (last_count - first_count)


def save_json(path, data):
    """Replace a single run atomically so interruption cannot leave partial JSON."""
    path = Path(path)
    path.parent.mkdir(parents=True, exist_ok=True)
    temporary = path.with_name(path.name + '.tmp-' + str(os.getpid()))
    try:
        with temporary.open('x', encoding='utf-8') as stream:
            json.dump(data, stream, ensure_ascii=False, indent=2, allow_nan=False)
            stream.write('\n')
            stream.flush()
            os.fsync(stream.fileno())
        os.replace(temporary, path)
    finally:
        temporary.unlink(missing_ok=True)
