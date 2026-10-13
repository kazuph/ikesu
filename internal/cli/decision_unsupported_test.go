package cli

import (
	"testing"
	"time"
)

func TestDecisionUnsupportedMachineRetryIsolation(t *testing.T) {
	for _, text := range []string{"unknown command: decision", "error: unrecognized subcommand 'decision'", "UNKNOWN COMMAND: DECISION"} {
		if !decisionUnsupported(text) {
			t.Fatal(text)
		}
	}
	for _, text := range []string{"unknown command: pane", "unsupported remote decision event: other", "connection refused"} {
		if decisionUnsupported(text) {
			t.Fatal(text)
		}
	}
	state := false
	result := remoteWatchResult{unsupported: true}
	notice, delay := remoteWatchRetry(&state, time.Second, result)
	if !notice || !state || delay != 30*time.Minute {
		t.Fatal(notice, delay)
	}
	notice, delay = remoteWatchRetry(&state, time.Second, result)
	if notice || delay != 30*time.Minute {
		t.Fatal("duplicate unsupported announcement")
	}
	notice, delay = remoteWatchRetry(&state, time.Second, remoteWatchResult{auth: true})
	if notice || delay != 30*time.Minute {
		t.Fatal("unsupported state lost without watch data")
	}
	other := false
	notice, delay = remoteWatchRetry(&other, time.Second, remoteWatchResult{auth: true})
	if !notice || delay != 10*time.Minute {
		t.Fatal("other machine auth delay changed")
	}
	notice, delay = remoteWatchRetry(&state, 0, remoteWatchResult{observed: true})
	if !notice || state || delay != decisionRetryInitial {
		t.Fatal("actual watch line must clear unsupported")
	}
	notice, _ = remoteWatchRetry(&state, 0, result)
	if !notice {
		t.Fatal("new unsupported episode must announce once")
	}
}
