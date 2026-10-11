package client

import (
	"testing"
	"time"
)

func TestEndpointRetryBackoffCapsAndOnlyStableConnectionsReset(t *testing.T) {
	now := time.Unix(1, 0)
	for _, local := range []bool{false, true} {
		var policy retryPolicy
		want := 500 * time.Millisecond
		limit := 120 * time.Second
		if local {
			limit = 30 * time.Second
		}
		for attempt := 0; attempt < 12; attempt++ {
			policy.failed(now, local, false)
			if got := policy.next.Sub(now); got != want {
				t.Fatalf("local=%v attempt=%d got %s want %s", local, attempt, got, want)
			}
			if retry, _ := policy.due(policy.next.Add(-time.Nanosecond)); retry {
				t.Fatal("retry happened before the specified interval")
			}
			if retry, notify := policy.due(policy.next); !retry || notify {
				t.Fatal("ordinary reconnect notified or failed to retry")
			}
			policy.connected(now)
			policy.failed(now.Add(stableConnection-time.Nanosecond), local, false)
			want = min(limit, want*2)
			if policy.delay != want {
				t.Fatal("short connection reset backoff")
			}
			want = min(limit, want*2)
		}
		policy.connected(now)
		policy.failed(now.Add(stableConnection), local, false)
		if policy.delay != initialRetryDelay {
			t.Fatal("stable connection did not reset backoff")
		}
	}
}

func TestEndpointAttentionRetryNotifiesOnceAndApprovalExpiryRequiresClick(t *testing.T) {
	now := time.Unix(1, 0)
	var policy retryPolicy
	policy.failed(now, false, true)
	if policy.next.Sub(now) != 30*time.Second {
		t.Fatal("needs-action retry interval changed")
	}
	if retry, notify := policy.due(now); retry || notify {
		t.Fatal("initial failure produced a retry notification")
	}
	if retry, notify := policy.due(now.Add(30 * time.Second)); !retry || !notify {
		t.Fatal("needs-action retry did not notify")
	}
	if retry, notify := policy.due(now.Add(time.Minute)); retry || notify {
		t.Fatal("retry notification repeated")
	}
	policy.approvalExpired()
	if retry, _ := policy.due(now.Add(time.Hour)); retry {
		t.Fatal("expired approval retried without a header click")
	}
	policy.retryNow(now)
	if retry, _ := policy.due(now); !retry {
		t.Fatal("header click did not retry")
	}
}
