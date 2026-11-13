package cli

import (
	"encoding/json"
	"fmt"

	"github.com/kazuph/ikesu/internal/config"
)

func (r *runner) configCLI(args []string) int {
	if len(args) == 2 && args[0] == "notify" && args[1] == "--json" {
		return r.configNotify()
	}
	if len(args) != 1 || args[0] != "check" {
		return usageErr(r, "usage: ikesu config check | ikesu config notify --json")
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

// configNotify は config.toml の [notify] を JSON で出す（Slack のプラグインが読む）。
func (r *runner) configNotify() int {
	c, err := config.Load()
	if err != nil {
		fmt.Fprintf(r.errOut(), "config parse error: %v\n", err)
		return 1
	}
	raw, err := json.Marshal(c.NotifyJSON())
	if err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	fmt.Fprintln(r.out(), string(raw))
	return 0
}
