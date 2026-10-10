package cli

import (
	"fmt"

	"github.com/kazuph/goterrace/internal/config"
)

func (r *runner) configCLI(args []string) int {
	if len(args) != 1 || args[0] != "check" {
		return usageErr(r, "usage: gtx config check")
	}
	c, err := config.Load()
	if err == nil && len(c.Warnings) == 0 {
		fmt.Fprintln(r.out(), "config: ok")
		return 0
	}
	fmt.Fprintln(r.out(), "config: issues found")
	if err != nil {
		fmt.Fprintf(r.out(), "config parse error: %v; using defaults\n", err)
	}
	for _, warning := range c.Warnings {
		fmt.Fprintln(r.out(), warning)
	}
	return 1
}
