"""Reject unapproved hosts and non-GUI SSH sessions before pasteboard access."""
import os
import subprocess


def require_mini_gui():
    if os.uname().nodename not in ('kazuphnoMac-mini.local', 'kazuphnoMac-mini'):
        raise SystemExit('STOP: this procedure is authorized only on mini')
    user = subprocess.check_output(['id', '-un'], text=True).strip()
    console = subprocess.check_output(['stat', '-f', '%Su', '/dev/console'], text=True).strip()
    if user != console:
        raise SystemExit('NOT_RUN: SSH user is not the active GUI user')
    subprocess.run(['launchctl', 'print', f'gui/{os.getuid()}'], stdout=subprocess.DEVNULL, check=True)
