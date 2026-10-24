# Approved-plan measurements (version two)

This implements IMPL-BRIEF step 1; no resize optimization is included yet.
The source baseline is 81c3a35 plus opt-in, pprof-build-only measurement counters.
No public API, CLI output contract, saved-session format or application behavior
is changed. Normal binaries leave the measurement hook disabled.

## Conditions and trials

Use 106x38 (a computed representative, not measured Ghostty dimensions), 160x45,
and 250x70. The representative follows the parent's observed central 54x36
area plus the product's existing two 26-column sidebars and two one-row bars.
Image protocol tests already use 8x16 cells; use that fixed geometry on every
side, record it, and continue sending SGR cell-coordinate mouse input.

The core matrix varies all four operations, light/normal/alternate workloads,
and 4/60/120Hz input. Extended conditions independently add three/four nested
panes, hidden tab and workspace panes, 10,000 generated history rows (record
actual retained rows/bytes), static/updating images, long/limit travel and
no-session. A combined four-pane/hidden/long-history/updating-image/long-travel
case is also measured. Never merge an unmeasured condition into a PASS.

Each whole trial creates a fresh private HOME, configuration, state and socket,
then prepares all workloads. Wait for their actual displayed markers and record
history metadata before the clocked input. Warm up ten motions and return to
the initial coordinate. Send 200 movements at the specified rate, independently
of completion. The slow 4Hz rate retains the prior instrument's 250ms spacing.
Release after the final input's full period at its last coordinate. A separate
semantic case deliberately releases at a different coordinate. Do not use that
reference failure as a permissive performance target.

Run whole products alternately three times per condition: candidate, reference,
candidate, reference, candidate, reference. Each trial keeps its own values.
Aggregate p50 and p95 as the median of their three per-trial values. Aggregate
maximum as the maximum across all trials. Keep ranges and raw files. Do not
average a missing value or discard a failed/truncated observation.

## Clocks, progress and gates

Go and Python explicitly use the host's CLOCK_MONOTONIC in nanoseconds. Record
clock resolution. Reader timestamps are taken immediately after the outer PTY
read, before file writes or emulator work. Parse synchronized-output boundaries
even when delimiters cross reads; do not collapse multiple frames into one read.
This measures availability of the actual frame bytes at the outer terminal,
not GPU rendering or physical scanout. Emulator CPU is monitored separately.

The real workload records SIGWINCH receipt and actual dimensions, writes a
sequence/dimensions marker, and the observer decodes it from the displayed grid.
Record separately input, border display, each affected PTY's resize receipt,
application write and application display. The light workload is an interactive
zsh with job-control disabled only inside the fixture (+m), sharing the monitor's
foreground process group so its real WINCH can be observed. The heavy workload
rewrites all its cells. It never modifies product state through a test backdoor.

G1: at 4Hz every input needs both a border and correct application observation
before the next input. Missing observations fail. Compare both p50/p95 with
1.1 times the reference. PTY delivery is measured separately, not substituted
for display. For the right sidebar, hide the candidate's left sidebar and compare
with the reference's left sidebar using the same total columns, pane widths and
workload. It is explicitly a proxy target, not a herdr right-sidebar measurement.

G2: split each 200-input slow trial into twenty consecutive ten-input blocks.
Require every block median to be at most 1.1 times the first, separately for the
border and application display. Missing entries fail, not shrink the block.

G3: at 60/120Hz record meaningful border/application updates, update intervals,
and position age relative to sent inputs. A new border with an old application
size does not count as application catch-up. Intermediate motions can coalesce,
but all input records must be present. Include the initial gap and the tail
until the released position AND application dimensions settle. An unfinished tail
stays in the measurement and final-size verification fails. Compare maximum
interval with 1.2 times reference, interval p95 with 1.1, and tracking-age p95
with 1.1. Release-to-final-settle is also saved separately; its unfinished/late
tail participates in these gates rather than gaining a new arbitrary time limit.
The two-second observation window is an instrument timeout, never a pass limit.

## Images, memory and provenance

Decode real RGB/RGBA/PNG payloads and retain their pixel hashes before returning
image/status completion. Invalid or unsupported data errors explicitly; never
ACK it as a shortcut. Save actual decoded-image and placement records. Normal,
delayed/stale/missed ACK, expiry, reconnect and multiple-viewer correctness also
requires the product's real socket/PTY tests during the implementation stage.

Record GC pause histogram deltas with their actual bucket bounds (not an
invented exact maximum), cumulative allocated bytes, post-GC live bytes, current
heap objects, and sampled process RSS separately. The metrics sampler runs every
100ms and does not force GC/ReadMemStats. A sample interval can straddle the
measurement boundary; keep the sampling times. Process RSS/CPU sampling is every
250ms and includes the observer as a separate role. No GOGC/memory-limit tuning
is used in the baseline. SPEC P1's own memory comparison remains required.

Save product and observer SHA-256, workload hash, versions, dimensions, flags,
condition JSON, raw output offsets and monotonic timeline. Frozen harness and
observer paths are selected through DRAG_HARNESS and DRAG_OBSERVER. Repeated runs
must use identical inputs. All servers stop through their own private API; an
observer requiring termination is matched with pgrep -u and the exact task-owned
executable before its proven PID is signalled. No user session/clipboard/settings
are accessed. Actual Claude Code is a separately authorized real-app condition:
ordinary HOME, copied conversation record, temporary cwd, no conversation input.
Its output-active state is explicitly not met under the parent's instruction;
the dedicated output workload is recorded as a separate condition.
