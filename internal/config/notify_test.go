package config

import (
	"strings"
	"testing"
	"time"
)

func TestNotifyAbsentIsDisabledWithoutWarnings(t *testing.T) {
	c, err := Decode([]byte("onboarding=false\n"))
	if err != nil || c.Notify.Enabled || len(c.Warnings) != 0 || c.Notify.ReachTimeout != 30*time.Second {
		t.Fatal(c.Notify, c.Warnings, err)
	}
}

func TestNotifyValid(t *testing.T) {
	c, err := Decode([]byte(`[notify]
reach_timeout = "45s"
[[notify.channel]]
id = "mac-panel"
send = "now"
[[notify.channel]]
id = "android"
send = "now"
[[notify.channel]]
id = "slack"
send = "unanswered"
after = "5m"
fallback_for = ["android"]
`))
	if err != nil || len(c.Warnings) != 0 || !c.Notify.Enabled || c.Notify.ReachTimeout != 45*time.Second || len(c.Notify.Channels) != 3 {
		t.Fatal(c.Notify, c.Warnings, err)
	}
	s, _ := c.Notify.Channel("slack")
	if s.Send != "unanswered" || s.After != 5*time.Minute || len(s.FallbackFor) != 1 || s.FallbackFor[0] != "android" {
		t.Fatal(s)
	}
}

func TestNotifyInvalidChannelsAreWarnedAndIgnored(t *testing.T) {
	c, err := Decode([]byte(`[notify]
reach_timeout = "soon"
[[notify.channel]]
id = "line"
send = "now"
[[notify.channel]]
id = "android"
send = "later"
[[notify.channel]]
id = "slack"
send = "unanswered"
after = "5 minutes"
[[notify.channel]]
id = "slack"
send = "unanswered"
fallback_for = ["slack"]
[[notify.channel]]
id = "slack"
send = "unanswered"
fallback_for = ["nope"]
[[notify.channel]]
id = "mac-panel"
send = "now"
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Notify.Channels) != 1 || c.Notify.Channels[0].ID != "mac-panel" || c.Notify.ReachTimeout != DefaultNotifyReachTimeout {
		t.Fatal(c.Notify)
	}
	joined := strings.Join(c.Warnings, "\n")
	for _, want := range []string{"reach_timeout", "channel[0].id", "channel[1].send", "channel[2].after", "channel[3].fallback_for", "channel[4].fallback_for"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing warning %q in %s", want, joined)
		}
	}
	if j := c.NotifyJSON(); len(j.Warnings) != len(c.Warnings) {
		t.Fatal(j.Warnings, c.Warnings)
	}
}
