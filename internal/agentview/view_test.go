package agentview

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestViewSchemaFiltersAndStableProjection(t *testing.T) {
	var view View
	raw := `{"source":"plugin.test","label":"Current blocked","filter":{"op":"all","filters":[{"op":"eq","field":"workspace_id","value":{"context":"current_workspace_id"}},{"op":"any","filters":[{"op":"eq","field":"status","value":"blocked"},{"op":"in","field":{"token":"owner"},"values":["me"]}]},{"op":"not","filter":{"op":"eq","field":"seen","value":true}},{"op":"exists","field":{"token":"owner"}}]},"sort":[{"field":"state_change_seq","order":"desc"}]}`
	if err := json.Unmarshal([]byte(raw), &view); err != nil {
		t.Fatal(err)
	}
	if err := view.Validate(); err != nil {
		t.Fatal(err)
	}
	rows := []Row{{1, map[string]any{"workspace_id": "s1", "status": "blocked", "seen": false, "token.owner": "me", "state_change_seq": uint64(4)}}, {2, map[string]any{"workspace_id": "s2", "status": "blocked", "seen": false, "token.owner": "me", "state_change_seq": uint64(5)}}, {3, map[string]any{"workspace_id": "s1", "status": "working", "seen": false, "token.owner": "me", "state_change_seq": uint64(6)}}}
	before := append([]Row(nil), rows...)
	got := view.Project(rows, map[string]string{"current_workspace_id": "s1"})
	if len(got) != 2 || got[0].Pane != 3 || got[1].Pane != 1 {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(rows, before) {
		t.Fatal("projection mutated semantic rows")
	}
}
func TestViewRejectsUnsupportedShapes(t *testing.T) {
	for _, raw := range []string{`{"source":""}`, `{"source":"x","filter":{"op":"maybe"}}`, `{"source":"x","filter":{"op":"eq","field":"cwd","value":"a"}}`, `{"source":"x","filter":{"op":"eq","field":"seen","value":null}}`, `{"source":"x","filter":{"op":"not"}}`, `{"source":"x","filter":{"op":"in","field":"agent"}}`, `{"source":"x","sort":[{"field":"status","order":"random"}]}`} {
		var v View
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			t.Fatal(err)
		}
		if err := v.Validate(); err == nil {
			t.Fatal(raw)
		}
	}
}
