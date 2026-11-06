package client

import (
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/model"
	"github.com/kazuph/ikesu/internal/render"
)

func TestGuardApprovalVisibilityAndDismissal(t *testing.T) {
	now := time.Unix(100, 0)
	guard := api.Decision{DecisionID: "d1", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"}}
	projection := &endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{guard}}
	var state approvalState
	state.update(projection, "p1", now, time.Time{})
	if state.open != "d1" {
		t.Fatalf("inactive input should show decision, got %q", state.open)
	}
	state.dismiss()
	state.update(projection, "p1", now, time.Time{})
	if state.open != "" {
		t.Fatal("dismissed decision reopened without user action")
	}
	state.openPane("p1")
	if state.open != "d1" {
		t.Fatal("manual reopen failed")
	}
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 2}, "p1", now, time.Time{})
	if state.open != "" || len(state.pending) != 0 {
		t.Fatal("resolved decision remained visible")
	}
}

func TestGuardApprovalWaitsForPaneActivationAfterInput(t *testing.T) {
	now := time.Unix(100, 0)
	guard := api.Decision{DecisionID: "d1", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"}}
	projection := &endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{guard}}
	var state approvalState
	state.update(projection, "p1", now, now)
	if state.open != "" {
		t.Fatal("typing opened approval")
	}
	state.update(projection, "p1", now.Add(11*time.Second), now)
	if state.open != "" {
		t.Fatal("elapsed time alone opened approval")
	}
	state.focus("p2")
	state.focus("p1")
	if state.open != "d1" {
		t.Fatal("returning to pane did not open approval")
	}
}

func TestGuardApprovalFiltersAndOrdersPendingDecisions(t *testing.T) {
	now := time.Unix(100, 0)
	pane := &api.DecisionOrigin{PaneID: "p1"}
	var state approvalState
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 2, Pending: []api.Decision{
		{DecisionID: "new", Kind: "command_guard", Status: "pending", Origin: pane, CreatedUnixMS: 2},
		{DecisionID: "old", Kind: "command_guard", Status: "pending", Origin: pane, CreatedUnixMS: 1},
		{DecisionID: "ask", Kind: "ask", Status: "pending", Origin: pane},
		{DecisionID: "outside", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1", OutsidePane: true}},
	}}, "p1", now, time.Time{})
	if len(state.pending) != 2 || state.open != "old" {
		t.Fatalf("pending=%v open=%s", state.pending, state.open)
	}
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1}, "p1", now, time.Time{})
	if state.open != "old" || len(state.pending) != 2 {
		t.Fatal("stale projection replaced pending state")
	}
}

func TestGuardApprovalRequiresSelectionBeforeAnswer(t *testing.T) {
	var state approvalState
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{{
		DecisionID: "d1", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"},
		Options: []api.DecisionOption{{ID: "approve"}, {ID: "day_allow"}, {ID: "reject"}},
	}}}, "p1", time.Unix(100, 0), time.Time{})
	if state.answer() != nil {
		t.Fatal("Enter without a selection must not answer")
	}
	state.selectOption(2)
	if state.open != "d1" || state.selected != "day_allow" {
		t.Fatal("number must select without answering")
	}
	answer := state.answer()
	if answer == nil || answer.DecisionID != "d1" || answer.OptionID == nil || *answer.OptionID != "day_allow" {
		t.Fatalf("answer=%+v", answer)
	}
}

func TestGuardApprovalMayOpenWhilePrefixSnapshotIsPending(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	e := &managedEndpoint{snapshot: &render.Input{State: state, Prefix: true, ModeHint: " PREFIX "}}
	m := &machineClient{}
	if !m.approvalCanOpen(e) {
		t.Fatal("prefix snapshot hid the newly opened approval")
	}
	e.snapshot.Prefix = false
	if m.approvalCanOpen(e) {
		t.Fatal("other modes must keep their input")
	}
}

func TestGuardApprovalRendersControlCharactersSafely(t *testing.T) {
	state := model.New(t.TempDir(), 120, 40)
	body := "request\x1b]52;c;bad\a"
	var approval approvalState
	approval.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{{DecisionID: "d1", Kind: "command_guard", Status: "pending", Title: "title\x1b", Body: &body, Origin: &api.DecisionOrigin{PaneID: "p1", Command: "echo\rX"}}}}, "p1", time.Unix(100, 0), time.Time{})
	in := render.Input{State: state}
	approval.overlay(&in)
	if in.Approval == nil {
		t.Fatal("missing overlay")
	}
	for _, line := range append(in.Approval.Lines, in.Approval.Title) {
		if strings.ContainsFunc(line, unicode.IsControl) {
			t.Fatalf("control character in display: %q", line)
		}
	}
}

