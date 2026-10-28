#!/usr/bin/env python3
"""Report a real controller identity only to the runner-owned API."""
import os
import sys
mode=sys.argv[1]
sys.argv=[sys.argv[0],'pane-self']
from socket_action import request
pane=os.environ.get('HERDR_PANE_ID') or os.environ.get('IKESU_PANE_ID')
params={'pane_id':pane,'source':'codex','agent':'codex','state':'working'}
if mode=='saved':
    identity=os.environ.get('PARITY_CONTROLLER_SESSION_ID')
    if not identity:raise SystemExit('NOT_RUN: actual controller session required')
    params['session_id']=identity
request('pane.report_agent',params)
