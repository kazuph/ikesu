package cli

import (
	"encoding/json"
	"testing"

	"github.com/kazuph/ikesu/internal/guard"
)

func TestGuardIdentityPolicyRejectsIncompleteResponse(t *testing.T) {
	valid, err := json.Marshal(guard.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGuardPolicy(valid); err != nil {
		t.Fatal(err)
	}
	for _, input := range []string{`{}`, `null`, `{"Mode":"enforce"}`, `[]`} {
		if _, err := decodeGuardPolicy([]byte(input)); err == nil {
			t.Errorf("accepted incomplete policy %s", input)
		}
	}
}
