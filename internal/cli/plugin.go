package cli

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/plugin"
	"strings"
)

const pluginHelp = `gtx plugin commands:
  gtx plugin install <owner>/<repo>[/subdir...] [--ref REF] [--yes]
  gtx plugin uninstall <plugin_id|owner/repo[/subdir...]>
  gtx plugin link <path> [--disabled]
  gtx plugin list [--plugin ID] [--json]
  gtx plugin config-dir <plugin_id>
  gtx plugin unlink <plugin_id>
  gtx plugin enable <plugin_id>
  gtx plugin disable <plugin_id>
  gtx plugin action <list|invoke>
  gtx plugin log list [--plugin ID] [--limit N]
  gtx plugin pane <open|focus|close>
`

func (r *runner) pluginCLI(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprint(r.out(), pluginHelp)
		return 0
	}
	version := r.o.Version
	if version == "" {
		version = "0.1.0"
	}
	store := plugin.DefaultStore(version)
	method := "plugin." + args[0]
	p := map[string]any{}
	rawJSON := false
	switch args[0] {
	case "link":
		if len(args) < 2 || len(args) > 3 || (len(args) == 3 && args[2] != "--disabled") {
			return usageErr(r, "usage: gtx plugin link <path> [--disabled]")
		}
		p["path"] = args[1]
		p["enabled"] = len(args) == 2
	case "list":
		for i := 1; i < len(args); {
			switch args[i] {
			case "--json":
				rawJSON = true
				i++
			case "--plugin":
				v, ok := flagValue(args, &i)
				if !ok {
					return usageErr(r, "--plugin requires ID")
				}
				p["plugin_id"] = v
			default:
				return usageErr(r, "usage: gtx plugin list [--plugin ID] [--json]")
			}
		}
	case "unlink", "enable", "disable":
		if len(args) != 2 {
			return usageErr(r, "usage: gtx plugin "+args[0]+" <plugin_id>")
		}
		p["plugin_id"] = args[1]
	case "config-dir":
		if len(args) != 2 {
			return usageErr(r, "usage: gtx plugin config-dir <plugin_id>")
		}
		path, err := store.ConfigDir(args[1])
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		fmt.Fprintln(r.out(), path)
		return 0
	default:
		fmt.Fprintln(r.errOut(), "unsupported: "+strings.Join(args, " "))
		return 1
	}
	resp, _ := store.RegistryRequest(&api.Request{ID: "cli:plugin", Method: method, Params: api.MarshalParams(p)})
	if resp.Err != nil {
		b, _ := json.Marshal(resp)
		fmt.Fprintln(r.errOut(), string(b))
		return 1
	}
	if args[0] == "list" && !rawJSON {
		var list struct {
			Plugins []plugin.Installed `json:"plugins"`
		}
		if err := json.Unmarshal(resp.Result, &list); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		if len(list.Plugins) == 0 {
			fmt.Fprintln(r.out(), "No plugins installed.")
		}
		for _, item := range list.Plugins {
			state := "enabled"
			if !item.Enabled {
				state = "disabled"
			}
			fmt.Fprintf(r.out(), "%s\t%s\t%s\t%s\n", item.ID, item.Version, state, item.Root)
		}
		return 0
	}
	b, _ := json.Marshal(resp)
	fmt.Fprintln(r.out(), string(b))
	return 0
}
