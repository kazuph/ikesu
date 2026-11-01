#!/usr/bin/env python3
"""Runtime measurement helpers for the isolated mini benchmark."""

import argparse
import hashlib
import importlib.util
import json
import math
import os
import re
import shlex
import shutil
import signal
import statistics
import subprocess
import sys
import tempfile
import time
from datetime import datetime, timezone
from pathlib import Path


# Both products document 10,000,000 bytes as their default scrollback limit.
SCROLLBACK_BYTES = 10_000_000


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
    add('no-session', 'rss.styled.increment', '色付き出力64MiB・1ペイン RSS の増分', 'KiB')
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
    for segment, label in (('queued_to_received', 'server送信からclient受信'),
                           ('received_to_written', 'client受信から端末書込')):
        for percentile in ('p50', 'p99'):
            add('diagnostic', f'trace.{segment}.{percentile}',
                f'内部区間 {label} {percentile}（対応した記録）', '秒', ('ikesu',))
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
    not_sent = sum(sample['status'] == 'not_sent' for sample in samples)
    result = {'scheduled': len(samples), 'sent': len(samples) - not_sent,
              'not_sent': not_sent, 'observed': len(observed), 'failed': failed,
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
        elif event.get('type') == 'capacity.screen':
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


def summarize_probe_trace(text):
    """Match probe sequence IDs and disclose dropped or unmatched records."""
    sent, replied = {}, {}
    unmatched_replies = give_up = 0
    truncated = 'ikesu_latency 上限に達した' in text
    for line in text.splitlines():
        if not line.startswith('ikesu_latency '):
            continue
        fields = dict(part.split('=', 1) for part in line.split()[1:] if '=' in part)
        event = fields.get('event')
        if event not in ('probe.sent', 'probe.reply', 'probe.give_up'):
            continue
        if event == 'probe.give_up':
            give_up += 1
            continue
        if event == 'probe.reply' and fields.get('matched') != 'true':
            unmatched_replies += 1
            continue
        key = (fields.get('endpoint'), fields.get('generation'), fields.get('draw_seq'))
        target = sent if event == 'probe.sent' else replied
        target.setdefault(key, []).append(int(fields['unix_us']))
    durations = []
    ambiguous = 0
    for key in sent.keys() & replied.keys():
        if len(sent[key]) != 1 or len(replied[key]) != 1 or replied[key][0] < sent[key][0]:
            ambiguous += 1
        else:
            durations.append((replied[key][0] - sent[key][0]) / 1e6)
    durations.sort()
    summary = {'sent': sum(map(len, sent.values())), 'matched': len(durations),
               'unmatched_sent': len(sent.keys() - replied.keys()),
               'unmatched_replies': unmatched_replies + len(replied.keys() - sent.keys()),
               'ambiguous_keys': ambiguous, 'give_up': give_up, 'truncated': truncated,
               'percentile_method': 'nearest-rank; matched records only'}
    if durations:
        summary['p50_seconds'] = durations[math.ceil(.5 * len(durations)) - 1]
        summary['p99_seconds'] = durations[math.ceil(.99 * len(durations)) - 1]
    return summary


def summarize_surface_trace(server_text, client_text):
    """Match one-client frame revisions without treating unmatched records as zero."""
    events = {'surface.queued': {}, 'surface.received': {}, 'terminal.written': {}}
    for text in (server_text, client_text):
        for line in text.splitlines():
            if not line.startswith('ikesu_latency '):
                continue
            fields = dict(part.split('=', 1) for part in line.split()[1:] if '=' in part)
            event = fields.get('event')
            if event not in events:
                continue
            key = (fields.get('boot_id'), fields.get('revision'))
            if None in key:
                continue
            events[event].setdefault(key, []).append(int(fields['unix_us']))
    result = {}
    for name, first_name, last_name in (
        ('queued_to_received', 'surface.queued', 'surface.received'),
        ('received_to_written', 'surface.received', 'terminal.written'),
    ):
        first, last = events[first_name], events[last_name]
        values, ambiguous = [], 0
        for key in first.keys() & last.keys():
            if len(first[key]) != 1 or len(last[key]) != 1 or last[key][0] < first[key][0]:
                ambiguous += 1
            else:
                values.append((last[key][0] - first[key][0]) / 1e6)
        values.sort()
        summary = {'first_records': sum(map(len, first.values())),
                   'last_records': sum(map(len, last.values())), 'matched': len(values),
                   'unmatched_first': len(first.keys() - last.keys()),
                   'unmatched_last': len(last.keys() - first.keys()), 'ambiguous_keys': ambiguous,
                   'percentile_method': 'nearest-rank; matched records only'}
        if values:
            summary['p50_seconds'] = values[math.ceil(.5 * len(values)) - 1]
            summary['p99_seconds'] = values[math.ceil(.99 * len(values)) - 1]
        result[name] = summary
    result['truncated'] = any('ikesu_latency 上限に達した' in text for text in (server_text, client_text))
    return result


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
    before = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
    output = subprocess.check_output(['ps', '-axo', 'pid=,ppid=,rss=,time=,command='], text=True)
    after = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
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


def wait_until(predicate, timeout, description, process=None):
    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if predicate():
            return
        if process is not None and process.poll() is not None:
            raise RuntimeError(f'{description}: ptyrun exited {process.returncode}')
        time.sleep(.05)
    raise TimeoutError(description)


class MeasurementSession:
    """One isolated application instance, controlled through its real CLI."""

    def __init__(self, binary, ptyrun, product, mode, work):
        self.binary = Path(binary).resolve()
        self.ptyrun = Path(ptyrun).resolve()
        self.product = product
        self.mode = mode
        self.root = Path(tempfile.mkdtemp(prefix='dx-', dir=work))
        self.session = 'dx-' + self.root.name
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith(('IKESU_', 'HERDR_'))}
        zdot = self.root / 'z'
        zdot.mkdir()
        (zdot / '.zshrc').write_text("PS1='DXPROBEREADY> '\n")
        config = self.root / 'config' / product
        config.mkdir(parents=True)
        config_file = config / 'config.toml'
        config_file.write_text(f'onboarding = false\n[advanced]\nscrollback_limit_bytes = {SCROLLBACK_BYTES}\n')
        self.env.update(HOME=str(self.root), XDG_CONFIG_HOME=str(self.root / 'config'),
                        XDG_STATE_HOME=str(self.root / 'state'), ZDOTDIR=str(zdot),
                        SHELL='/bin/zsh', TERM='xterm-256color')
        if product == 'ikesu':
            self.env.update(IKESU_STATE_DIR=str(self.root / 'state'), IKESU_CONFIG=str(config_file))
        self.ready = self.root / 'screen.txt'
        self.release = self.root / 'release'
        self.process_file = self.root / 'process.json'
        self.steps = self.root / 'steps.txt'
        self.steps.write_text(f'waittext 60s DXPROBEREADY>\nscreen {self.ready}\n'
                              f'waitfile {self.release} 3600s\nkill\n')
        self.log = None
        self.proc = None
        self.pid = None
        self.timeline = None
        self.screen_tokens = False
        self.screen_text = None
        self.launch_ns = None

    def start(self):
        if self.mode != 'server':
            raise ValueError('MeasurementSession requires server mode')
        self.log = (self.root / 'ptyrun.log').open('w')
        args = [str(self.ptyrun), '-cols', '160', '-rows', '45', '-steps', str(self.steps),
                '-process-file', str(self.process_file)]
        if self.timeline:
            args.extend(['-timeline', str(self.timeline)])
        if self.screen_tokens:
            args.append('-screen-tokens')
        if self.screen_text:
            args.extend(['-screen-text', self.screen_text])
        args.extend(['--', str(self.binary), '--session', self.session])
        self.launch_ns = time.clock_gettime_ns(time.CLOCK_MONOTONIC)
        self.proc = subprocess.Popen(args, cwd=self.root, env=self.env, stdout=self.log,
                                     stderr=subprocess.STDOUT)
        wait_until(lambda: self.ready.exists() and 'DXPROBEREADY>' in self.ready.read_text(),
                   70, 'initial prompt screen', self.proc)
        self.pid = json.loads(self.process_file.read_text())['pid']
        return self

    def cli(self, *args):
        command = [str(self.binary), '--session', self.session, *args]
        result = subprocess.run(command, cwd=self.root, env=self.env, text=True,
                                capture_output=True, timeout=30)
        if result.returncode:
            raise RuntimeError(f'{args[:2]} failed ({result.returncode}): {result.stderr.strip()}')
        return result.stdout

    def panes(self):
        data = json.loads(self.cli('pane', 'list'))
        return data['result']['panes']

    def wait_pane_text(self, pane, expected, timeout=60):
        def appeared():
            return expected in self.cli('pane', 'read', pane, '--source', 'visible', '--lines', '10')
        wait_until(appeared, timeout, f'{pane}: {expected}', self.proc)

    def create_panes(self, count):
        panes = self.panes()
        if len(panes) != 1:
            raise RuntimeError(f'expected one initial pane, found {len(panes)}')
        ids = [panes[0]['pane_id']]
        workspace = panes[0]['workspace_id']
        self.wait_pane_text(ids[0], 'DXPROBEREADY>')
        for _ in range(count - 1):
            self.cli('tab', 'create', '--workspace', workspace, '--no-focus')
            added = {p['pane_id'] for p in self.panes()} - set(ids)
            if len(added) != 1:
                raise RuntimeError(f'tab creation added {len(added)} panes')
            pane = added.pop()
            ids.append(pane)
            self.wait_pane_text(pane, 'DXPROBEREADY>')
        if len(self.panes()) != count:
            raise RuntimeError('pane count differs after creation')
        return ids

    def emit_lines(self, panes):
        script = self.root / 'emit.py'
        script.write_text("import sys\nfrom pathlib import Path\n"
                          "for n in range(20000): print(f'{n} benchmark output')\n"
                          "print('DX'+'COMPLETE'+sys.argv[1], flush=True)\n"
                          "Path(sys.argv[2]).write_text('done')\n")
        for index, pane in enumerate(panes):
            done = self.root / f'output-{index}.done'
            command = ' '.join(map(shlex.quote, ['python3', str(script), str(index), str(done)]))
            self.cli('pane', 'run', pane, command)
        for index, pane in enumerate(panes):
            done = self.root / f'output-{index}.done'
            wait_until(done.exists, 300, f'{pane} output producer', self.proc)
            self.wait_pane_text(pane, f'DXCOMPLETE{index}', timeout=300)

    def close(self):
        if self.proc is None:
            return
        errors = []
        if self.mode == 'server':
            try:
                self.cli('server', 'stop')
            except Exception as exc:
                errors.append(str(exc))
        self.release.write_text('done')
        try:
            self.proc.wait(timeout=30)
        except subprocess.TimeoutExpired:
            try:
                terminate_ptyrun(self)
            except Exception as exc:
                errors.append(str(exc))
        if self.log:
            self.log.close()
        if errors:
            raise RuntimeError('; '.join(errors))


