package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/herdrimport"
	"github.com/kazuph/goterrace/internal/session"
	"golang.org/x/sys/unix"
)

const importUsage = "gtx session import-herdr [--source DIR] [--dry-run] [--yes]"

func (r *runner) sessionCLI(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: "+importUsage)
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprintln(r.out(), importUsage+"\nRead ~/.config/herdr by default. Preview first; --dry-run never writes. Confirm with yes or --yes. Existing gtx workspaces are retained. Live herdr agents are restored as shells.")
		return 0
	}
	if args[0] != "import-herdr" {
		return usageErr(r, "unknown session command: "+args[0])
	}
	source := filepath.Join(r.o.getenv("HOME"), ".config", "herdr")
	dry, yes := false, false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--dry-run":
			dry = true
		case "--yes":
			yes = true
		case "--source":
			if i+1 >= len(args) {
				return usageErr(r, "usage: "+importUsage)
			}
			i++
			source = args[i]
		case "help", "--help", "-h":
			return r.sessionCLI([]string{"help"})
		default:
			return usageErr(r, "unknown import option: "+args[i])
		}
	}
	abs, err := filepath.Abs(source)
	if err != nil {
		return r.importError(err)
	}
	source = abs
	cfg, err := config.Load()
	if err != nil {
		return r.importError(err)
	}
	p, err := herdrimport.Load(source, cfg)
	if err != nil {
		return r.importError(err)
	}
	r.printImport(p)
	if dry {
		return 0
	}
	if !yes {
		fmt.Fprint(r.out(), "取り込みますか？ yes と入力すると追加します [yes/no]: ")
		answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil || strings.TrimSpace(answer) != "yes" {
			fmt.Fprintln(r.out(), "取り込みを中止しました（変更なし）")
			return 0
		}
	}
	conn, err := net.Dial("unix", r.socketPath())
	if err == nil {
		conn.Close()
		return r.printResult("cli:session:import-herdr", api.MethodSessionImportHerdr, api.SessionImportHerdrParams{Source: source, Digest: p.Digest, HerdrRunning: p.Running})
	}
	// A socket failure is not permission to overwrite a running session.
	dir := filepath.Dir(r.socketPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return r.importError(err)
	}
	lock, err := os.OpenFile(api.LockPath(dir), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return r.importError(err)
	}
	defer lock.Close()
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return r.importError(fmt.Errorf("gtx is running but unreachable; no changes made: %w", err))
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	latest, err := herdrimport.Load(source, cfg)
	if err != nil {
		return r.importError(err)
	}
	if latest.Digest != p.Digest || latest.Running != p.Running {
		return r.importError(fmt.Errorf("herdr source changed; preview and confirm again"))
	}
	target, err := session.Load(session.Path(dir))
	if err != nil {
		return r.importError(err)
	}
	if target != nil {
		if warnings := target.Validate(); len(warnings) > 0 {
			return r.importError(fmt.Errorf("target session requires repair: %v", warnings))
		}
	}
	merged, _ := session.Append(target, latest.File)
	if err := session.Save(session.Path(dir), merged); err != nil {
		return r.importError(err)
	}
	fmt.Fprintf(r.out(), "%d スペースを保存しました。gtx --session %s で復元します\n", len(latest.File.Workspaces), r.o.Session)
	return 0
}
func (r *runner) importError(err error) int {
	fmt.Fprintln(r.errOut(), "gtx import-herdr:", err)
	return 1
}
func (r *runner) printImport(p *herdrimport.Plan) {
	tabs := 0
	for _, w := range p.File.Workspaces {
		tabs += len(w.Tabs)
	}
	fmt.Fprintf(r.out(), "取り込み前の一覧：%d スペース / %d タブ / %d pane（herdr稼働中=%t）\n", len(p.File.Workspaces), tabs, len(p.Panes), p.Running)
	for _, w := range p.File.Workspaces {
		fmt.Fprintf(r.out(), "スペース %q section=%s cwd=%q active_tab=%d\n", w.Name, w.Section, w.Cwd, w.ActiveTab+1)
		for i, t := range w.Tabs {
			fmt.Fprintf(r.out(), "  タブ %d %q pane=%d focus=p%d zoom=p%d\n", i+1, t.Name, len(t.Panes), t.Focused, t.Zoomed)
		}
	}
	for _, x := range p.Panes {
		action := "shell"
		if x.Agent != "" && x.Reason == "" {
			action = "再開 " + x.Agent + " 会話ID=" + x.SessionID
		}
		if x.Reason != "" {
			action = "shell: " + x.Reason + " (agent=" + x.Agent + ", 会話ID=" + x.SessionID + ")"
		}
		fmt.Fprintf(r.out(), "  pane p%d %q cwd=%q → %s\n", x.Number, x.Label, x.Cwd, action)
	}
	for _, reason := range p.Unrestored {
		fmt.Fprintln(r.out(), "戻せない項目:", reason)
	}
	// Keep the exact parsed preview available for machine verification.
	raw, _ := json.Marshal(struct {
		Active  int `json:"active_workspace"`
		Sidebar int `json:"sidebar_width"`
	}{p.File.Active, p.File.SidebarWidth})
	fmt.Fprintln(r.out(), string(raw))
}
