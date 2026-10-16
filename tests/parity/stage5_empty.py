#!/usr/bin/env python3
"""Close only runner-owned workspaces to observe the real empty UI."""
import sys
sys.argv = [sys.argv[0], 'workspace-list']
from socket_action import request
for ws in request('workspace.list', {})['result']['workspaces']:
    request('workspace.close', {'workspace_id':ws['workspace_id']})
