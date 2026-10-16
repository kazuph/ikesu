#!/usr/bin/env python3
"""Generate repeatable stage-one keyboard observations for observe.py."""
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)

def send(value):
    return 'send ' + json.dumps(value, ensure_ascii=False) + '\n'

protocols = {
    'plain': '', 'cursor': '\x1b[?1h', 'kitty1': '\x1b[>1u',
    'kitty3': '\x1b[>3u', 'kitty7': '\x1b[>7u',
    'kitty8': '\x1b[>8u', 'kitty31': '\x1b[>31u',
    'kitty3-mok1': '\x1b[>3u\x1b[>4;1m',
}
keys = ['\x1b[97u', '\x1b[97;5u', '\x1b[97:65;2;65u', '\x1b[13;2u',
        '\x1b[A', '\x1bOA', '\x1b[1;2A', '\x1b[97;1:2u',
        '\x1b[97;1:3u', '\x1b[57441u', '\x1b[57441;1:3u',
        '\x1b[5;2~', '日', 'END']
for name, modes in protocols.items():
    body = send(f'python3 @FIXTURES@/key_probe.py @OUT@/received.jsonl {modes.encode().hex() or "00"}\r')
    body += 'waittext 5s PROBE_READY\n'
    for key in keys:
        body += send(key) + 'sleep 40ms\n'
    body += 'screen @OUT@/complete.txt\ncells @OUT@/complete.json\nkill\n'
    (out/f'protocol-{name}.steps').write_text(body)

body = 'screen @OUT@/startup.txt\n'
body += send('python3 @FIXTURES@/keyboard_lifecycle.py\r')
body += 'waittext 5s LIFECYCLE_DONE\nscreen @OUT@/restored.txt\n'
body += send('\x14q') + 'sleep 300ms\nscreen @OUT@/detached.txt\nkill\n'
(out/'lifecycle.steps').write_text(body)

for name, modes in {'plain': '', 'cursor': '\x1b[?1h', 'alternate': '\x1b[?1049h',
                    'focus': '\x1b[?1004h', 'mouse': '\x1b[?1000h', 'kitty': '\x1b[>1u'}.items():
    body = send('\x14t') + 'sleep 400ms\n'
    body += send(f'python3 @FIXTURES@/key_probe.py @OUT@/received.jsonl {modes.encode().hex() or "00"}\r')
    body += 'waittext 5s PROBE_READY\n' + send('\x1b')
    body += 'sleep 300ms\nscreen @OUT@/escape.txt\ncells @OUT@/escape.json\nkill\n'
    (out/f'popup-{name}.steps').write_text(body)

body = send('\x14t') + 'sleep 400ms\n' + send('\x1b\x1b')
body += 'sleep 100ms\nscreen @OUT@/alt-escape.txt\ncells @OUT@/alt-escape.json\nkill\n'
(out/'popup-alt-escape.steps').write_text(body)

for name, modes in {'plain': '', 'cursor': '\x1b[?1h', 'alternate': '\x1b[?1049h'}.items():
    body = send(f'python3 @FIXTURES@/key_probe.py @OUT@/received.jsonl {modes.encode().hex() or "00"} 120\r')
    body += 'waittext 5s PROBE_READY\nscreen @OUT@/live.txt\ncells @OUT@/live.json\n'
    for label, key in [('up', '\x1b[5~'), ('release', '\x1b[5;1:3~'),
                       ('down', '\x1b[6~'), ('shift-up', '\x1b[5;2~')]:
        body += send(key) + f'sleep 100ms\nscreen @OUT@/{label}.txt\ncells @OUT@/{label}.json\n'
    body += 'kill\n'
    (out/f'page-{name}.steps').write_text(body)

body = send('python3 @FIXTURES@/key_probe.py @OUT@/received.jsonl 1b5b3f31303030681b5b3f3130303668\r')
body += 'waittext 5s PROBE_READY\n'
body += send('\x1b') + 'sleep 100ms\ncells @OUT@/escape.json\n'
body += send('\x1b[<0;70;20M') + 'sleep 100ms\n'
body += send('\x1b[<0;70;') + 'sleep 40ms\n' + send('20M') + 'sleep 100ms\n'
body += send('\x1b[<0;70;') + 'sleep 200ms\n' + send('20M') + 'sleep 100ms\n'
body += send('END') + 'sleep 100ms\nscreen @OUT@/complete.txt\ncells @OUT@/complete.json\nkill\n'
(out/'timing.steps').write_text(body)

for name, modes in [('plain', '00'), ('mouse', '1b5b3f31303030681b5b3f3130303668')]:
    body = send(f'python3 @FIXTURES@/key_probe.py @OUT@/received.jsonl {modes}\r')
    body += 'waittext 5s PROBE_READY\n' + send('\x1b')
    body += 'sleep 400ms\ncells @OUT@/escape.json\nkill\n'
    (out/f'escape-{name}.steps').write_text(body)

body = send('\x14t') + 'sleep 400ms\n'
body += send('/usr/bin/vim -Nu NONE -n -i NONE\r') + 'sleep 500ms\n'
body += send('iSTAGE1_VIM_TEXT') + 'sleep 100ms\n' + send('\x1b')
body += 'sleep 100ms\nscreen @OUT@/vim-normal.txt\ncells @OUT@/vim-normal.json\n'
body += send(':w! @OUT@/vim-buffer.txt\r') + 'sleep 200ms\n'
body += send(':q!\r') + 'sleep 200ms\nscreen @OUT@/vim-exit.txt\nkill\n'
(out/'popup-vim.steps').write_text(body)

body = send('seq 1 120\r') + 'sleep 200ms\n'
body += send('seq 1 120 | less -X\r') + 'sleep 300ms\n'
body += send('\x1b[6~') + 'sleep 100ms\nscreen @OUT@/less-down.txt\n'
body += send('\x1b[5~') + 'sleep 100ms\nscreen @OUT@/less-up.txt\n'
body += send('q') + 'sleep 100ms\nscreen @OUT@/less-exit.txt\nkill\n'
(out/'page-less.steps').write_text(body)

body = 'screen @OUT@/first.txt\n' + send('\x14N') + 'sleep 100ms\n'
body += send('STAGE1_SECOND\r') + 'sleep 400ms\nscreen @OUT@/second.txt\n'
for label, key in [('previous', '\x1b[107;9u'), ('next', '\x1b[106;9u'),
                   ('one', '\x1b[49;9u'), ('two', '\x1b[50;9u')]:
    body += send(key) + f'sleep 150ms\nscreen @OUT@/{label}.txt\ncells @OUT@/{label}.json\n'
body += 'kill\n'
(out/'workspace-cmd.steps').write_text(body)
