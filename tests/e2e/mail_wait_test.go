package e2e

import (
	"time"

	"github.com/kazuph/ikesu/internal/runtime"
)

// PTY notice tests can exceed five seconds: setup types into the pane and
// mail must wait for the agreed input quiet period before the existing timeout.
const noticeWait = runtime.MailInputQuiet + 10*time.Second
