#!/usr/bin/env python3
"""Independent mobile switcher actions on three real workspaces and tabs."""
from pathlib import Path
import sys
root = Path(sys.argv[1])
root.mkdir(parents=True, exist_ok=True)
base = (Path(__file__).parent/'mobile-switcher.steps').read_text().split('screen @OUT@/open.txt')[0]
actions = {
 'new-workspace': 'click 5 5', 'new-tab': 'click 5 14',
 'first-tab': 'click 5 15', 'first-workspace': 'click 5 7',
 'second-workspace': 'click 5 9', 'workspace-detail': 'click 5 8',
 'section-collapse': 'click 5 6', 'settings': 'click 5 22',
 'heading': 'click 5 4', 'blank-header': 'click 5 2', 'close-top': 'click 55 1',
}
for name, action in actions.items():
    text = base+'screen @OUT@/before.txt\ncells @OUT@/before.json\n'+action+'\nsleep 250ms\nscreen @OUT@/after.txt\ncells @OUT@/after.json\nkill\n'
    (root/(name+'.steps')).write_text(text)
