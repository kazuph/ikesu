package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestG9RunCompletionGuidanceAndJSONOrder(t *testing.T) {
	pane := "p7"
	for _, tc := range []struct {
		pane       *string
		completion string
		required   []string
		forbidden  string
	}{
		{nil, "summary", []string{"[gtx run] exit=<code> label=label job=job", "end your turn", "gtx log job"}, ""},
		{nil, "none", []string{"no completion notice is sent", "gtx log job"}, "will reach the caller"},
		{&pane, "summary", []string{"[gtx run] exit=<code> label=label pane=p7", "without sleeping or polling", "gtx log job"}, ""},
	} {
		hint := runSpawnNextHint("job", "label", tc.pane, tc.completion)
		for _, want := range tc.required {
			if !strings.Contains(hint, want) {
				t.Fatal(hint, want)
			}
		}
		if tc.forbidden != "" && strings.Contains(hint, tc.forbidden) {
			t.Fatal(hint)
		}
		data, err := json.Marshal(runSpawnOutput{Job: "job", Label: "label", Mode: "background", Pane: tc.pane, Next: hint})
		if err != nil {
			t.Fatal(err)
		}
		last := -1
		for _, key := range []string{"job", "label", "mode", "pane", "next"} {
			pos := strings.Index(string(data), `"`+key+`":`)
			if pos <= last {
				t.Fatal("field order", string(data))
			}
			last = pos
		}
	}
}
