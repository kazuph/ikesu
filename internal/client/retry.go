package client

import "time"

// SPEC G11 P4 fixes these intervals for endpoint connection supervision.
const (
	initialRetryDelay = 500 * time.Millisecond
	remoteRetryLimit  = 120 * time.Second
	localRetryLimit   = 30 * time.Second
	stableConnection  = 60 * time.Second
	attentionRetry    = 30 * time.Second
	machineSwitchWait = 5 * time.Second
	connectionNotice  = 10 * time.Second
	sshConnectWait    = 15 * time.Second
	sshApprovalWait   = 30 * time.Minute
)

type retryPolicy struct {
	next, connectedAt time.Time
	delay             time.Duration
	attention, manual bool
}

func (p *retryPolicy) connected(now time.Time) {
	p.connectedAt = now
	p.next = time.Time{}
	p.attention, p.manual = false, false
}

func (p *retryPolicy) failed(now time.Time, local, attention bool) {
	if !p.connectedAt.IsZero() && now.Sub(p.connectedAt) >= stableConnection {
		p.delay = 0
	}
	p.connectedAt = time.Time{}
	p.attention = attention
	p.manual = false
	if attention {
		p.next = now.Add(attentionRetry)
		return
	}
	limit := remoteRetryLimit
	if local {
		limit = localRetryLimit
	}
	if p.delay == 0 {
		p.delay = initialRetryDelay
	} else {
		p.delay = min(limit, p.delay*2)
	}
	p.next = now.Add(p.delay)
}

// Approval expiry is the one failure that stays stopped until a header click.
func (p *retryPolicy) approvalExpired() {
	p.next = time.Time{}
	p.manual = true
}

func (p *retryPolicy) retryNow(now time.Time) {
	p.manual = false
	p.next = now
}

// A needs-action notification belongs to the retry, never to the initial
// failure or ordinary reconnect progress. Consuming it prevents duplicates.
func (p *retryPolicy) due(now time.Time) (retry, notify bool) {
	if p.manual || p.next.IsZero() || now.Before(p.next) {
		return false, false
	}
	p.next = time.Time{}
	return true, p.attention
}