func TestLateAnswerManualOpenAndSelection(t *testing.T) {
	pane := &api.DecisionOrigin{PaneID: "p1", Agent: "codex", Reason: "screen_prompt"}
	late := api.Decision{DecisionID: "late", Kind: "agent_prompt", Status: "pending", Origin: pane, LateAnswer: true, MultiSelect: true, AllowText: true, Options: []api.DecisionOption{{ID: "1", Label: "赤"}, {ID: "2", Label: "青"}}}
	visible := late
	visible.DecisionID = "visible"
	visible.LateAnswer = false
	guard := api.Decision{DecisionID: "guard", Kind: "command_guard", Status: "pending", Origin: pane, CreatedUnixMS: 2}
	var state approvalState
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{late, visible}}, "p1", time.Unix(100, 0), time.Time{})
	if len(state.pending) != 1 || state.open != "" {
		t.Fatalf("late auto-opened or visible question included: %+v", state)
	}
	state.focus("p2")
	state.focus("p1")
	if state.open != "" {
		t.Fatal("late question auto-opened on focus")
	}
	state.openPane("p1")
	state.selectOption(1)
	state.checked = map[string]bool{"1": true, "2": true}
	state.reason = "補足 12"
	answer := state.answer()
	if answer == nil || len(answer.OptionIDs) != 2 || answer.OptionIDs[0] != "1" || answer.OptionIDs[1] != "2" || answer.Text == nil || *answer.Text != state.reason {
		t.Fatalf("multi answer: %+v", answer)
	}
	state.dismiss()
	if state.answer() != nil || len(state.pending) != 1 {
		t.Fatal("escape lost pending decision")
	}
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 2, Pending: []api.Decision{late, guard}}, "p1", time.Unix(101, 0), time.Time{})
	if len(state.pending) != 2 || state.open != "guard" {
		t.Fatal("guard no longer auto-opens", state.open)
	}
}

func TestLateAnswerSingleAndTextOnly(t *testing.T) {
	for _, tc := range []struct {
		selectOption bool
		text         string
	}{{true, ""}, {false, "12"}} {
		var state approvalState
		d := api.Decision{DecisionID: "late", Kind: "agent_prompt", Status: "pending", LateAnswer: true, AllowText: true, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "1", Label: "A"}}}
		state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{d}}, "p1", time.Now(), time.Time{})
		state.openPane("p1")
		if tc.selectOption {
			state.selectOption(1)
		}
		state.reason = tc.text
		answer := state.answer()
		if answer == nil || tc.selectOption && (len(answer.OptionIDs) != 1 || answer.OptionIDs[0] != "1") || tc.text != "" && (answer.Text == nil || *answer.Text != tc.text) {
			t.Fatalf("unexpected answer: %+v", answer)
		}
	}
}

func TestLateAnswerRendersDetailsAndPreviewFallback(t *testing.T) {
	heading, body, description, preview := "Question 1/1", "Which color?", "warm tone", "example red"
	d := api.Decision{DecisionID: "d", Kind: "agent_prompt", Status: "pending", LateAnswer: true, QuestionLabel: &heading, Title: "Choose", Body: &body, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "1", Label: "Red", Description: &description, Preview: &preview}, {ID: "2", Label: "Blue", PreviewUnavailable: true}}}
	var state approvalState
	state.update(&endpoint.DecisionProjection{BootID: "a", Revision: 1, Pending: []api.Decision{d}}, "p1", time.Now(), time.Time{})
	state.openPane("p1")
	in := render.Input{State: model.New(t.TempDir(), 120, 40)}
	state.overlay(&in)
	if in.Approval == nil {
		t.Fatal("missing question")
	}
	text := in.Approval.Title + "\n" + strings.Join(in.Approval.Lines, "\n")
	for _, want := range []string{heading, body, "Red", description, preview, "Blue", "画面で確認"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
}

func TestApprovalPresentationOriginAndRejectInstructions(t *testing.T) {
	app := model.New(t.TempDir(), 120, 40)
	ws := app.ActiveWorkspace()
	ws.Name, ws.CustomName = "作業場", true
	body := "申請の理由"
	d := api.Decision{DecisionID: "guard", Kind: "command_guard", Status: "pending", Title: "実行確認（規則: rm-paths）", Body: &body, AllowText: true, Origin: &api.DecisionOrigin{PaneID: "p1", WorkspaceLabel: "古い名前", Command: "echo test"}, Options: []api.DecisionOption{{ID: "approve", Label: "承認"}, {ID: "day_allow", Label: "1日許可"}, {ID: "reject", Label: "却下"}}}
	state := approvalState{pending: []api.Decision{d}, open: "guard", selected: "reject", reason: "却下します"}
	in := render.Input{State: app}
	state.overlay(&in)
	text := strings.Join(in.Approval.Lines, "\n")
	for _, want := range []string{"p1 · 作業場 · rule rm-paths", " reason 申請の理由", " reject note 却下します", "confirm ↵  ·  back ctrl+u  ·  close esc"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	if strings.Contains(text, "select 1-3") || strings.Contains(text, "\n back ctrl+u") {
		t.Fatalf("reject input shows selection guidance: %q", text)
	}
}

func TestApprovalFooterWrapsOnlyBetweenInstructions(t *testing.T) {
	app := model.New(t.TempDir(), 102, 40)
	app.ActiveWorkspace().Name, app.ActiveWorkspace().CustomName = "作業場", true
	d := api.Decision{DecisionID: "guard", Kind: "command_guard", Status: "pending", Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "1"}, {ID: "2"}, {ID: "3"}}}
	state := approvalState{pending: []api.Decision{d}, open: "guard"}
	in := render.Input{State: app}
	state.overlay(&in)
	var footer []string
	for i, kind := range in.Approval.Kinds {
		if kind.Role == "footer" {
			footer = append(footer, strings.TrimSpace(in.Approval.Lines[i]))
		}
	}
	if got := strings.Join(footer, " | "); got != "select 1-3  ·  confirm ↵  ·  scroll ↑↓ | close esc" {
		t.Fatalf("footer split inside instruction: %q", got)
	}
}