class MonolithMemorySession:
    """Use the same keyboard script for both monolithic applications."""

    def __init__(self, binary, ptyrun, product, count, work):
        self.binary = Path(binary).resolve()
        self.ptyrun = Path(ptyrun).resolve()
        self.root = Path(tempfile.mkdtemp(prefix='dx-', dir=work))
        self.count = count
        self.env = {k: v for k, v in os.environ.items()
                    if not k.startswith(('IKESU_', 'HERDR_'))}
        zdot = self.root / 'z'
        zdot.mkdir()
        (zdot / '.zshrc').write_text('id="${IKESU_PANE_ID:-$HERDR_PANE_ID}"\n'
                                    'PS1="DX${id//_/}> "\n')
        config = self.root / 'config' / product
        config.mkdir(parents=True)
        config_file = config / 'config.toml'
        config_file.write_text('onboarding = false\n[ui]\nprompt_new_tab_name = false\n'
                               f'[advanced]\nscrollback_limit_bytes = {SCROLLBACK_BYTES}\n')
        self.env.update(HOME=str(self.root), XDG_CONFIG_HOME=str(self.root / 'config'),
                        XDG_STATE_HOME=str(self.root / 'state'), ZDOTDIR=str(zdot),
                        SHELL='/bin/zsh', TERM='xterm-256color')
        if product == 'ikesu':
            self.env.update(IKESU_STATE_DIR=str(self.root / 'state'), IKESU_CONFIG=str(config_file))
        self.idle = self.root / 'idle'
        self.release_idle = self.root / 'release-idle'
        self.output = self.root / 'output'
        self.release_output = self.root / 'release-output'
        self.process_file = self.root / 'process.json'
        emitter = self.root / 'emit.py'
        emitter.write_text("import sys\nfrom pathlib import Path\n"
                           "for n in range(20000): print(f'{n} benchmark output')\n"
                           "print('DX'+'COMPLETE'+sys.argv[1], flush=True)\n"
                           "Path(sys.argv[2]).write_text('done')\n")
        steps = ['waittext 60s DXp1>']
        for pane in range(2, count + 1):
            steps.extend(['send "\\u0002c"', f'waittext 60s DXp{pane}>'])
        steps.extend([f'screen {self.root}/idle-screen', f'notifyfile {self.idle}',
                      f'waitfile {self.release_idle} 3600s'])
        for pane in range(count, 0, -1):
            done = self.root / f'output-{pane}.done'
            command = ' '.join(map(shlex.quote, ['python3', str(emitter), str(pane), str(done)])) + '\r'
            steps.extend([f'send {json.dumps(command)}', f'waitfile {done} 300s',
                          f'waittext 300s DXCOMPLETE{pane}'])
            if pane > 1:
                steps.extend(['send "\\u0002p"', f'waittext 60s DXp{pane - 1}>'])
        steps.extend([f'screen {self.root}/output-screen', f'notifyfile {self.output}',
                      f'waitfile {self.release_output} 3600s', 'kill'])
        self.steps = self.root / 'steps.txt'
        self.steps.write_text('\n'.join(steps) + '\n')
        self.proc = None
        self.log = None
        self.pid = None

    def start(self):
        self.log = (self.root / 'ptyrun.log').open('w')
        self.proc = subprocess.Popen([str(self.ptyrun), '-cols', '160', '-rows', '45',
                                      '-steps', str(self.steps), '-process-file', str(self.process_file),
                                      '--', str(self.binary), '--no-session'], cwd=self.root,
                                     env=self.env, stdout=self.log, stderr=subprocess.STDOUT)
        wait_until(self.idle.exists, 90 + 60 * self.count, 'monolith pane setup', self.proc)
        screen = (self.root / 'idle-screen').read_text()
        if (f'DXp{self.count}>' not in screen or
                'TIMEOUT' in (self.root / 'ptyrun.log').read_text()):
            raise RuntimeError('last pane prompt missing on screen')
        self.pid = json.loads(self.process_file.read_text())['pid']
        return self

    def emit_lines(self):
        self.release_idle.write_text('done')
        wait_until(self.output.exists, 300 * self.count, 'monolith pane output', self.proc)
        if 'DXCOMPLETE1' not in (self.root / 'output-screen').read_text():
            raise RuntimeError('last output marker missing on screen')
        log = (self.root / 'ptyrun.log').read_text()
        if 'TIMEOUT' in log:
            raise RuntimeError('ptyrun reported a timeout')

    def close(self):
        if self.proc is None:
            return
        self.release_idle.write_text('done')
        self.release_output.write_text('done')
        try:
            self.proc.wait(timeout=30)
        except subprocess.TimeoutExpired:
            terminate_ptyrun(self)
        finally:
            if self.log:
                self.log.close()


