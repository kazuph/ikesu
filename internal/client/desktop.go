package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/notify"
	"os"
)

// warn receives delivery failures, including ones reported later by the
// notification command. A nil warn writes to stderr.
func deliverDesktop(n endpoint.Notification, c config.Config, session, machine string, warn func(error)) {
	if warn == nil {
		warn = func(err error) { fmt.Fprintln(os.Stderr, "ikesu: notification:", err) }
	}
	if err := (notify.Desktop{Warn: warn}).Deliver(n, c, session, machine); err != nil {
		warn(err)
	}
}
