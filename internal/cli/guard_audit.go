package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
)

// parseSince は 7d・12h・2w のような期間を読む。
func parseSince(s string) (time.Duration, error) {
	if len(s) < 2 {
		return 0, fmt.Errorf("--since は 7d・12h・2w の形です")
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("--since は 7d・12h・2w の形です")
	}
	unit := map[byte]time.Duration{'h': time.Hour, 'd': 24 * time.Hour, 'w': 7 * 24 * time.Hour}[s[len(s)-1]]
	if unit == 0 {
		return 0, fmt.Errorf("--since は 7d・12h・2w の形です")
	}
	return time.Duration(n) * unit, nil
}

// guardAudit は ikesu guard audit。コマンド承認の1週間分を束ねて見せる。
func (r *runner) guardAudit(c guard.Config, root string, a []string) int {
	since, top := 7*24*time.Hour, 5
	var jsonMode, replay bool
	htmlPath := ""
	for len(a) > 0 {
		switch a[0] {
		case "--json":
			jsonMode, a = true, a[1:]
		case "--replay":
			replay, a = true, a[1:]
		case "--since", "--top", "--html":
			if len(a) < 2 {
				return usageErr(r, "guard audit "+a[0]+" requires a value")
			}
			switch a[0] {
			case "--since":
				d, err := parseSince(a[1])
				if err != nil {
					return usageErr(r, err.Error())
				}
				since = d
			case "--top":
				n, err := strconv.Atoi(a[1])
				if err != nil || n <= 0 {
					return usageErr(r, "--top requires a positive integer")
				}
				top = n
			case "--html":
				htmlPath = a[1]
			}
			a = a[2:]
		default:
			return usageErr(r, "unknown guard audit option")
		}
	}
	now := time.Now()
	asks, err := guard.ReadAsks(guard.AsksPath(root), now.Add(-since).Unix())
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	decisions, note := r.auditDecisions()
	rep, err := guard.BuildAudit(guard.AuditOptions{Root: root, Now: now, Since: since, Top: top, Asks: asks, Decisions: decisions, ServerNote: note, Replay: replay, Config: c, SampleLog: filepath.Join(root, "guard", "decisions.jsonl"), Env: guard.EnvMap(os.Environ())})
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	if htmlPath != "" {
		if err := os.WriteFile(htmlPath, []byte(guard.RenderAuditHTML(rep)), 0600); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		fmt.Fprintln(r.errOut(), "HTML:", htmlPath)
	}
	if jsonMode {
		if err := guard.RenderAuditJSON(r.out(), rep); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		return 0
	}
	guard.RenderAuditText(r.out(), rep)
	return 0
}

// auditDecisions はサーバーの回答済みの承認を取る。繋がらなければ空と、その旨の注。
func (r *runner) auditDecisions() ([]api.Decision, string) {
	data, err := r.request("cli:guard:audit", api.MethodDecisionList, api.DecisionListParams{})
	if err != nil {
		return nil, "サーバーに繋がらないため、asks.jsonl の記録だけで集計しました: " + err.Error()
	}
	var resp api.Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, "サーバーの応答を読めないため、asks.jsonl の記録だけで集計しました: " + err.Error()
	}
	if resp.Err != nil {
		return nil, "サーバーが承認の一覧を返さないため、asks.jsonl の記録だけで集計しました: " + strings.TrimSpace(resp.Err.Error())
	}
	var list api.DecisionListResult
	if err := json.Unmarshal(resp.Result, &list); err != nil {
		return nil, "サーバーの承認一覧を読めないため、asks.jsonl の記録だけで集計しました: " + err.Error()
	}
	return list.Decisions, ""
}
