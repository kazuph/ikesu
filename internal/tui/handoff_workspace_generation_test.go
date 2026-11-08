package tui

import (
	"bytes"
	"encoding/gob"
	"io"
	"testing"

	"github.com/kazuph/ikesu/internal/teststate"
)

// Workspaces from servers older than workspace generations reach live handoff
// with an empty generation. HQ selection needs one, so the import fills it.
func TestLiveHandoffFillsMissingWorkspaceGeneration(t *testing.T) {
	teststate.Lock(t)
	for _, tc := range []struct{ name, before string }{{"missing", ""}, {"kept", "KEEPGENERATION"}} {
		t.Run(tc.name, func(t *testing.T) {
			a := specRuntimeApp(t)
			a.st.Workspaces[0].Generation = tc.before
			var encoded bytes.Buffer
			if err := gob.NewEncoder(&encoded).Encode(a.HandoffState()); err != nil {
				t.Fatal(err)
			}
			var transfer HandoffState
			if err := gob.NewDecoder(&encoded).Decode(&transfer); err != nil {
				t.Fatal(err)
			}
			imported := NewApp(a.st, a.sup, io.Discard, a.cwd)
			imported.RestoreHandoff(transfer)
			got := imported.st.Workspaces[0].Generation
			if got == "" {
				t.Fatal("imported workspace has no generation")
			}
			if tc.before != "" && got != tc.before {
				t.Fatalf("generation changed: %q -> %q", tc.before, got)
			}
		})
	}
}
