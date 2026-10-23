# One-cell drag measurement

Run on Mac mini only, under `~/bench-terrace.noindex/work/drag-resize`.
The product source is unchanged. Build `cmd/ikesu` with the existing `pprof`
build tag, and build `tools/bench/ptyrun`. The reference executable is the
installed `~/.local/share/herdr-builds/0.2.66/herdr` (never its source).

```
python3 source/tests/dragresize/run.py "$PWD" ikesu light vertical
python3 source/tests/dragresize/run.py "$PWD" herdr heavy horizontal
python3 source/tests/dragresize/analyze.py "$PWD"
```

The runner creates a short, private `/tmp/drag-*` HOME/state/socket tree.
It creates one real workspace with two real shell PTYs through the API.
It records shell WINCH traps in light mode; heavy mode runs a real terminal
program that clears and rewrites every cell on each SIGWINCH. Logs timestamp
signals, not ioctl calls (multiple signals can coalesce). No dependency is added.

Both clients use a 160x45 PTY. For common operations, ikesu's right sidebar is
hidden with its two documented toggle steps. Both left sidebars are 26 cells.
The reference does not implement the right sidebar; do not fabricate a result.
Vertical/horizontal means moving the divider along X/Y respectively.
The observer finds the pane corner on the actual initial screen, presses the
border, then alternates adjacent one-cell positions 40 times, 250ms apart.
It waits for the expected corner glyph at the new position with a fresh cell
timestamp, outside synchronized-output mode. The timestamp is the PTY read time
of that completed output, at millisecond precision. This measures availability
of an emulator-screen update, not physical display scanout or GPU rendering.
The polling and emulator queue affect observation, not that saved read timestamp.

Every movement records sent time, observed time (zero means not observed), raw
output byte offsets, and byte count over the 250ms window. `outer.bin` permits
independent ANSI coverage analysis. CPU is process CPU-time delta divided by
measured interval; 100% means one core. Server and display client are separate.
The shared host's load is saved; high-load runs are relative comparisons only.
`DRAG_PROFILE=1` writes separate existing Go CPU profiles for server and client;
these are diagnostic runs, excluded from comparison summaries.

Only the isolated server is stopped, through `server.stop`; its exit is verified.
No clipboard commands, sounds, running user sessions, or herdr source are used.
