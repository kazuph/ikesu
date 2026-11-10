package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func unitsOverlay(t *testing.T, o *api.DecisionOrigin) render.Input {
	t.Helper()
	body := "申請の理由"
	d := api.Decision{DecisionID: "guard", Kind: "command_guard", Status: "pending", Title: "実行確認（規則: rm-paths）", Body: &body, AllowText: true, Origin: o, Options: []api.DecisionOption{{ID: "approve", Label: "承認"}, {ID: "reject", Label: "却下", Role: "reject"}}}
	state := approvalState{pending: []api.Decision{d}, open: "guard", selected: "approve"}
	in := render.Input{State: model.New(t.TempDir(), 60, 40)}
	state.overlay(&in)
	return in
}

func TestApprovalCommandUnitsRender(t *testing.T) {
	long := "bash ./very-long-script-name-that-keeps-going-and-going-and-going-and-going-and-going.sh"
	o := &api.DecisionOrigin{PaneID: "p1", Command: "cd sub && " + long, GuardRule: "script-danger", GuardReason: "x", CommandUnits: []api.CommandUnit{
		{Op: "", Text: "cd sub", Label: "移動", Verdict: "allow"},
		{Op: "&&", Text: long, Label: "実行", Verdict: "ask", Rule: "rm-paths", Reason: "削除操作は確認が必要です", Target: "/bin/rm /outside/victim"},
		{Op: "|", Text: "tail -5 " + strings.Repeat("x", 80), Label: "読む", Verdict: "allow"},
	}}
	in := unitsOverlay(t, o)
	text := strings.Join(in.Approval.Lines, "")
	for _, want := range []string{"3行のうち 2行目が確認対象", " 1 移動 cd sub", " 2 実行 && bash", "中で: /bin/rm /outside/victim", "理由: 削除操作は確認が必要です（規則 rm-paths）"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, " $ ") {
		t.Errorf("全体の表示が残った: %q", text)
	}
	role := func(prefix string) string {
		for n, l := range in.Approval.Lines {
			if strings.HasPrefix(l, prefix) {
				return in.Approval.Kinds[n].Role
			}
		}
		return "none"
	}
	if role(" 1 移動") != "dim" || role(" 2 実行") != "warning" || role(" 3 読む") != "dim" {
		t.Errorf("色の役が違う: %q %q %q", role(" 1 移動"), role(" 2 実行"), role(" 3 読む"))
	}
	// ask の行は切らずに折り返す。他の行は幅で切る。
	if !strings.Contains(strings.Join(in.Approval.Lines, ""), "going.sh") {
		t.Errorf("ask の行が切られた")
	}
	for _, l := range in.Approval.Lines {
		if strings.HasPrefix(l, " 3 読む") && !strings.HasSuffix(l, "…") {
			t.Errorf("他の行が切られていない: %q", l)
		}
		if ansi.StringWidth(l) > 60 {
			t.Errorf("幅を超えた: %q", l)
		}
	}
}

func TestApprovalCommandUnitsNoAskLine(t *testing.T) {
	o := &api.DecisionOrigin{PaneID: "p1", Command: "nice -z ls && echo a", GuardRule: "shell-line", CommandUnits: []api.CommandUnit{
		{Text: "nice -z ls", Label: "実行", Verdict: "allow"}, {Op: "&&", Text: "echo a", Label: "読む", Verdict: "allow"}}}
	text := strings.Join(unitsOverlay(t, o).Approval.Lines, "")
	if !strings.Contains(text, "行を特定できない（規則 shell-line）") {
		t.Errorf("%q", text)
	}
}

func TestApprovalWithoutCommandUnitsKeepsOldView(t *testing.T) {
	text := strings.Join(unitsOverlay(t, &api.DecisionOrigin{PaneID: "p1", Command: "echo test"}).Approval.Lines, "\n")
	if !strings.Contains(text, " $ echo test") {
		t.Errorf("%q", text)
	}
}
