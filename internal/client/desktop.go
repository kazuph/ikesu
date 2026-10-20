package client

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/config"
	"github.com/kazuph/ikesu/internal/endpoint"
	"github.com/kazuph/ikesu/internal/notify"
	"os"
)

func deliverDesktop(n endpoint.Notification, c config.Config, session, machine string) {
	if err := (notify.Desktop{}).Deliver(n, c, session, machine); err != nil {
		fmt.Fprintln(os.Stderr, "ikesu: notification:", err)
	}
}
