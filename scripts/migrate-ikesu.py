#!/usr/bin/env python3
"""Offline, reversible stage-three migration. Run on each host with servers stopped."""
import argparse
import ast
import errno
import fcntl
import json
import os
import re
from pathlib import Path
import shutil
import socket
import sqlite3
import stat
import sys
import tempfile


class Migration:
    def __init__(self, home, source_state=None, source_config=None, prepared_repo=None):
        self.home = home
        self.prepared_repo = prepared_repo or Path(__file__).resolve().parents[1]
        self.state = home / '.local/state'
        self.config = home / '.config'
        self.pairs = [(source_state or self.state / 'gtx-2e01183', self.state / 'ikesu'),
                      (source_config or self.config / 'gtx', self.config / 'ikesu')]
        self.locks = []
        self.replacements = [(str(self.pairs[0][0] / "gtx.db"), str(self.pairs[0][1] / "ikesu.db")),
                             (str(self.pairs[0][0]), str(self.pairs[0][1])),
                             (str(self.pairs[1][0]), str(self.pairs[1][1])),
                             (str(home / 'src/github.com/kazuph/goterrace'),
                              str(home / 'src/github.com/kazuph/ikesu'))]

    def check(self):
        for source, target in self.pairs:
            if source.is_symlink() or (source.exists() and not source.is_dir()) or (source == self.pairs[0][0] and not source.is_dir()):
                raise RuntimeError(f'source must be a real directory: {source}')
            if os.path.lexists(target):
                raise RuntimeError(f'destination already exists: {target}')
        for root in sorted(set(self.state.glob('gtx*')) | {self.pairs[0][0]}):
            if not root.is_dir():
                continue
            for path in sorted(root.rglob('*')):
                mode = path.lstat().st_mode
                if path.name.endswith('.lock') and stat.S_ISREG(mode):
                    handle = path.open('rb')
                    self.locks.append(handle)
                    try:
                        fcntl.flock(handle, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    except BlockingIOError:
                        raise RuntimeError(f'server lock is held: {path}')
                if stat.S_ISSOCK(mode):
                    with socket.socket(socket.AF_UNIX) as probe:
                        probe.setblocking(False)
                        result = probe.connect_ex(str(path))
                    if result not in (errno.ENOENT, errno.ECONNREFUSED):
                        raise RuntimeError(f'live or unverified socket: {path}: {os.strerror(result)}')
        print('PRESERVE inventory:')
        for path in sorted(self.state.glob('gtx*')) + sorted(self.config.glob('gtx*')):
            if path not in [source for source, _ in self.pairs]:
                print(f'  {path}')
                if path.is_dir() and not path.is_symlink():
                    for db in sorted(path.rglob('*.db')):
                        print(f'    DB {db}')

    def text(self, value):
        # Match path components, avoiding similarly named sibling repositories.
        for old, new in self.replacements:
            start = 0
            while True:
                at = value.find(old, start)
                if at < 0:
                    break
                end = at + len(old)
                if end == len(value) or value[end] in '/\\\"\' \t\r\n':
                    value = value[:at] + new + value[end:]
                    start = at + len(new)
                else:
                    start = end
        return value.replace("/gtx-plugin.toml", "/ikesu-plugin.toml")

    def value(self, value):
        if isinstance(value, str):
            return self.text(value)
        if isinstance(value, list):
            return [self.value(item) for item in value]
        if isinstance(value, dict):
            return {key: self.value(item) for key, item in value.items()}
        return value

    def references(self, value):
        if isinstance(value, list):
            return [self.references(item) for item in value]
        if isinstance(value, dict):
            return {key: self.value(item) if key in ('cwd', 'log_path', 'session_path', 'plugin_root', 'manifest_path') else self.references(item)
                    for key, item in value.items()}
        return value

    def database(self, path, dry):
        # Database rows can contain JSON payloads as well as plain absolute paths.
        uri = path.resolve().as_uri() + '?mode=ro'
        with sqlite3.connect(uri, uri=True) as db:
            statements = []
            tables = {row[0] for row in db.execute("SELECT name FROM sqlite_master WHERE type='table'")}
            job_namespace = 'actors' in tables and 'dispatches' in tables
            if job_namespace:
                print(f'JOB NAMESPACE {path}: gtx-jobs -> ikesu-jobs; gtx-run -> ikesu-run (merge actor references on collision)')
            for (table,) in db.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'"):
                quote = lambda name: '"' + name.replace('"', '""') + '"'
                columns = list(db.execute(f'PRAGMA table_info({quote(table)})'))
                keys = [column[1] for column in columns if column[5]]
                if not keys:
                    keys = ['rowid']
                for column in columns:
                    name = column[1]
                    # Historical message bodies, commands and output are evidence.
                    if name not in ('cwd', 'project', 'log_path', 'session_id', 'payload', 'record'):
                        continue
                    rows = db.execute(f'SELECT {",".join(map(quote, keys))}, {quote(name)} FROM {quote(table)} WHERE typeof({quote(name)}) = \'text\'')
                    for row in rows:
                        before = row[-1]
                        try:
                            decoded = json.loads(before)
                        except (ValueError, TypeError):
                            after = self.text(before)
                        else:
                            changed = self.references(decoded)
                            after = before if changed == decoded else json.dumps(changed, ensure_ascii=False, separators=(',', ':'))
                        if before != after:
                            print(f'REWRITE DB {path} table={table} column={name} key={row[:-1]!r}')
                            sql = f'UPDATE {quote(table)} SET {quote(name)}=? WHERE ' + ' AND '.join(f'{quote(key)} IS ?' for key in keys)
                            statements.append((sql, (after,) + row[:-1]))
        if (statements or job_namespace) and not dry:
            backup = Path(str(path) + '.pre-ikesu')
            if backup.exists():
                raise RuntimeError(f'backup exists: {backup}')
            with sqlite3.connect(uri, uri=True) as original, sqlite3.connect(backup) as saved:
                original.backup(saved)
            with sqlite3.connect(path) as db:
                db.execute('PRAGMA foreign_keys=ON')
                for sql, args in statements:
                    db.execute(sql, args)
                if job_namespace:
                    db.execute("UPDATE dispatches SET room='ikesu-jobs' WHERE room='gtx-jobs'")
                    for old_id, kind in db.execute("SELECT id,kind FROM actors WHERE name='gtx-run'").fetchall():
                        target = db.execute("SELECT id FROM actors WHERE name='ikesu-run' AND kind=?", (kind,)).fetchone()
                        if target:
                            new_id = target[0]
                            for column in ('from_actor', 'to_actor'):
                                db.execute(f'UPDATE dispatches SET {column}=? WHERE {column}=?', (new_id, old_id))
                            for column in ('source', 'model', 'session_id', 'pane_id'):
                                db.execute(f'UPDATE actors SET {column}=COALESCE({column},(SELECT {column} FROM actors WHERE id=?)) WHERE id=?', (old_id,new_id))
                            db.execute('UPDATE actors SET first_seen_at=min(first_seen_at,(SELECT first_seen_at FROM actors WHERE id=?)),last_seen_at=max(last_seen_at,(SELECT last_seen_at FROM actors WHERE id=?)) WHERE id=?',(old_id,old_id,new_id))
                            db.execute('DELETE FROM actors WHERE id=?', (old_id,))
                        else:
                            db.execute("UPDATE actors SET name='ikesu-run' WHERE id=?", (old_id,))

    def registry(self, original):
        changed = self.value(original)
        old_entries = original if isinstance(original, list) else original.get('plugins', [])
        new_entries = changed if isinstance(changed, list) else changed.get('plugins', [])
        for old, new in zip(old_entries, new_entries):
            root = old.get('plugin_root')
            if not root:
                continue
            if old.get('plugin_id') == 'kazuph.mac-dialog-decisions' or Path(root).name == 'mac-dialog-decisions':
                new_root = self.home / 'src/github.com/kazuph/ikesu/apps/mac-dialog-decisions'
                prepared = self.prepared_repo / 'apps/mac-dialog-decisions/ikesu-plugin.toml'
                candidates = [new_root / 'ikesu-plugin.toml', prepared]
            else:
                new_root = Path(self.text(root))
                candidates = [new_root / 'ikesu-plugin.toml', Path(root) / 'ikesu-plugin.toml']
            if not any(candidate.is_file() for candidate in candidates):
                raise RuntimeError(f'new plugin manifest is missing: {new_root / "ikesu-plugin.toml"}; prepare it before migration')
            new['plugin_root'] = str(new_root)
            new['manifest_path'] = str(new_root / 'ikesu-plugin.toml')
            if 'min_gtx_version' in new:
                new['min_ikesu_version'] = new.pop('min_gtx_version')
            print(f'PLUGIN ROOT {root} -> {new_root}; manifest validated')
        return changed

    def config_values(self, text, path):
        # Preserve TOML keys, comments and formatting. This edits string
        # tokens rather than reserializing the user's configuration.
        pattern = re.compile(r"""#[^\n]*|"{3}[\s\S]*?"{3}|'{3}[\s\S]*?'{3}|"(?:\\.|[^"\\])*"|'[^']*'|[^"'#]+""")
        table, key = '', ''
        in_value, depth = False, 0
        out = []
        for match in pattern.finditer(text):
            token = match.group()
            if token.startswith('#'):
                out.append(token)
                continue
            if token.startswith(('"', "'")):
                width = 3 if token.startswith(('"""', "'''")) else 1
                value = token[width:-width]
                if token.startswith('"'):
                    value = ast.literal_eval(token)
                if width == 3 and value.startswith('\n'):
                    value = value[1:]
                is_key = text[match.end():].lstrip().startswith(('=', '.'))
                if in_value and not is_key and value == 'gtx':
                    print(f'CONFIG VALUE {path}: {table + "." if table else ""}{key}: gtx -> ikesu')
                    token = token[:width] + 'ikesu' + token[-width:]
                elif not in_value:
                    key += token
                out.append(token)
                continue
            for char in token:
                if char == '=' and not in_value:
                    in_value = True
                elif char == '\n' and depth == 0:
                    if not in_value and key.strip().startswith('['):
                        table = key.strip().strip('[]')
                    key, in_value = '', False
                elif in_value:
                    if char in '[{':
                        depth += 1
                    elif char in ']}':
                        depth -= 1
                elif not char.isspace():
                    key += char
            out.append(token)
        return ''.join(out)

    def rewrite(self, root, dry):
        for path in sorted(root.rglob('*')):
            relative = path.relative_to(root)
            if path.name == 'gtx' and relative.parts[:2] == ('guard', 'bin'):
                print(f'RETIRE guard entry {path} (preserved in original directory backup)')
                if not dry:
                    path.unlink()
                continue
            if path.is_symlink():
                old = os.readlink(path)
                new = self.text(old)
                if old != new:
                    print(f'REWRITE LINK {path}: {old} -> {new}')
                    if not dry:
                        path.unlink()
                        path.symlink_to(new)
                continue
            if not path.is_file():
                continue
            if path.suffix == '.log' or 'job-logs' in relative.parts:
                continue
            if path.suffix == '.db':
                self.database(path, dry)
                continue
            if path.suffix in ('.db-wal', '.db-shm') or path.name.endswith('.pre-ikesu'):
                continue
            raw = path.read_bytes()
            if b'\0' in raw:
                continue
            try:
                before = raw.decode('utf-8')
            except UnicodeDecodeError:
                continue
            if path.suffix == '.json':
                parsed = json.loads(before)
                changed = self.registry(parsed) if path.name == "plugins.json" else self.value(parsed)
                after = before if parsed == changed else json.dumps(changed, ensure_ascii=False, indent=2) + '\n'
            else:
                after = self.text(before)
                if path.name == 'config.toml':
                    after = self.config_values(after, path)
            if after != before:
                print(f'REWRITE FILE {path} (keep .pre-ikesu)')
                if not dry:
                    backup = Path(str(path) + '.pre-ikesu')
                    if backup.exists():
                        raise RuntimeError(f'backup exists: {backup}')
                    shutil.copy2(path, backup)
                    path.write_text(after)
        # Only storage filenames change; persisted data formats and schemas stay intact.
        for path in sorted(root.rglob('gtx*'), reverse=True):
            if path.is_file() and path.name in ('gtx.db', 'gtx.db-wal', 'gtx.db-shm', 'gtx.lock', 'gtx-spawn.lock'):
                target = path.with_name(path.name.replace('gtx', 'ikesu', 1))
                print(f'RENAME {path} -> {target}')
                if not dry:
                    if target.exists():
                        raise RuntimeError(f'filename collision: {target}')
                    path.rename(target)

    def run(self, dry):
        self.check()
        for source, target in self.pairs:
            print(f'MOVE {source} -> {target}; preserve original at {source}.pre-ikesu' if source.exists() else f'CREATE {target}: source config is absent')
        if dry:
            for source, _ in self.pairs:
                self.rewrite(source, True)
            print('DRY RUN: no files changed')
            return
        staged = []
        undo = []
        try:
            # Prepare all rewrites before removing either original directory.
            for source, target in self.pairs:
                backup = Path(str(source) + '.pre-ikesu')
                if os.path.lexists(backup):
                    raise RuntimeError(f'backup exists: {backup}')
                target.parent.mkdir(parents=True, exist_ok=True)
                stage = Path(tempfile.mkdtemp(prefix='.ikesu-migrate-', dir=target.parent))
                staged.append(stage)
                # Stale Unix sockets are transport endpoints, never persisted data.
                if source.exists():
                    shutil.copytree(source, stage, dirs_exist_ok=True, symlinks=True,
                                ignore=lambda directory, names: [name for name in names if stat.S_ISSOCK((Path(directory) / name).lstat().st_mode)])
                self.rewrite(stage, False)
            for (source, target), stage in zip(self.pairs, staged):
                backup = Path(str(source) + '.pre-ikesu')
                if source.exists():
                    source.rename(backup)
                    undo.append((backup, source))
                # Do not overwrite a destination created after the preflight.
                if os.path.lexists(target):
                    raise RuntimeError(f'destination appeared: {target}')
                stage.rename(target)
                undo.append((target, stage))
        except BaseException:
            for source, target in reversed(undo):
                source.rename(target)
            print('ROLLBACK: originals restored', file=sys.stderr)
            raise
        finally:
            for stage in staged:
                if stage.exists():
                    shutil.rmtree(stage)
        print('MIGRATED: original directories and changed files retained as .pre-ikesu')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--dry-run', action='store_true')
    parser.add_argument('--source-state', type=lambda value: Path(value).expanduser().absolute())
    parser.add_argument('--source-config', type=lambda value: Path(value).expanduser().absolute())
    args = parser.parse_args()
    migration = Migration(Path.home(), args.source_state, args.source_config)
    try:
        migration.run(args.dry_run)
    except Exception as error:
        print(f'migration refused/failed: {error}', file=sys.stderr)
        return 1
    finally:
        for handle in migration.locks:
            handle.close()
    return 0


if __name__ == '__main__':
    sys.exit(main())
