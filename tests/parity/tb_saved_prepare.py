#!/usr/bin/env python3
"""Copy the approved distribution and test binaries into mini's isolated area."""
import argparse
import hashlib
import json
from pathlib import Path
import shlex
import subprocess

p = argparse.ArgumentParser()
p.add_argument('--tb-dist', type=Path, required=True)
p.add_argument('--gtx', type=Path, required=True)
p.add_argument('--out', type=Path, required=True)
a = p.parse_args()
a.out.mkdir(parents=True, exist_ok=True)
remote = '/Users/kazuph/bench-terrace.noindex/work/tb-optimize/saved'
for path in (a.tb_dist/'bin/terminal-browser', a.gtx):
    if not path.is_file():
        raise SystemExit(f'missing approved input: {path}')

lock_command = ('~/bench-terrace.noindex/work/perf-lock.py python3 -c '
                + shlex.quote("import sys; print('TB_LOCK_READY', flush=True); sys.stdin.read()"))
with (a.out/'lock.log').open('w') as log:
    lock = subprocess.Popen(['/usr/bin/ssh', '-T', 'mini', lock_command],
                            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=log, text=True)
    try:
        for line in lock.stdout:
            log.write(line)
            log.flush()
            if line.strip() == 'TB_LOCK_READY':
                break
        else:
            raise RuntimeError('mini shared lock was not acquired')
        subprocess.run(['/usr/bin/ssh', 'mini', f'mkdir -p {remote}/bin {remote}/tb'], check=True)
        commands = [
            ['rsync', '-a', '--stats', str(a.tb_dist.resolve())+'/', f'mini:{remote}/tb/'],
            ['rsync', '-a', '--stats', str(a.gtx.resolve()), f'mini:{remote}/bin/gtx'],
        ]
        with (a.out/'copy.log').open('w') as copy_log:
            for command in commands:
                subprocess.run(command, stdout=copy_log, stderr=subprocess.STDOUT, check=True)
        probe = subprocess.run(['/usr/bin/ssh', 'mini',
                                f'uname -m; uptime; {remote}/bin/gtx --version; '
                                '~/bench-terrace.noindex/work/detail/herdr-0.2.57 --version; '
                                f'cat {remote}/tb/VERSION'], capture_output=True, text=True, check=True)
        (a.out/'environment.log').write_text(probe.stdout+probe.stderr)
        (a.out/'inputs.json').write_text(json.dumps({
            'gtx_sha256': hashlib.sha256(a.gtx.read_bytes()).hexdigest(),
            'tb_version': (a.tb_dist/'VERSION').read_text().strip(),
            'tb_source': str(a.tb_dist.resolve()), 'remote_directory': remote,
            'lock': lock_command, 'commands': commands,
        }, indent=2)+'\n')
    finally:
        lock.stdin.close()
        try:
            lock.wait(timeout=10)
        except subprocess.TimeoutExpired:
            lock.terminate()
            lock.wait(timeout=10)
        lock.stdout.close()
    if lock.returncode != 0:
        raise SystemExit(f'lock holder exited {lock.returncode}')
print('Prepared mini distribution and gtx; no application server was started.')