class StyledMemorySession(MonolithMemorySession):
    """The original six-stage coloured Japanese output on one pane."""

    def __init__(self, binary, ptyrun, product, work):
        super().__init__(binary, ptyrun, product, 1, work)
        emitter = self.root / 'styled-emit.py'
        emitter.write_text('''import sys
mb=int(sys.argv[1]); tag=sys.argv[2]
words=["ペイン", "\\x1b[1mbold\\x1b[22m", "メモリ", "\\x1b[4munder\\x1b[24m", "計測", "value"]
def line(i):
    parts=[f"\\x1b[38;5;{(i*7+j)%256}m{words[(i+j)%len(words)]}\\x1b[39m" for j in range(14)]
    return f"\\x1b[48;5;{i%256}m"+" ".join(parts)+"\\x1b[0m\\n"
out=0; i=0; w=sys.stdout.write
while out < mb*1024*1024:
    s=line(i); w(s); out+=len(s.encode()); i+=1
sys.stdout.flush(); print("DONE_"+tag)
''')
        steps = ['waittext 60s DXp1>', 'sleep 3s', f'screen {self.root}/idle-screen',
                 f'notifyfile {self.idle}', f'waitfile {self.release_idle} 3600s']
        for index, size in enumerate((2, 2, 4, 8, 16, 32)):
            tag = f'm{size}_{index}'
            command = ' '.join(map(shlex.quote, ['python3', str(emitter), str(size), tag])) + '\r'
            steps.extend([f'send {json.dumps(command)}', f'waittext 180s DONE_{tag}', 'sleep 2s'])
        steps.extend([f'screen {self.root}/output-screen', f'notifyfile {self.output}',
                      f'waitfile {self.release_output} 3600s', 'kill'])
        self.steps.write_text('\n'.join(steps) + '\n')

    def emit_lines(self):
        self.release_idle.write_text('done')
        wait_until(self.output.exists, 6 * 180 + 30, 'styled output', self.proc)
        if 'DONE_m32_5' not in (self.root / 'output-screen').read_text():
            raise RuntimeError('styled output marker missing on screen')
        if 'TIMEOUT' in (self.root / 'ptyrun.log').read_text():
            raise RuntimeError('ptyrun reported a timeout')


