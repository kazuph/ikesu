# Display state API

`client.view.list` → `{type:"client_view_list",views:[{client_id,machine_id,session,workspace_id,pane_ids,focused}]}`. `events.subscribe` accepts `client.view_changed` (wire event `client_view_changed`) for updates and disconnects.

Each view comes from that terminal's painted snapshot, including every tab in its displayed workspace. `focused` is null until a terminal focus/blur report, then boolean. A disconnected terminal is removed. No shared workspace focus or persisted state is used. Unreported clients are omitted. Machine IDs match the local saved catalog (`local` for the local machine). Empty session/workspace/panes means the display cannot currently be identified.
