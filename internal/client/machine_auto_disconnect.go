package client

import (
	"cmp"
	"fmt"
	"time"

	"github.com/kazuph/ikesu/internal/machine"
)

func (m *machineClient) autoDisconnectAfter() time.Duration {
	if m.settings == nil || m.settings.config == nil {
		return 0
	}
	return m.settings.config.Remote.AutoDisconnectAfter
}

func (e *managedEndpoint) autoDisconnectDeadline(after time.Duration) time.Time {
	if after <= 0 || !e.profile.Enabled || e.connection != nil || e.retry.offlineSince.IsZero() {
		return time.Time{}
	}
	deadline := e.retry.offlineSince.Add(after)
	if e.retry.disconnectSaveAfter.After(deadline) {
		return e.retry.disconnectSaveAfter
	}
	return deadline
}

func (m *machineClient) autoDisconnect(e *managedEndpoint, after time.Duration, now time.Time) bool {
	profile := e.profile
	reason := cmp.Or(e.reason, "no connection established")
	catalog := machine.Catalog{Path: m.catalogPath()}
	// Keep the profile enabled if the save fails and avoid a tight timer loop.
	e.retry.disconnectSaveAfter = now.Add(attentionRetry)
	if _, err := catalog.Change(profile.ID, "disable", "", &profile); err != nil {
		m.notify(fmt.Sprintf("%s: automatic disconnect failed: %v", profile.Label, err))
		return false
	}
	if err := m.syncMachineCatalog(); err != nil {
		m.notify(err.Error())
		return false
	}
	toast := machineFailureToast(profile.Label+": automatically disconnected", fmt.Sprintf("After %s without a connection: %s", after, reason))
	m.autoDisconnectNotices = append(m.autoDisconnectNotices, toast)
	return true
}
