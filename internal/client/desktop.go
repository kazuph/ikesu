package client

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
	"github.com/kazuph/goterrace/internal/notify"
	"os"
)

func deliverDesktop(n endpoint.Notification, c config.Config, session, machine string) {
	if err := (notify.Desktop{}).Deliver(n, c, session, machine); err != nil {
		fmt.Fprintln(os.Stderr, "gtx: notification:", err)
	}
}