def measure_styled_memory(binary, ptyrun, product, work):
    session = StyledMemorySession(binary, ptyrun, product, work)
    result = {'product': product, 'metric': 'styled-memory', 'panes': 1,
              'root': str(session.root), 'status': 'running'}
    try:
        session.start()
        result['idle'] = process_snapshot(session.pid, binary, 'no-session')
        session.emit_lines()
        result['output'] = process_snapshot(session.pid, binary, 'no-session')
        result['status'] = 'ok'
    except Exception as exc:
        result.update(status='failed', error=f'{type(exc).__name__}: {exc}')
    finally:
        try:
            session.close()
        except Exception as exc:
            result.update(status='failed', cleanup_error=f'{type(exc).__name__}: {exc}')
    return result


def terminate_ptyrun(session):
    """Stop only the runner proven to contain this session's unique root."""
    matches = subprocess.run(['pgrep', '-u', str(os.getuid()), '-f', str(session.root)],
                             capture_output=True, text=True, check=False)
    owned = {int(pid) for pid in matches.stdout.split()}
    if session.proc.pid not in owned:
        raise RuntimeError(f'cannot identify ptyrun pid={session.proc.pid} root={session.root}')
    child_pid = session.pid
    if child_pid is None and session.process_file.exists():
        child_pid = json.loads(session.process_file.read_text())['pid']
    os.kill(session.proc.pid, signal.SIGTERM)
    session.proc.wait(timeout=10)
    if child_pid and subprocess.run(['ps', '-p', str(child_pid)], capture_output=True).returncode == 0:
        raise RuntimeError(f'measured process still running pid={child_pid} root={session.root}')


def measure_memory(binary, ptyrun, product, mode, count, work, measure_idle_cpu=False):
    if mode == 'server':
        session = MeasurementSession(binary, ptyrun, product, mode, work)
    elif mode == 'no-session':
        session = MonolithMemorySession(binary, ptyrun, product, count, work)
    else:
        raise ValueError('unknown launch mode')
    result = {'product': product, 'mode': mode, 'panes': count,
              'root': str(session.root), 'status': 'running'}
    try:
        session.start()
        panes = session.create_panes(count) if mode == 'server' else None
        result['idle'] = process_snapshot(session.pid, binary, mode)
        if measure_idle_cpu:
            if mode != 'server' or count != 20:
                raise ValueError('idle CPU requires 20 server-mode panes')
            time.sleep(60)
            result['cpu_end'] = process_snapshot(session.pid, binary, mode)
            result['cpu'] = cpu_between(result['idle'], result['cpu_end'])
        if mode == 'server':
            session.emit_lines(panes)
        else:
            session.emit_lines()
        result['output'] = process_snapshot(session.pid, binary, mode)
        result['status'] = 'ok'
    except Exception as exc:
        result.update(status='failed', error=f'{type(exc).__name__}: {exc}')
    finally:
        try:
            session.close()
        except Exception as exc:
            result.update(status='failed', cleanup_error=f'{type(exc).__name__}: {exc}')
    return result


def read_events(path):
    with Path(path).open(encoding='utf-8') as stream:
        return [json.loads(line) for line in stream if line.strip()]


def measure_startup(binary, ptyrun, product, work):
    session = MeasurementSession(binary, ptyrun, product, 'server', work)
    result = {'product': product, 'metric': 'startup', 'root': str(session.root), 'status': 'running'}
    session.timeline = session.root / 'timeline.jsonl'
    session.screen_text = 'DXPROBEREADY>'
    try:
        session.start()
        events = read_events(session.timeline)
        observed = next(e['clock_ns'] for e in events if e.get('type') == 'screen.text'
                        and e.get('text') == session.screen_text)
        if observed < session.launch_ns:
            raise ValueError('prompt timestamp precedes launch')
        result.update(status='ok', launch_ns=session.launch_ns, observed_ns=observed,
                      seconds=(observed - session.launch_ns) / 1e9,
                      resolution='completed terminal frame, PTY read CLOCK_MONOTONIC')
    except Exception as exc:
        result.update(status='failed', error=f'{type(exc).__name__}: {exc}')
    finally:
        try:
            session.close()
        except Exception as exc:
            result.update(status='failed', cleanup_error=f'{type(exc).__name__}: {exc}')
    return result


def measure_flood(binary, ptyrun, product, work):
    """Reuse the 2M-line workload and screen end point of tests/perf."""
    session = MeasurementSession(binary, ptyrun, product, 'server', work)
    result = {'product': product, 'metric': 'flood', 'root': str(session.root), 'status': 'running'}
    session.timeline = session.root / 'timeline.jsonl'
    session.screen_text = '1999999 line of throughput test'
    producer = session.root / 'flood.py'
    finished = session.root / 'producer.json'
    seen = session.root / 'screen-seen'
    end_gate = session.root / 'flood-end'
    producer.write_text("import json,os,sys,time\n"
                        "start=time.clock_gettime_ns(time.CLOCK_MONOTONIC)\n"
                        "w=sys.stdout.write\n"
                        "for i in range(2000000): w('%d line of throughput test\\n' % i)\n"
                        "sys.stdout.flush()\n"
                        "end=time.clock_gettime_ns(time.CLOCK_MONOTONIC)\n"
                        "p=sys.argv[1]; tmp=p+'.tmp'\n"
                        "with open(tmp,'w') as f: json.dump({'start_ns':start,'end_ns':end},f)\n"
                        "os.replace(tmp,p)\n")
    command = ' '.join(map(shlex.quote, ['python3', str(producer), str(finished)])) + '\r'
    session.steps.write_text(f'waittext 60s DXPROBEREADY>\nscreen {session.ready}\n'
                             f'waitfile {session.release} 3600s\nsendtimed {json.dumps(command)}\n'
                             'waittoken 300s 1999999 line of throughput test\n'
                             f'notifyfile {seen}\nwaitfile {finished} 300s\n'
                             f'waitfile {end_gate} 3600s\nkill\n')
    try:
        session.start()
        session.release.write_text('start')
        wait_until(seen.exists, 310, 'final line on screen', session.proc)
        wait_until(finished.exists, 310, 'flood producer completion', session.proc)
        log = (session.root / 'ptyrun.log').read_text()
        if 'TIMEOUT' in log:
            raise TimeoutError('ptyrun timed out during flood')
        events = read_events(session.timeline)
        sends = [e for e in events if e.get('type') == 'capacity.input' and e.get('data') == command]
        if len(sends) != 1:
            raise ValueError('flood command start missing or duplicated')
        start = sends[0]['clock_ns']
        producer_times = json.loads(finished.read_text())
        seen_events = [e for e in events if e.get('type') == 'screen.text'
                       and e.get('text') == session.screen_text]
        if len(seen_events) != 1 or seen_events[0]['clock_ns'] < start:
            raise ValueError('final screen frame missing or invalid')
        screen_ns = seen_events[0]['clock_ns']
        screen_seconds = (screen_ns - start) / 1e9
        result.update(status='ok', command_sent_ns=start, producer=producer_times,
                      write_seconds=(producer_times['end_ns'] - producer_times['start_ns']) / 1e9,
                      screen_seconds=screen_seconds, screen_ns=screen_ns,
                      lag_seconds=(screen_ns - producer_times['end_ns']) / 1e9,
                      screen_resolution='completed terminal frame, PTY read CLOCK_MONOTONIC')
    except Exception as exc:
        result.update(status='failed', error=f'{type(exc).__name__}: {exc}')
    finally:
        end_gate.write_text('done')
        try:
            session.close()
        except Exception as exc:
            result.update(status='failed', cleanup_error=f'{type(exc).__name__}: {exc}')
    return result


