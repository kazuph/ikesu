package client

import (
	"os"
	"path/filepath"

	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/platform"
)

// NestedBlocked reports whether a real gtx server process is an ancestor
// of this process and the config does not opt into nesting. Leaked
// GOTERRACE/GTX_* environment alone is not enough — the guard only bites
// when the process tree actually contains `gtx server` (SPEC G5).
func NestedBlocked() bool {
	cfg, err := config.Load()
	if err == nil && cfg.AllowNested() {
		return false
	}
	for cur, depth := os.Getppid(), 0; cur > 1 && depth < 64; depth++ {
		if isGtxServerProcess(cur) {
			return true
		}
		next := platform.PPID(cur)
		if next <= 0 || next == cur {
			break
		}
		cur = next
	}
	return false
}

// isGtxServerProcess reports whether pid is `gtx server` (the supervisor
// process — pane shells are its children).
func isGtxServerProcess(pid int) bool {
	args := platform.ProcArgs(pid)
	if len(args) < 2 {
		return false
	}
	base := filepath.Base(args[0])
	if base != "gtx" && base != "goterrace" {
		return false
	}
	for _, a := range args[1:] {
		if a == "server" {
			return true
		}
	}
	return false
}
