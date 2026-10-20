package render

import (
	"encoding/json"
	"github.com/kazuph/ikesu/internal/agentview"
	"github.com/kazuph/ikesu/internal/layout"
	"github.com/kazuph/ikesu/internal/model"
	"testing"
)

func TestAgentViewProjectionPreservesLayoutAndOrdersRealRows(t *testing.T) {
	s := model.New("/tmp", 120, 40)
	s.Apply(model.CmdSplitFocused{Dir: layout.Vertical})
	panes := map[int]*PaneSnap{1: {Agent: "codex", Status: StatusGlyph{Key: "blocked"}, Tokens: map[string]string{"owner": "me"}, StateChangeSeq: 2}, 2: {Agent: "qwen", Status: StatusGlyph{Key: "working"}, StateChangeSeq: 3}}
	snapshot, _ := json.Marshal(s.Snapshot())
	var view agentview.View
	if err := json.Unmarshal([]byte(`{"source":"test","filter":{"op":"eq","field":{"token":"owner"},"value":"me"},"sort":[{"field":"attention"}]}`), &view); err != nil {
		t.Fatal(err)
	}
	s.AgentView = &view
	rows := SidebarRows(s, panes)
	agents := 0
	for _, row := range rows {
		if row.Kind == RowAgent {
			agents++
			if row.Pane != 1 {
				t.Fatal(row)
			}
		}
	}
	if agents != 1 {
		t.Fatal(rows)
	}
	after, _ := json.Marshal(s.Snapshot())
	if string(snapshot) != string(after) {
		t.Fatal("view persisted or changed layout")
	}
	s.AgentView = nil
	agents = 0
	for _, row := range SidebarRows(s, panes) {
		if row.Kind == RowAgent {
			agents++
		}
	}
	if agents != 2 {
		t.Fatal("clear did not restore built-in rows")
	}
}