def measure_input_latency(binary, ptyrun, product, work, during_flood=False, trace=False):
    session = MeasurementSession(binary, ptyrun, product, 'server', work)
    if trace:
        session.env['IKESU_LATENCY_TRACE'] = '1'
    session.timeline = session.root / 'timeline.jsonl'
    session.screen_tokens = True
    kind = 'flood' if during_flood else 'idle'
    result = {'product': product, 'metric': 'latency.' + kind, 'trace': trace,
              'root': str(session.root), 'status': 'running'}
    completed = session.root / 'latency-complete'
    end_gate = session.root / 'latency-end'
    flood_stop = session.root / 'flood-stop'
    flood_started = session.root / 'flood-start.json'
    flood_ended = session.root / 'flood-end.json'
    steps = ['waittext 60s DXPROBEREADY>', f'screen {session.ready}',
             f'waitfile {session.release} 3600s']
    if during_flood:
        producer = session.root / 'ongoing-flood.py'
        producer.write_text('''import json, os, re, select, sys, termios, time, tty
from pathlib import Path
start, stop, end = map(Path, sys.argv[1:])
old = termios.tcgetattr(0)
pattern = re.compile(rb'CAPRX[0-9]{3}!')
buffer = b''
block = 'benchmark output during input\\r\\n' * 100
def stamp(path):
    tmp = path.with_suffix('.tmp')
    tmp.write_text(json.dumps({'ns': time.clock_gettime_ns(time.CLOCK_MONOTONIC)}))
    os.replace(tmp, path)
try:
    tty.setcbreak(0)
    rows = os.get_terminal_size(1).lines
    if rows < 3:
        raise RuntimeError('terminal is too short for the flood and input response')
    sys.stdout.write(f'\\x1b[2;{rows}r\\x1b[2;1H' + block)
    sys.stdout.flush()
    stamp(start)
    while not stop.exists():
        sys.stdout.write(block)
        sys.stdout.flush()
        if select.select([0], [], [], 0)[0]:
            buffer += os.read(0, 4096)
            while (match := pattern.search(buffer)):
                token = match.group().decode('ascii')
                buffer = buffer[match.end():]
                sys.stdout.write('\\x1b7\\x1b[1;1H\\x1b[2K' + token + '\\x1b8')
                sys.stdout.flush()
            buffer = buffer[-64:]
    stamp(end)
finally:
    sys.stdout.write('\\x1b[r')
    sys.stdout.flush()
    termios.tcsetattr(0, termios.TCSADRAIN, old)
''')
        command = ' '.join(map(shlex.quote, ['python3', str(producer), str(flood_started),
                                               str(flood_stop), str(flood_ended)])) + '\r'
        steps.extend([f'send {json.dumps(command)}', f'waitfile {flood_started} 60s'])
    for i in range(100):
        token = f'CAPRX{i:03d}!'
        steps.extend([f'sendtimed {json.dumps(token)}', f'waittoken 60s {token}', 'send "\\u0015"'])
    steps.append(f'notifyfile {completed}')
    if during_flood:
        steps.append(f'waitfile {flood_ended} 300s')
    steps.extend([f'waitfile {end_gate} 3600s', 'kill'])
    session.steps.write_text('\n'.join(steps) + '\n')
    try:
        session.start()
        session.release.write_text('start')

        def complete_or_fail():
            log = (session.root / 'ptyrun.log').read_text()
            if 'TIMEOUT' in log:
                raise TimeoutError('ptyrun timed out while observing input')
            return completed.exists()

        wait_until(complete_or_fail, 6100, '100 input observations', session.proc)
        window = None
        if during_flood:
            flood_stop.write_text('stop')
            wait_until(flood_ended.exists, 300, 'flood termination', session.proc)
            window = (json.loads(flood_started.read_text())['ns'],
                      json.loads(flood_ended.read_text())['ns'])
        samples = latency_from_events(read_events(session.timeline), 100, window)
        summary = latency_summary(samples)
        result.update(samples=samples, summary=summary, flood_window_ns=window,
                      resolution='completed terminal frame, PTY read CLOCK_MONOTONIC')
        if summary['observed'] != 100 or summary['failed']:
            raise ValueError('not all 100 input events were observed')
        result['status'] = 'ok'
    except Exception as exc:
        result.update(status='failed', error=f'{type(exc).__name__}: {exc}')
        if session.timeline.exists() and 'samples' not in result:
            try:
                result['samples'] = latency_from_events(read_events(session.timeline), 100)
                if during_flood:
                    for sample in result['samples']:
                        if sample['status'] == 'ok':
                            sample['status'] = 'flood_window_unverified'
                result['summary'] = latency_summary(result['samples'])
            except (ValueError, OSError, json.JSONDecodeError):
                pass
    finally:
        flood_stop.write_text('stop')
        end_gate.write_text('done')
        try:
            session.close()
        except Exception as exc:
            result.update(status='failed', cleanup_error=f'{type(exc).__name__}: {exc}')
    if trace:
        trace_dir = session.root / 'state' / session.session
        client_log, server_log = trace_dir / 'client.log', trace_dir / 'server.log'
        if client_log.is_file() and server_log.is_file():
            client_text = client_log.read_text(errors='replace')
            server_text = server_log.read_text(errors='replace')
            summary = summarize_probe_trace(client_text)
            summary['server_truncated'] = 'ikesu_latency 上限に達した' in server_text
            summary['client_log'] = str(client_log)
            summary['server_log'] = str(server_log)
            result['internal_trace'] = summary
            result['internal_surface'] = summarize_surface_trace(server_text, client_text)
        else:
            result['trace_error'] = 'client or server trace log missing'
    return result


