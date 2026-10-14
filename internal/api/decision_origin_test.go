package api

import (
	"encoding/json"
	"testing"
)

func TestDecisionOldPayloadWithoutOriginLabelsOrOptionIDs(t *testing.T) {
	var d Decision
	if err := json.Unmarshal([]byte(`{"decision_id":"old","origin":{"pane_id":"p1"},"answer":{"option_id":"yes","text":null,"responder":"cli"}}`), &d); err != nil {
		t.Fatal(err)
	}
	if d.Origin == nil || d.Origin.PaneID != "p1" || d.Origin.WorkspaceLabel != "" || d.Origin.TabLabel != "" || d.Origin.PaneLabel != "" || d.Answer == nil || d.Answer.OptionID == nil || *d.Answer.OptionID != "yes" || len(d.Answer.OptionIDs) != 0 {
		t.Fatal(d)
	}
}
