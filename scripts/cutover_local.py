#!/usr/bin/env python3
"""Stopped-server cutover. Default is execution; use --dry-run for a read-only plan.

The operator must launch outside both terminal servers with a new OS session.
Old executables are deleted only after both new servers pass verification.
Before deletion every completed operation has a reverse operation. Once deletion
begins, failure leaves the verified new deployment in place for repair.
"""
import argparse
import contextlib
import datetime
import fcntl
import hashlib
import json
import os
from pathlib import Path
import shlex
import shutil
import socket
import stat
import subprocess
import sys
import time


def command(argv, env=None, cwd=None):
    result = subprocess.run([str(x) for x in argv], env=env, cwd=cwd,
                            text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    if result.returncode:
        raise RuntimeError(f'command failed ({result.returncode}): {argv}: {result.stderr or result.stdout}')
    return result.stdout.strip()


def wait_for(check):
    # Same readiness budget as CLI server restart; these are bounded probes,
    # never background-job status polling.
    deadline = time.monotonic() + 15
    last = None
    while time.monotonic() < deadline:
        try:
            return check()
        except (OSError, AssertionError, ValueError, subprocess.CalledProcessError, RuntimeError) as error:
            last = error
        time.sleep(.05)
    raise RuntimeError(f'readiness deadline: {last}')


class Host:
    def __init__(self, settings, migration_source):
        self.s = settings
        self.home = Path(settings['home'])
        self.repo = self.home / 'src/github.com/kazuph/goterrace'
        self.new_repo = self.repo.with_name('ikesu')
        if not settings['sha']:
            settings['sha'] = command(['git', 'rev-parse', 'HEAD'], cwd=self.repo)
        assert all(c in '0123456789abcdef' for c in settings['sha']), 'release SHA must be hex'
        self.state = self.home / '.local/state' / ('gtx' if settings['mini'] else 'gtx-2e01183')
        self.config = self.home / '.config/gtx'
        self.new_state = self.home / '.local/state/ikesu'
        self.lib = self.home / '.local/lib/ikesu'
        self.old_lib = self.home / '.local/lib/gtx'
        self.old = (self.home / '.local/bin/gtx') if settings['mini'] else self.old_lib / 'releases/49f0363/gtx-2e01183'
        self.binary = Path(settings['binary'])
        self.installed = self.lib / 'releases' / settings['sha'] / 'ikesu'
        self.link = self.home / '.local/bin/ikesu'
        self.journal = self.home / '.ikesu-cutover-journal.json'
        self.migration_source = migration_source
        self.record = {'done': [], 'copies': [], 'deleted': False}
        self.payload = None
        self.owns_journal = False

    def env(self, new=False):
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('IKESU_', 'GTX_', 'GOTERRACE_'))}
        root = self.new_state if new else self.state
        config = self.home / '.config/ikesu' if new else self.config
        env.update(HOME=str(self.home),
                   IKESU_DB_PATH=str(root / ('ikesu.db' if new else 'gtx.db')),
                   IKESU_CONFIG=str(config / 'config.toml'))
        if new:
            env['IKESU_STATE_DIR'] = str(root)
        else:
            env['IKESU_SOCKET_PATH'] = self.record.get('socket', self.s['source_socket'])
            env['IKESU_SOCKET_PATH_EXPLICIT'] = '1'
        return env

    def cli(self, *args, new=False):
        return command([self.installed if new else self.old, *(['--session', 'default'] if new else []), *args],
                       env=self.env(new), cwd=self.home)

    def json_cli(self, *args, new=False):
        value = json.loads(self.cli(*args, new=new))
        if 'error' in value:
            raise RuntimeError(value['error'])
        return value['result']

    def counts(self, new=False):
        workspaces = self.json_cli('workspace', 'list', new=new)['workspaces']
        panes = self.json_cli('pane', 'list', new=new)['panes']
        return {'workspaces': len(workspaces), 'panes': len(panes)}

    def save(self):
        self.owns_journal = True
        self.journal.write_text(json.dumps(self.record, indent=2))

    def checkpoint(self, name):
        print(f'COMPLETE {name}', flush=True)
        if self.s.get('fail_after') == name:
            raise RuntimeError(f'injected failure after {name}')

    def preflight(self, dry=False):
        assert self.old.is_file(), f'old executable missing: {self.old}'
        assert self.state.is_dir(), f'state missing: {self.state}'
        for path in (self.home / '.config/ikesu', self.new_state, self.lib):
            assert not os.path.lexists(path), f'destination exists: {path}'
        if os.path.lexists(self.link):
            assert self.link.is_symlink() and self.old_lib.resolve() in self.link.resolve().parents, f'existing ikesu is not an old-release alias: {self.link}'
            self.record['ikesu_link'] = os.readlink(self.link)
        if not self.s['mini']:
            assert self.repo.is_dir(), f'repository missing: {self.repo}'
            assert not os.path.lexists(self.new_repo), f'destination exists: {self.new_repo}'
            assert command(['git', 'branch', '--show-current'], cwd=self.repo) == 'main', 'project checkout must remain main'
            assert (self.repo / 'apps/mac-dialog-decisions/ikesu-plugin.toml').is_file(), 'main must include stage3: canonical new plugin manifest missing'
            command(['git', 'merge-base', '--is-ancestor', self.s['sha'], 'main'], cwd=self.repo)
            self.record['worktrees'] = [row.split(' ', 1)[1] for row in command(
                ['git', 'worktree', 'list', '--porcelain'], cwd=self.repo).splitlines()
                if row.startswith('worktree ')]
            for old in self.claude_sources():
                assert not self.claude_target(old).exists(), f'Claude destination exists: {self.claude_target(old)}'
        assert not self.journal.exists(), f'unresolved cutover journal: {self.journal}'
        version = command([self.binary, '--version'])
        assert version.split()[-1] == '0.4.0', f'not v0.4.0: {version}'
        self.payload = self.binary.read_bytes()
        print(f'BINARY {self.binary} sha256={hashlib.sha256(self.payload).hexdigest()}', flush=True)
        jobs = self.cli('run', 'list')
        active = [line for line in jobs.splitlines() if len(line.split()) > 1 and line.split()[1] in ('queued', 'running', 'cancelling')]
        assert not active, f'active jobs prevent shutdown: {active}'
        self.record['counts'] = self.counts()
        print(f'BEFORE {self.record["counts"]}; active jobs=0', flush=True)
        for source in (self.state, self.config):
            assert not Path(str(source) + '.pre-ikesu').exists(), f'backup exists: {source}.pre-ikesu'
        if dry:
            print('PLAN stop server -> verify socket/locks released -> offline migration', flush=True)
            # Inspect transformations while live without acquiring server locks.
            # No migration, stop, repair, install, journal, or log file is created.
            migration = self.migration()
            for source, _ in migration.pairs:
                if source.exists():
                    migration.rewrite(source, True)
            self.inventory()
            print('PLAN repo move + worktree repair + Claude COPY' if not self.s['mini'] else 'PLAN mini config absent is supported')
            print(f'PLAN install {self.installed} -> start default -> version/counts/DB check -> delete {self.old_lib} and old links')
            print('DRY RUN: no files changed', flush=True)
        else:
            self.save()
        self.checkpoint('preflight')

    def migration(self):
        namespace = {'__name__': 'cutover_migration', '__file__': self.s['migration_file']}
        exec(compile(self.migration_source, self.s['migration_file'], 'exec'), namespace)
        return namespace['Migration'](self.home, self.state, self.config, prepared_repo=self.repo)

    def stopped(self):
        roots = [self.new_state] if 'migrate' in self.record['done'] else list((self.home / '.local/state').glob('gtx*'))
        for path in (path for root in roots if root.is_dir() for path in root.rglob('*')):
            mode = path.lstat().st_mode
            if stat.S_ISSOCK(mode):
                with socket.socket(socket.AF_UNIX) as conn:
                    conn.settimeout(.1)
                    try:
                        conn.connect(str(path))
                    except (ConnectionRefusedError, FileNotFoundError):
                        pass
                    else:
                        raise AssertionError(f'socket still accepts: {path}')
            if path.name.endswith('.lock') and stat.S_ISREG(mode):
                with path.open('rb') as handle:
                    fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
        return True

    def stop(self):
        self.cli('server', 'stop')
        self.record['done'].append('stop')
        self.save()
        wait_for(self.stopped)
        self.checkpoint('stop')

    def migrate(self):
        migration = self.migration()
        try:
            migration.run(False)
        finally:
            for handle in migration.locks:
                handle.close()
        self.record['done'].append('migrate')
        self.save()
        self.checkpoint('migrate')

    def move_repo(self):
        # Record each irreversible-to-the-process boundary before subsequent work.
        self.repo.rename(self.new_repo)
        self.record['done'].append('repo')
        self.save()
        self.repair(self.new_repo)
        manifest = self.new_repo / 'apps/mac-dialog-decisions/ikesu-plugin.toml'
        assert manifest.is_file(), f'relocated new plugin manifest missing: {manifest}'
        print(f'VERIFIED relocated plugin manifest {manifest}', flush=True)
        self.checkpoint('repo')

    def repair(self, root):
        paths = [str(root / Path(path).relative_to(self.repo.resolve())) for path in self.record['worktrees']]
        command(['git', 'worktree', 'repair', *paths], cwd=root)
        actual = command(['git', 'worktree', 'list', '--porcelain'], cwd=root)
        for path in paths:
            assert f'worktree {Path(path).resolve()}\n' in actual + '\n', f'worktree repair missing {path}'
        print(actual, flush=True)

    def claude_sources(self):
        projects = self.home / '.claude/projects'
        prefix = str(self.repo).replace('/', '-')
        return sorted(path for path in projects.glob(prefix + '*')
                      if path.is_dir() and (path.name == prefix or path.name.startswith(prefix + '--worktree-')))

    def claude_target(self, source):
        before = str(self.repo).replace('/', '-')
        after = str(self.new_repo).replace('/', '-')
        return source.with_name(after + source.name[len(before):])

    def copy_claude(self):
        for old in self.claude_sources():
            new = self.claude_target(old)
            self.record['copies'].append(str(new))
            self.save()
            shutil.copytree(old, new, symlinks=True)
            print(f'COPY CLAUDE {old} -> {new}; original retained', flush=True)
        self.record['done'].append('claude')
        self.save()
        self.checkpoint('claude')

    def install(self):
        self.record['done'].append('install')
        self.save()
        self.installed.parent.mkdir(parents=True)
        self.installed.write_bytes(self.payload)
        self.installed.chmod(0o755)
        self.link.parent.mkdir(parents=True, exist_ok=True)
        if self.link.is_symlink(): self.link.unlink()
        self.link.symlink_to(self.installed)
        assert command([self.link, '--version']).split()[-1] == '0.4.0'
        self.checkpoint('install')

    def start(self, new=True):
        binary = self.installed if new else self.old
        env = self.env(new)
        with (self.home / ('ikesu-server.log' if new else 'gtx-recovery.log')).open('ab') as log:
            subprocess.Popen(['nohup', str(binary), '--session', 'default', 'server'],
                             env=env, cwd=self.home, stdin=subprocess.DEVNULL,
                             stdout=log, stderr=log, start_new_session=True)
        wait_for(lambda: self.json_cli('server', 'ping', new=new))

    def start_verify(self):
        self.record['done'].append('start')
        self.save()
        self.start()
        pong = self.json_cli('server', 'ping', new=True)
        assert pong['version'] == '0.4.0', pong
        after = self.counts(True)
        assert after == self.record['counts'], f'count mismatch: {after} != {self.record["counts"]}'
        database = Path(self.cli('log', '--db', new=True))
        assert database == self.new_state / 'ikesu.db', f'unexpected DB: {database}'
        print(f'VERIFIED version=0.4.0 counts={after} database={database}', flush=True)
        self.checkpoint('verify')

    def delete_old(self):
        # Beyond here, do not restore paths to binaries that may have been deleted.
        self.record['deleted'] = True
        self.save()
        for name in ('gtx', 'gtx-2e01183'):
            path = self.home / '.local/bin' / name
            if os.path.lexists(path):
                path.unlink()
        shutil.rmtree(self.old_lib)
        assert not self.old_lib.exists()
        self.checkpoint('delete')

    def inventory(self):
        for parent in (self.home / '.local/state', self.home / '.config'):
            for path in sorted(parent.glob('gtx*')):
                print(f'PRESERVE {path}', flush=True)

    def rollback(self):
        if self.record['deleted']:
            print('ROLLFORWARD REQUIRED: old executables deletion began; keep verified v0.4.0 and repair from log', flush=True)
            return False
        errors = []
        def undo(label, action):
            try:
                action()
                print(f'ROLLBACK OK {label}', flush=True)
            except Exception as error:
                errors.append(f'{label}: {error}')
                print(f'ROLLBACK FAILED {label}: {error}', flush=True)
        done = self.record['done']
        if 'start' in done:
            def stop_new():
                self.cli('server', 'stop', new=True)
                wait_for(self.stopped)
            undo('new server stop', stop_new)
            if errors:
                return False  # Never move live state.
        if 'install' in done:
            def uninstall():
                if self.link.is_symlink(): self.link.unlink()
                if self.record.get('ikesu_link'):
                    self.link.symlink_to(self.record['ikesu_link'])
                if self.lib.exists(): shutil.rmtree(self.lib)
            undo('install', uninstall)
        for copy in reversed(self.record['copies']):
            undo('Claude copy', lambda copy=copy: shutil.rmtree(copy) if Path(copy).exists() else None)
        if 'repo' in done:
            def repo_back():
                self.new_repo.rename(self.repo)
                self.repair(self.repo)
            undo('repository', repo_back)
        if 'migrate' in done:
            for source, target in reversed([(self.state, self.new_state), (self.config, self.home / '.config/ikesu')]):
                def restore(source=source, target=target):
                    if target.exists(): shutil.rmtree(target)
                    backup = Path(str(source) + '.pre-ikesu')
                    if backup.exists(): backup.rename(source)
                undo('migration ' + str(source), restore)
        if 'stop' in done and not errors:
            def restart_old():
                wait_for(self.stopped)
                self.record['socket'] = str(self.state / 'default/gtx.sock')
                self.start(False)
                restored = self.counts(False)
                assert restored == self.record['counts'], f'rollback counts differ: {restored}'
                print(f'RESTORED old counts={restored}', flush=True)
            undo('old server restart', restart_old)
        if not errors and self.owns_journal and self.journal.exists(): self.journal.unlink()
        return not errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('--binary', type=Path, default=Path(__file__).resolve().parents[1] / '.artifacts/rename-ikesu-stage3/ikesu-v040')
    parser.add_argument('--source-socket', type=Path, default=Path.home() / '.local/state/gtx/default/gtx.sock', help='current inherited old-server API socket')
    parser.add_argument('--mini-binary', default='~/bench-terrace.noindex/work/rename-ikesu-stage3/ikesu-review1')
    parser.add_argument('--sha', default=None)
    parser.add_argument('--ssh', default='ssh')
    parser.add_argument('--fail-after', help='Failure-injection checkpoint: [mini:]preflight/stop/migrate/repo/claude/install/verify/delete')
    parser.add_argument('--remote-phase', choices=['prepare', 'delete', 'rollback', 'dry-run'], help=argparse.SUPPRESS)
    args = parser.parse_args()
    source = Path(__file__).read_text() if not args.remote_phase else ''
    migration_file = Path(__file__).with_name('migrate-ikesu.py') if not args.remote_phase else Path('/cutover/migrate-ikesu.py')
    migration_source = migration_file.read_text() if not args.remote_phase else ''
    if args.remote_phase:
        packet = json.load(sys.stdin)
        settings = packet['settings']
        settings['home'] = str(Path.home())
        settings['source_socket'] = str(Path.home() / '.local/state/gtx/default/gtx.sock')
        settings['binary'] = str(Path(settings['binary']).expanduser())
        host = Host(settings, packet['migration'])
        if args.remote_phase == 'rollback' and not host.journal.exists():
            print('ROLLBACK mini: no pending journal', flush=True)
            return 0
        if args.remote_phase in ('delete', 'rollback'):
            host.record = json.loads(host.journal.read_text())
            host.owns_journal = True
        try:
            if args.remote_phase == 'dry-run': host.preflight(True)
            elif args.remote_phase == 'prepare':
                host.preflight(); host.stop(); host.migrate(); host.install(); host.start_verify()
            elif args.remote_phase == 'delete': host.delete_old(); host.inventory(); host.journal.unlink()
            elif not host.rollback(): return 1
        except Exception:
            if args.remote_phase == 'prepare': host.rollback()
            raise
        return 0
    sha = args.sha
    settings = dict(home=str(Path.home()), mini=False, binary=str(args.binary.resolve()), sha=sha,
                    migration_file=str(migration_file), source_socket=str(args.source_socket), fail_after=args.fail_after)
    host = Host(settings, migration_source)
    remote_settings = dict(settings, mini=True, binary=args.mini_binary,
                           fail_after=(args.fail_after or '').removeprefix('mini:') if (args.fail_after or '').startswith('mini:') else None)
    packet = json.dumps({'settings': remote_settings, 'migration': migration_source})
    # The exact same implementation runs remotely; dry-run never copies scripts.
    remote_source = "__file__ = '/cutover/cutover_local.py'\n" + source
    def remote(phase):
        code = remote_source + '\n'
        argv = [args.ssh, 'mini', 'python3 -c ' + shlex.quote(code) + ' --remote-phase ' + phase]
        result = subprocess.run(argv, input=packet, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        print(result.stdout, end='', flush=True)
        if result.returncode: raise RuntimeError(f'mini {phase} failed: exit={result.returncode}')
    def execute():
        mini_attempted = False
        if args.dry_run:
            errors = []
            for name, check in [('local', lambda: host.preflight(True)), ('mini', lambda: remote('dry-run'))]:
                try: check()
                except Exception as error:
                    errors.append(str(error))
                    print(f'DRY RUN REFUSED {name}: {error}', flush=True)
            return int(bool(errors))
        try:
            host.preflight(False)
            mini_attempted = True
            remote('prepare')
            host.stop(); host.migrate(); host.move_repo(); host.copy_claude(); host.install(); host.start_verify()
            # Both hosts have verified new servers before deleting any old binary.
            host.delete_old()
            remote('delete')
            host.inventory()
            host.journal.unlink()
            print('SUCCESS: both hosts v0.4.0; old executables removed; original state/config retained', flush=True)
            return 0
        except Exception as error:
            print(f'FAILED: {error}', flush=True)
            if not args.dry_run:
                host.rollback()
                if mini_attempted and not host.record['deleted']:
                    try: remote('rollback')
                    except Exception as rollback_error: print(f'ROLLBACK FAILED mini: {rollback_error}', flush=True)
            return 1
    if args.dry_run:
        return execute()
    stamp = datetime.datetime.now().strftime('%Y%m%d-%H%M%S-%f')
    logfile = host.home / f'ikesu-cutover-{stamp}.log'
    print(f'Cutover log: {logfile}', flush=True)
    with logfile.open('w') as log, contextlib.redirect_stdout(log), contextlib.redirect_stderr(log):
        return execute()


if __name__ == '__main__':
    sys.exit(main())