def capture_heap(session, sequence):
    snapshot = process_snapshot(session.pid, session.binary, 'server')
    servers = [p for p in snapshot['processes'] if p['role'] == 'server']
    if len(servers) != 1:
        raise RuntimeError(f'expected one diagnostic server, found {len(servers)}')
    pid = servers[0]['pid']
    matches = subprocess.run(['pgrep', '-u', str(os.getuid()), '-f', str(session.binary)],
                             capture_output=True, text=True, check=False)
    if pid not in {int(value) for value in matches.stdout.split()}:
        raise RuntimeError('diagnostic server PID does not match the task binary')
    prefix = session.root / f'heap-{pid}-{sequence}'
    record, error = Path(str(prefix) + '.json'), Path(str(prefix) + '.error')
    os.kill(pid, signal.SIGUSR2)

    def ready():
        if error.exists():
            raise RuntimeError(error.read_text())
        return record.exists()

    wait_until(ready, 60, 'post-GC heap snapshot', session.proc)
    heap = json.loads(record.read_text())
    if heap['pid'] != pid or not Path(heap['profile']).is_file():
        raise RuntimeError('heap snapshot PID or profile is invalid')
    rss = process_snapshot(session.pid, session.binary, 'server')
    server = next((p for p in rss['processes'] if p['pid'] == pid), None)
    if server is None:
        raise RuntimeError('diagnostic server disappeared before RSS observation')
    return {'heap': heap, 'rss': rss, 'rss_after_heap_ns': rss['ps_start_ns'] - heap['monotonic_ns'],
            'heap_kib': heap['heap_alloc_bytes'] / 1024,
            'rss_minus_heap_kib': server['rss_kib'] - heap['heap_alloc_bytes'] / 1024}


def measure_heap(binary, ptyrun, count, work):
    session = MeasurementSession(binary, ptyrun, 'ikesu', 'server', work)
    session.env['IKESU_HEAP_TRIGGER_PREFIX'] = str(session.root / 'heap-{pid}')
    result = {'product': 'ikesu', 'metric': 'heap', 'panes': count,
              'root': str(session.root), 'status': 'running'}
    try:
        session.start()
        panes = session.create_panes(count)
        result['idle'] = capture_heap(session, 1)
        session.emit_lines(panes)
        result['output'] = capture_heap(session, 2)
        result['status'] = 'ok'
    except Exception as exc:
        result.update(status='failed', error=f'{type(exc).__name__}: {exc}')
    finally:
        try:
            session.close()
        except Exception as exc:
            result.update(status='failed', cleanup_error=f'{type(exc).__name__}: {exc}')
    return result


def store_memory_result(ledger, index, result):
    """Keep snapshots and publish comparable values without inventing missing roles."""
    mode, product, count = result['mode'], result['product'], result['panes']
    result['round'] = index + 1
    ledger['runs'].append(result)
    for state in ('idle', 'output'):
        snapshot = result.get(state)
        for role in (('server', 'client', 'total') if mode == 'server' else ('total',)):
            value = snapshot['rss_kib'].get(role) if snapshot else None
            if value is None or 'cleanup_error' in result or 'evidence_error' in result:
                reason = ('server/client processes not separately identifiable' if snapshot
                          else result.get('error', 'snapshot was not reached'))
                if 'cleanup_error' in result:
                    reason = result['cleanup_error']
                if 'evidence_error' in result:
                    reason = result['evidence_error']
                sample = {'status': 'failed', 'error': reason}
            else:
                sample = {'status': 'ok', 'value': value}
            record_sample(ledger, mode, f'rss.{state}.{count}.{role}', product, index, sample)
    if mode == 'server' and count == 20:
        sample = ({'status': 'ok', 'value': result['cpu']['percent_one_core']}
                  if 'cpu' in result and result['status'] == 'ok' else
                  {'status': 'failed', 'error': result.get('error', result.get('evidence_error', 'CPU not measured'))})
        record_sample(ledger, mode, 'idle_cpu', product, index, sample)


def store_styled_memory_result(ledger, index, result):
    result['round'] = index + 1
    ledger['runs'].append(result)
    if result['status'] == 'ok' and 'evidence_error' not in result:
        value = result['output']['rss_kib']['total'] - result['idle']['rss_kib']['total']
        sample = {'status': 'ok', 'value': value}
    else:
        sample = {'status': 'failed', 'error': result.get('error', result.get('cleanup_error',
                                                   result.get('evidence_error', 'measurement failed')))}
    record_sample(ledger, 'no-session', 'rss.styled.increment', result['product'], index, sample)


def calculate_memory_increments(ledger):
    lookup = {(m['mode'], m['key']): m for m in ledger['metrics']}
    for state in ('idle', 'output'):
        for low, high in ((1, 10), (10, 20)):
            for mode, roles in (('server', ('server', 'client', 'total')),
                                ('no-session', ('total',))):
                for role in roles:
                    first = lookup[(mode, f'rss.{state}.{low}.{role}')]
                    last = lookup[(mode, f'rss.{state}.{high}.{role}')]
                    for product in ('ikesu', 'herdr'):
                        for index, (a, b) in enumerate(zip(first['samples'][product], last['samples'][product])):
                            if a['status'] == b['status'] == 'ok':
                                sample = {'status': 'ok', 'value': pane_increment(a['value'], b['value'], low, high)}
                            elif 'not_started' in (a['status'], b['status']):
                                sample = {'status': 'not_started', 'error': '未実行'}
                            else:
                                sample = {'status': 'failed', 'error': '元のRSS計測に失敗'}
                            record_sample(ledger, mode, f'rss_slope.{state}.{low}.{high}.{role}',
                                          product, index, sample)


