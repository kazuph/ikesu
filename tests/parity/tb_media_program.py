#!/usr/bin/env python3
"""Exercise file, temporary-file and POSIX SHM Kitty media in an owned pane."""
import base64
import ctypes
import json
import mmap
import os
from pathlib import Path
import select
import time
import tempfile
import tty
import uuid

endpoint = os.environ.get('HERDR_SOCKET_PATH') or os.environ.get('IKESU_SOCKET_PATH')
if not endpoint or not str(Path(endpoint).resolve()).startswith('/private/tmp/parity-'):
    raise SystemExit('REFUSED: not an observer-owned pane')
tty.setraw(0)
libc = ctypes.CDLL(None, use_errno=True)
# shm_open is variadic: macOS arm64 passes its mode in the variadic ABI.
libc.shm_open.argtypes = [ctypes.c_char_p, ctypes.c_int]
libc.shm_open.restype = ctypes.c_int
records = []
resources = []
pixels = bytes([255, 0, 0, 0, 255, 0])


def resource(medium):
    if medium != 's':
        directory = Path(tempfile.gettempdir()) if medium == 't' else Path.home()
        path = directory/('tty-graphics-protocol-'+uuid.uuid4().hex)
        path.write_bytes(pixels)
        return str(path)
    name = '/tb-'+uuid.uuid4().hex[:16]
    resources.append(name)
    (Path.home()/'tb-media-resources.json').write_text(json.dumps(resources)+'\n')
    fd = libc.shm_open(name.encode(), os.O_CREAT | os.O_EXCL | os.O_RDWR, ctypes.c_uint(0o600))
    if fd < 0:
        raise OSError(ctypes.get_errno(), 'create owned SHM')
    try:
        os.ftruncate(fd, len(pixels))
        with mmap.mmap(fd, len(pixels), flags=mmap.MAP_SHARED, prot=mmap.PROT_READ | mmap.PROT_WRITE) as memory:
            memory[:] = pixels
    except BaseException:
        libc.shm_unlink(name.encode())
        raise
    finally:
        os.close(fd)
    return name


def exists(medium, name):
    if medium != 's':
        return Path(name).exists()
    fd = libc.shm_open(name.encode(), os.O_RDONLY, ctypes.c_uint(0))
    if fd < 0:
        if ctypes.get_errno() != 2:
            raise OSError(ctypes.get_errno(), 'check owned SHM')
        return False
    os.close(fd)
    return True


for index, medium in enumerate(('f', 't', 's')):
    for action in ('q', 'T'):
        name = resource(medium)
        try:
            payload = base64.b64encode(name.encode())
            os.write(1, f'\x1b[5;{7+index*8}H\x1b_Ga={action},t={medium},f=24,s=2,v=1,i={100+index},c=4,r=2,C=1;'.encode()
                     + payload+b'\x1b\\')
            response = bytearray()
            deadline = time.monotonic()+2
            while not response.endswith(b'\x1b\\'):
                remaining = deadline-time.monotonic()
                if remaining <= 0 or not select.select([0], [], [], remaining)[0]:
                    break
                response.extend(os.read(0, 4096))
            records.append({'medium': medium, 'action': action, 'reply_hex': response.hex(),
                            'exists_after': exists(medium, name), 'pixels_hex': pixels.hex()})
            (Path.home()/'tb-media.json').write_text(json.dumps(records, indent=2)+'\n')
        finally:
            if medium == 's':
                libc.shm_unlink(name.encode())
            else:
                Path(name).unlink(missing_ok=True)
            if records and records[-1]['medium'] == medium and records[-1]['action'] == action:
                records[-1]['exists_after_fixture_cleanup'] = exists(medium, name)
                (Path.home()/'tb-media.json').write_text(json.dumps(records, indent=2)+'\n')
    os.write(1, b'\x1b[1;1HTB_MEDIA_'+medium.upper().encode())
    if os.read(0, 1) != b'n':
        raise SystemExit('unexpected media observation input')
(Path.home()/'tb-media.json').write_text(json.dumps(records, indent=2)+'\n')
os.write(1, b'\x1b[1;1HTB_MEDIA_DONE')
os.read(0, 1)
