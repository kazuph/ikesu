#!/usr/bin/env python3
"""Observe an isolated product after a direct file's producer success ack."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import tempfile
import time


def line(connection):
    data = bytearray()
    while b'\n' not in data:
        chunk = connection.recv(65536)
        if not chunk:
            raise RuntimeError('stream closed before a JSON reply')
        data.extend(chunk)
    return json.loads(data.split(b'\n', 1)[0])


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--binary', required=True)
    parser.add_argument('--terminal', required=True)
    parser.add_argument('--out', required=True)
    parser.add_argument('--set-limits-only', action='store_true', help='observe the documented decoded 512 KiB set boundary without attaching a terminal')
    args = parser.parse_args()
    binary, terminal = str(Path(args.binary).resolve()), str(Path(args.terminal).resolve())
    out = Path(args.out).resolve()
    out.mkdir(parents=True, exist_ok=True)
    result = {'version': subprocess.check_output([binary, '--version'], text=True).strip(),
              'binary_sha256': hashlib.sha256(Path(binary).read_bytes()).hexdigest(),
              'observer_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              'terminal_sha256': hashlib.sha256(Path(terminal).read_bytes()).hexdigest(),
              'placement_emulation': 'crop controls unsupported; image receive hashes only',
              'load': subprocess.check_output(['uptime'], text=True).strip()}
    clients, server, stream, endpoint = [], None, None, None
    with tempfile.TemporaryDirectory(prefix='tb-f-', dir='/tmp') as root_name:
        root = Path(root_name)
        env = {key: value for key, value in os.environ.items()
               if not key.startswith(('HERDR', 'GTX', 'GOTERRACE', 'CLAUDE', 'CODEX'))
               and key not in ('SSH_CONNECTION', 'SSH_TTY', 'TMUX', 'STY')}
        for name in ('home', 'config', 'state', 'data', 'runtime', 'cwd'):
            (root / name).mkdir(mode=0o700)
        env.update(HOME=str(root/'home'), XDG_CONFIG_HOME=str(root/'config'),
                   XDG_STATE_HOME=str(root/'state'), XDG_DATA_HOME=str(root/'data'),
                   XDG_RUNTIME_DIR=str(root/'runtime'), HERDR_STATE_DIR=str(root/'state/h'),
                   GTX_STATE_DIR=str(root/'state/g'), TERM='xterm-ghostty', TERM_PROGRAM='ghostty',
                   SHELL='/bin/zsh')
        config = root/'config.toml'
        config.write_text('onboarding=false\n[experimental]\nkitty_graphics=true\n')
        env.update(HERDR_CONFIG_PATH=str(config), GTX_CONFIG=str(config))

        def raw_request(method, params):
            with socket.socket(socket.AF_UNIX) as connection:
                connection.settimeout(15)
                connection.connect(str(endpoint))
                connection.sendall(json.dumps({'id': 'lifetime', 'method': method, 'params': params}).encode()+b'\n')
                reply = line(connection)
                return reply

        def request(method, params):
            reply = raw_request(method, params)
            if 'error' in reply:
                raise RuntimeError(reply)
            return reply['result']

        def attach(label):
            log = (out/f'{label}.log').open('w')
            process = subprocess.Popen([terminal, '-out', str(out/label), '--', binary, '--session', 'file-life'],
                                       cwd=root/'cwd', env=env, stdout=log, stderr=subprocess.STDOUT)
            clients.append((process, log))
            return process

        def wait_for(predicate, description):
            deadline = time.monotonic()+15
            while True:
                value = predicate()
                if value:
                    return value
                if time.monotonic() >= deadline:
                    raise RuntimeError(f'timeout: {description}')
                time.sleep(.05)  # Owned fixture readiness, not background-job polling.

        def graphics(label):
            path = out/label/'graphics.jsonl'
            if not path.exists():
                return []
            return [json.loads(row) for row in path.read_text().splitlines() if row]

        try:
            with (out/'server.log').open('w') as log:
                server = subprocess.Popen([binary, '--session', 'file-life', 'server'], cwd=root/'cwd',
                                          env=env, stdout=log, stderr=subprocess.STDOUT)
                def discover():
                    paths = [path for path in root.rglob('*.sock') if path.name in ('herdr.sock', 'gtx.sock')]
                    return paths[0] if len(paths) == 1 else None
                endpoint = wait_for(discover, 'owned server socket')
                created = request('workspace.create', {'cwd': str(root/'cwd'), 'focus': True})
                pane = created['root_pane']['pane_id']
                result['pane'] = pane
                if args.set_limits_only:
                    result['set_limit_cases'] = []
                    for size in (512*1024-4, 512*1024, 512*1024+4):
                        pixels = bytes((17, 18, 19, 255))*(size//4)
                        reply = raw_request('pane.graphics.set', {
                            'pane_id': pane, 'format': 'rgba', 'image_width': size//4, 'image_height': 1,
                            'data_base64': base64.b64encode(pixels).decode(),
                            'placement': {'grid_cols': 1, 'grid_rows': 1}})
                        result['set_limit_cases'].append({'decoded_bytes': size, 'reply': reply})
                        if 'result' in reply:
                            request('pane.graphics.clear', {'pane_id': pane})
                    result['procedure_complete'] = True
                    return
                attach('first')
                def direct_info():
                    try:
                        info = request('pane.graphics.info', {'pane_id': pane})
                        result['last_info'] = info
                        return info if info.get('file_frame_transport') == 'direct-kitty' else None
                    except RuntimeError as error:
                        result['last_info_error'] = str(error)
                info = wait_for(direct_info, 'direct-kitty advertisement')
                result['info'] = info
                source = Path(info['file_frame_directory'])/'lifetime.rgba'
                pixels = bytes([255, 0, 0, 255])*6
                source.write_bytes(pixels)
                source.chmod(0o600)
                result['producer_sha256'] = hashlib.sha256(pixels).hexdigest()
                stream = socket.socket(socket.AF_UNIX)
                stream.settimeout(15)
                stream.connect(str(endpoint))
                stream.sendall(json.dumps({'id': 'source', 'method': 'pane.graphics.stream',
                                           'params': {'pane_id': pane}}).encode()+b'\n')
                result['opened'] = line(stream)
                header = {'format': 'rgba', 'image_width': 2, 'image_height': 3,
                          'file': {'path': str(source)}, 'sequence': 7, 'revision': 8,
                          'placement': {'viewport_col': 0, 'viewport_row': 0, 'grid_cols': 2, 'grid_rows': 3}}
                stream.sendall(json.dumps(header).encode()+b'\n')
                result['ack'] = line(stream)
                if result['ack'].get('result', {}).get('type') != 'pane_graphics_frame_ack':
                    raise RuntimeError('source reuse denied: no successful producer ack')
                result['terminal_records_at_ack'] = graphics('first')
                result['terminal_pixels_received_before_ack'] = any(
                    result['producer_sha256'] in item['hashes'].values()
                    for item in result['terminal_records_at_ack'])
                if not result['terminal_pixels_received_before_ack']:
                    raise RuntimeError('producer ack preceded actual terminal pixel receipt')
                def received_pixels(label):
                    records = graphics(label)
                    return records if any(result['producer_sha256'] in item['hashes'].values() for item in records) else None
                result['first_received'] = wait_for(lambda: received_pixels('first'), 'actual terminal pixels')
                source.unlink()
                result['source_removed_after_ack'] = not source.exists()
                attach('second')
                def second_ready():
                    screen = out/'second/screen.txt'
                    return screen.exists() and 'NORMAL' in screen.read_text()
                wait_for(second_ready, 'second viewer rendered UI')
                try:
                    stream.settimeout(5)
                    result['stream_after_second_viewer_hex'] = stream.recv(1).hex()
                except socket.timeout:
                    result['stream_after_second_viewer_timeout'] = True
                result['second_received'] = graphics('second')
                try:
                    result['second_info'] = request('pane.graphics.info', {'pane_id': pane})
                except RuntimeError as error:
                    result['second_info_error'] = str(error)
                result['procedure_complete'] = True
        except Exception as error:
            result['error'] = str(error)
            raise
        finally:
            result['runtime_logs'] = {str(path.relative_to(root)): path.read_text(errors='replace')
                                      for path in root.rglob('*.log') if path.is_file()}
            if stream is not None:
                stream.close()
            cleanup = []
            for process, log in clients:
                if process.poll() is None:
                    process.send_signal(signal.SIGTERM)
                try:
                    process.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    process.kill()
                    process.wait()
                    cleanup.append('FAIL: terminal required kill')
                log.close()
            if server is not None:
                if endpoint is not None:
                    try:
                        cleanup.append(request('server.stop', {}))
                    except Exception as error:
                        cleanup.append(str(error))
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()
                    cleanup.append('FAIL: server required kill')
            result['cleanup'] = cleanup
            (out/'result.json').write_text(json.dumps(result, indent=2)+'\n')


if __name__ == '__main__':
    main()