def store_timing_result(ledger, index, result):
    result['round'] = index + 1
    ledger['runs'].append(result)
    metric, product = result['metric'], result['product']
    if metric == 'startup':
        fields = [('startup', result.get('seconds'))]
    elif metric == 'flood':
        fields = [(f'flood.{key}', result.get(field)) for key, field in (
            ('write', 'write_seconds'), ('screen', 'screen_seconds'), ('lag', 'lag_seconds'))]
    elif metric.startswith('latency.'):
        summary = result.get('summary', {})
        fields = [(f'{metric}.{p}', summary.get(f'{p}_seconds')) for p in ('p50', 'p99')]
    else:
        raise ValueError('unknown timing metric: ' + metric)
    if result.get('trace'):
        trace = result.get('internal_surface', {})
        for segment in ('queued_to_received', 'received_to_written'):
            for percentile in ('p50', 'p99'):
                value = trace.get(segment, {}).get(percentile + '_seconds')
                valid = value is not None and not trace.get('truncated') and result['status'] == 'ok'
                sample = ({'status': 'ok', 'value': value} if valid else
                          {'status': 'failed', 'error': result.get('trace_error', 'trace missing or truncated')})
                record_sample(ledger, 'diagnostic', f'trace.{segment}.{percentile}', product, index, sample)
        return
    for key, value in fields:
        sample = ({'status': 'ok', 'value': value} if result['status'] == 'ok' and value is not None else
                  {'status': 'failed', 'error': result.get('error', result.get('cleanup_error',
                                                   result.get('evidence_error', 'measurement failed')))})
        record_sample(ledger, 'server', key, product, index, sample)


def store_heap_result(ledger, index, result):
    result['round'] = index + 1
    ledger['runs'].append(result)
    for state in ('idle', 'output'):
        observation = result.get(state)
        for key, field in (('heap', 'heap_kib'), ('rss_minus_heap', 'rss_minus_heap_kib')):
            sample = ({'status': 'ok', 'value': observation[field]}
                      if observation and result['status'] == 'ok' else
                      {'status': 'failed', 'error': result.get('error', result.get('cleanup_error',
                                                       result.get('evidence_error', 'heap not measured')))})
            record_sample(ledger, 'diagnostic', f'{key}.{state}.{result["panes"]}', 'ikesu', index, sample)


def file_sha256(path):
    digest = hashlib.sha256()
    with Path(path).open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def preserve_evidence(result, index, destination):
    """Keep raw terminal observations and heap profiles beside the run JSON."""
    kind = result.get('mode', result.get('metric', 'run'))
    count = f'-{result["panes"]}pane' if 'panes' in result else ''
    suffix = '-trace' if result.get('trace') else ''
    directory = destination / f'{index + 1}-{result["product"]}-{kind}{count}{suffix}'
    directory.mkdir(parents=True, exist_ok=False)
    source = Path(result['root'])
    for name in ('steps.txt', 'ptyrun.log', 'screen.txt', 'idle-screen', 'output-screen', 'styled-emit.py',
                 'process.json', 'timeline.jsonl', 'producer.json', 'flood-start.json', 'flood-end.json'):
        path = source / name
        if path.is_file():
            shutil.copy2(path, directory / name)
    for state in ('idle', 'output'):
        observation = result.get(state, {})
        heap = observation.get('heap') if isinstance(observation, dict) else None
        if heap:
            profile = Path(heap['profile'])
            target = directory / f'{state}.heap'
            shutil.copy2(profile, target)
            heap['profile'] = str(target)
    trace = result.get('internal_trace')
    if trace:
        for key in ('client_log', 'server_log'):
            source_log = Path(trace[key])
            target = directory / source_log.name
            shutil.copy2(source_log, target)
            trace[key] = str(target)
    result['evidence_dir'] = str(directory)


def command_output(*args):
    try:
        return subprocess.check_output(args, text=True, stderr=subprocess.STDOUT, timeout=30).strip()
    except (OSError, subprocess.SubprocessError) as exc:
        return f'取得失敗: {exc}'


