package snapshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealGitRunnerStopsChildrenWithinDeadline(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
[ -z "$IKESU_PANE_ID" ] || exit 11
[ "$1 $2 $3 $4" = "--no-optional-locks -c core.fsmonitor=false -C" ] || exit 12
case "$6 $7" in
  "rev-parse --is-inside-work-tree") printf 'true\n' ;;
  "rev-parse --abbrev-ref") printf 'branch\n' ;;
  "status --short")
    /bin/sleep 30 &
    wait ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("IKESU_PANE_ID", "p777")
	start := time.Now()
	info := CollectGit(context.Background(), dir, nil)
	elapsed := time.Since(start)
	if info == nil || info.Branch != "branch" || info.Truncated != "git timeout" || elapsed < 2900*time.Millisecond || elapsed > 4*time.Second {
		t.Fatalf("elapsed=%s, git=%+v", elapsed, info)
	}
}

func TestGitStatusSummaryStreamsAcrossChunks(t *testing.T) {
	t.Parallel()
	var summary statusSummary
	for _, chunk := range []string{strings.Repeat(" M file\n", 49) + "?? last", " kept\n", "ignored", "\n" + strings.Repeat("omitted\n", 10000), "no newline"} {
		_, _ = summary.Write([]byte(chunk))
	}
	want := strings.Repeat(" M file\n", 49) + "?? last kept\nほか 10002 件"
	if got := summary.String(); got != want {
		t.Fatalf("unexpected status summary %q", got)
	}
}