func TestLateApprovalPresentationIncludesCheckedOptions(t *testing.T) {
	app := model.New(t.TempDir(), 120, 40)
	heading, notice, desc, preview := "Question 1/1", "Codex は空の答えで先に進みました。答えると会話で届けます", "明るい色", "赤い見本"
	d := api.Decision{DecisionID: "late", Kind: "agent_prompt", Status: "pending", LateAnswer: true, MultiSelect: true, AllowText: true, Title: "色を選ぶ", QuestionLabel: &heading, Body: &notice, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "red", Label: "赤", Description: &desc, Preview: &preview}, {ID: "blue", Label: "青", PreviewUnavailable: true}}}
	state := approvalState{pending: []api.Decision{d}, open: "late", selected: "blue", checked: map[string]bool{"red": true, "blue": true}}
	in := render.Input{State: app}
	state.overlay(&in)
	if !in.Approval.Late || in.Approval.Title != " answer question" {
		t.Fatalf("unexpected question header: %+v", in.Approval)
	}
	text := strings.Join(in.Approval.Lines, "\n")
	for _, want := range []string{heading, "色を選ぶ", notice, "✓ 1  赤", "✓ 2  青", desc, preview, "画面で確認", "check space"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %q", want, text)
		}
	}
	for i, line := range in.Approval.Lines {
		if strings.Contains(line, "✓ 2  青") && !in.Approval.Kinds[i].Selected {
			t.Error("current option must be highlighted")
		}
	}
}

func TestLateApprovalOmitsTerminalTitleAndAbsentPreview(t *testing.T) {
	app := model.New(t.TempDir(), 120, 40)
	d := api.Decision{DecisionID: "late", Kind: "agent_prompt", Status: "pending", LateAnswer: true, Title: "質問", Origin: &api.DecisionOrigin{PaneID: "p1", PaneLabel: "terminal title", TabLabel: "terminal tab", WorkspaceLabel: "サイドバーの名前"}, Options: []api.DecisionOption{{ID: "1", Label: "赤"}, {ID: "2", Label: "青", PreviewUnavailable: true}}}
	state := approvalState{pending: []api.Decision{d}, open: "late"}
	in := render.Input{State: app, WorkspaceNames: map[int]string{0: "サイドバーの名前-terminal title"}}
	state.overlay(&in)
	text := strings.Join(in.Approval.Lines, "\n")
	if !strings.Contains(text, "p1 · サイドバーの名前") || strings.Contains(text, "terminal title") || strings.Contains(text, "terminal tab") {
		t.Fatalf("wrong origin: %q", text)
	}
	if strings.Count(text, "preview: 画面で確認") != 1 {
		t.Fatalf("wrong preview fallback: %q", text)
	}
}

func TestLateApprovalTextModeShowsInputInstructions(t *testing.T) {
	app := model.New(t.TempDir(), 120, 40)
	d := api.Decision{DecisionID: "late", Kind: "agent_prompt", Status: "pending", LateAnswer: true, AllowText: true, Origin: &api.DecisionOrigin{PaneID: "p1"}, Options: []api.DecisionOption{{ID: "1", Label: "赤"}, {ID: "2", Label: "青"}}}
	state := approvalState{pending: []api.Decision{d}, open: "late", textMode: true, reason: "自由入力"}
	in := render.Input{State: app}
	state.overlay(&in)
	text := strings.Join(in.Approval.Lines, "\n")
	if !strings.Contains(text, "text 自由入力") || !strings.Contains(text, "confirm ↵  ·  back ctrl+u  ·  close esc") {
		t.Fatalf("missing input or guidance: %q", text)
	}
	if strings.Contains(text, "select 1-2") || strings.Contains(text, "input · back ctrl+u") {
		t.Fatalf("text mode shows selection or redundant guidance: %q", text)
	}
}