def main():
    parser = argparse.ArgumentParser(description='Measure isolated ikesu and herdr sessions on mini.')
    parser.add_argument('--ikesu', type=Path, required=True)
    parser.add_argument('--ikesu-pprof', type=Path, required=True)
    parser.add_argument('--herdr', type=Path, required=True)
    parser.add_argument('--ptyrun', type=Path, required=True)
    parser.add_argument('--commit', required=True)
    parser.add_argument('--output', type=Path)
    args = parser.parse_args()
    if not re.fullmatch(r'[0-9a-f]{40}', args.commit):
        parser.error('--commit requires the full source commit SHA')
    binaries = {'ikesu': args.ikesu.resolve(), 'herdr': args.herdr.resolve()}
    for binary in (*binaries.values(), args.ikesu_pprof, args.ptyrun):
        if not binary.is_file() or not os.access(binary, os.X_OK):
            parser.error(f'executable not found: {binary}')
    root = Path.home() / 'bench-terrace.noindex'
    if not root.is_dir():
        parser.error('mini benchmark directory is missing')
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%SZ')
    output = args.output or Path.home() / '.local/state/ikesu/metrics' / f'runtime-{stamp}-{args.commit[:12]}.json'
    if output.exists():
        parser.error(f'output already exists: {output}')
    metadata = {'commit': args.commit, 'created_utc': stamp, 'host': command_output('hostname'),
                'os': command_output('uname', '-a'), 'go': command_output('go', 'version'),
                'rustc_detected': command_output('rustc', '--version'),
                'herdr_build_toolchain': '配布バイナリのビルド時の Rust 版は未確認',
                'versions': {name: command_output(str(path), '--version') for name, path in binaries.items()},
                'binary_sha256': {name: file_sha256(path) for name, path in
                                  {**binaries, 'ikesu_pprof': args.ikesu_pprof, 'ptyrun': args.ptyrun}.items()},
                'go_build_info': command_output('go', 'version', '-m', str(binaries['ikesu'])),
                'terminal': '160列 × 45行、既存 ptyrun の端末エミュレーター',
                'styled_output': '色付き・日本語出力を2, 2, 4, 8, 16, 32MiBの順に流し、1ペインの出力前後のRSS差を計測。各段階の出力完了後に2秒置く。',
                'shell': '/bin/zsh、隔離した HOME・設定・状態',
                'layout': '各ペインを別タブに配置、表示1ペイン・非表示は残り',
                'scrollback_limit_bytes': SCROLLBACK_BYTES,
                'rss_scope': '対象実行ファイルのserver・全clientのみ。シェル、外側PTY、子プロセスを除外。共有ページはRSS合計で重複する。',
                'cpu': '20ペイン、60秒のCPU秒差÷壁時計秒。1コアを100%とする。',
                'input': '外側PTYから一意の文字を100回送信。通常時はシェルのエコー、出力中は出力を続けるプログラムが固定行に応答する。完成した端末フレームを同じ単調時計で観測。p99はnearest-rank。',
                'trace': '比較と別実行。server送信、client受信、端末書込の対応した区間と未対応数を記録する。時刻は同じminiの壁時計。書込失敗は破棄されるため、欠損がない証明にはならない。',
                'heap': '比較とは別のpprofビルド。GC後のHeapAllocと、その直後のserver RSSの差。差にはスタック・コード・mmap等を含み、同時刻ではない。',
                'order': '3回、各条件でikesu/herdrの順を交互に入れ替える',
                'load_at_start': command_output('uptime'), 'status': 'running'}
    ledger = new_ledger(metadata)
    save_json(output, ledger)
    evidence_root = output.parent / (output.stem + '-files')
    spec = importlib.util.spec_from_file_location('test_go', Path(__file__).with_name('test-go.py'))
    queue = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(queue)
    started = time.monotonic()

    def run(index, func, *arguments):
        load = command_output('uptime')
        start = time.monotonic()
        result = func(*arguments)
        result['load_before'] = load
        result['elapsed_seconds'] = time.monotonic() - start
        try:
            preserve_evidence(result, index, evidence_root)
        except OSError as exc:
            result.update(status='failed', evidence_error=f'{type(exc).__name__}: {exc}')
        return result

    def checkpoint():
        calculate_memory_increments(ledger)
        save_json(output, ledger)

    def interrupted(_signum, _frame):
        raise KeyboardInterrupt

    signal.signal(signal.SIGTERM, interrupted)
    try:
        with queue.heavy_test_slot(root / '.locks/test-go.lock', Path.cwd(), 'dx-metrics-runtime'):
            for index in range(3):
                order = ('ikesu', 'herdr') if index % 2 == 0 else ('herdr', 'ikesu')
                for mode in ('server', 'no-session'):
                    for count in (1, 10, 20):
                        for product in order:
                            print(f'RUN round={index + 1} {mode} {count} {product}', flush=True)
                            result = run(index, measure_memory, binaries[product], args.ptyrun, product,
                                         mode, count, '/tmp', mode == 'server' and count == 20)
                            store_memory_result(ledger, index, result)
                            checkpoint()
                for product in order:
                    print(f'RUN round={index + 1} styled memory {product}', flush=True)
                    result = run(index, measure_styled_memory, binaries[product], args.ptyrun, product, '/tmp')
                    store_styled_memory_result(ledger, index, result)
                    checkpoint()
                for metric in ('startup', 'flood', 'latency.idle', 'latency.flood'):
                    for product in order:
                        print(f'RUN round={index + 1} {metric} {product}', flush=True)
                        if metric == 'startup':
                            result = run(index, measure_startup, binaries[product], args.ptyrun, product, '/tmp')
                        elif metric == 'flood':
                            result = run(index, measure_flood, binaries[product], args.ptyrun, product, '/tmp')
                        else:
                            result = run(index, measure_input_latency, binaries[product], args.ptyrun, product,
                                         '/tmp', metric == 'latency.flood')
                        store_timing_result(ledger, index, result)
                        checkpoint()
                for count in (1, 10, 20):
                    print(f'RUN round={index + 1} diagnostic heap {count}', flush=True)
                    result = run(index, measure_heap, args.ikesu_pprof, args.ptyrun, count, '/tmp')
                    store_heap_result(ledger, index, result)
                    checkpoint()
                print(f'RUN round={index + 1} diagnostic trace', flush=True)
                result = run(index, measure_input_latency, binaries['ikesu'], args.ptyrun, 'ikesu', '/tmp', False, True)
                store_timing_result(ledger, index, result)
                checkpoint()
        ledger['metadata']['status'] = ('failed' if any(
            sample['status'] != 'ok' for metric in ledger['metrics']
            for samples in metric['samples'].values() for sample in samples) else 'complete')
    except BaseException as exc:
        ledger['metadata']['status'] = 'interrupted'
        ledger['metadata']['interruption'] = f'{type(exc).__name__}: {exc}'
        for metric in ledger['metrics']:
            for samples in metric['samples'].values():
                for sample in samples:
                    if sample['status'] == 'not_started':
                        sample.update(status='failed', error='中断により未実施')
    finally:
        ledger['metadata']['elapsed_seconds'] = time.monotonic() - started
        ledger['metadata']['load_at_end'] = command_output('uptime')
        save_json(output, ledger)
        report = output.with_suffix('.html')
        result = subprocess.run([sys.executable, str(Path(__file__).with_name('metrics-report.py')),
                                 str(output), str(report)], check=False)
        print(f'JSON={output}\nHTML={report}\nSTATUS={ledger["metadata"]["status"]}', flush=True)
    return 0 if ledger['metadata']['status'] == 'complete' and result.returncode == 0 else 1


if __name__ == '__main__':
    sys.exit(main())
