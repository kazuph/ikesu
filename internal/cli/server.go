package cli

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/api"
)

func (r *runner) server(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: gtx server <stop>")
	}
	switch args[0] {
	case "stop":
		if len(args) != 1 {
			return usageErr(r, "usage: gtx server stop")
		}
		return r.sendOK(api.MethodServerStop, api.EmptyParams{})
	case "ping":
		if len(args) != 1 {
			return usageErr(r, "usage: gtx server ping")
		}
		return r.printResult("cli:ping", api.MethodPing, api.EmptyParams{})
	default:
		fmt.Fprintf(r.errOut(), "unknown server command: %s\n", args[0])
		return 2
	}
}
