#!/usr/bin/env python3
"""Observe FIX-1 using identical input, real isolated servers and display clients."""
import argparse
import json
from pathlib import Path
import subprocess

p=argparse.ArgumentParser();p.add_argument('--gtx',required=True,type=Path);p.add_argument('--out',required=True,type=Path);a=p.parse_args()
here=Path(__file__).resolve().parent;work=Path.home()/'bench-terrace.noindex/work/detail'
a.out.mkdir(parents=True,exist_ok=True);procedures=a.out/'procedures';procedures.mkdir(exist_ok=True)
def send(text):return 'send '+json.dumps(text)+'\n'
def action(text):return send('python3 @FIXTURES@/stage6a_action.py '+text+'\r')+'waitfile @HOME@/action-'+text.split()[0]+'-done.json 15s\n'
def snap(name):return 'sleep 300ms\nscreen @OUT@/'+name+'.txt\ncells @OUT@/'+name+'.json\n'
def observe(name,steps,delivery,env=None):
    path=procedures/(name+'.steps');path.write_text(steps+'kill\n')
    config=procedures/(name+'.toml')
    config.write_text((work/'user-config.toml').read_text().replace('[ui.toast]\ndelivery = "system"','')+(f'\n[ui.toast]\ndelivery="{delivery}"\n' if delivery!='in-app' else ''))
    cmd=['python3',str(here/'observe.py'),'--herdr',str(work/'herdr-0.2.57'),'--gtx',str(a.gtx),'--ptyrun',str(work/'ptyrun'),'--config-file',str(config),'--steps',str(path),'--out',str(a.out/name),'--clipboard-sandbox','--server']
    if env is not None:cmd+=['--terminal-env',json.dumps(env)]
    if delivery=='in-app':cmd+=['--in-app-toast']
    print(name,flush=True);subprocess.run(cmd,check=True)

terminals={
 'ghostty':{'TERM_PROGRAM':'ghostty','TERM':'xterm-256color'},
 'iterm':{'TERM_PROGRAM':'iTerm.app','TERM':'xterm-256color'},
 'wezterm':{'TERM_PROGRAM':'WezTerm','TERM':'xterm-256color'},
 'kitty':{'KITTY_WINDOW_ID':'1','TERM':'xterm-256color'},
 'term-ghostty':{'TERM':'xterm-ghostty'},'term-kitty':{'TERM':'xterm-kitty'},'term-wezterm':{'TERM':'wezterm'},
 'tmux':{'TERM_PROGRAM':'ghostty','TERM':'xterm-256color','TMUX':'/tmp/parity-tmux'},
 'unknown':{'TERM':'xterm-256color'},
}
for name,env in terminals.items():observe('terminal-'+name,action('notification-fix-terminal')+snap('final'),'terminal',env)
for count,height in [(1,12),(2,12),(3,13),(13,26)]:
    steps=action(f'repo-count {count}')+'click 5 7\nsleep 200ms\nrclick 5 7\nsleep 300ms\nclicktext "Open worktree..."\n'+snap('initial')
    steps+=send('\x1b[B'*13)+snap('last')+send('/'+('main' if count==1 else 'branch'))+snap('filtered')+send('\x1b[B'*13)+snap('filtered-last')
    visible=(height-7)//2;selected=min(max(1,count-1)-1,visible-1)
    y=(45-height)//2+4+2*selected
    steps+=f'click 40 {y+1}\n'+snap('clicked')+action('list')
    observe(f'worktree-{count}',steps,'in-app')

# Close an in-app notification early with the real prefix+o key.
observe('inapp-rate',action('notification-empty')+send('\x14o')+action('notification-second')+snap('limited')+'sleep 1s\n'+action('notification-third')+snap('after-second'),'in-app')
subprocess.run(['python3',str(here/'stage6a_fix1_gate.py'),str(a.out)],check=True)
