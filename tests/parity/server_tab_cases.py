#!/usr/bin/env python3
"""Exercise real persistent-client tab overflow with identical input."""
from pathlib import Path
import sys

root = Path(sys.argv[1])
root.mkdir(parents=True, exist_ok=True)
body = 'send "printf \'\\\\033[3J\\\\033[H\\\\033[2J\'\\r"\nsleep 250ms\n'
for index in range(2, 19):
    body += 'send "\\x02c"\nsleep 200ms\n'
    body += f'send "TAB-{index}\\r"\nsleep 200ms\n'
body += 'screen @OUT@/many.txt\ncells @OUT@/many.json\n'
for name, command in (
    ('narrow', 'resize 90 25'),
    ('wheel-up', 'mouse wheel-up 40 1'),
    ('wheel-down', 'mouse wheel-down 40 1'),
    ('previous', 'send "\\x02p"'),
    ('next', 'send "\\x02n"'),
):
    body += command + f'\nsleep 250ms\nscreen @OUT@/{name}.txt\ncells @OUT@/{name}.json\n'
body += 'kill\n'
(root / 'overflow.steps').write_text(body)
