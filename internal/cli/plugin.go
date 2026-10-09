package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/plugin"
	"golang.org/x/term"
	"os"
	"strconv"
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
	case "pane":
		return r.pluginPaneCLI(args[1:])
	case "install":
		if len(args) < 2 {
			return usageErr(r, "usage: gtx plugin install <owner>/<repo>[/subdir...] [--ref REF] [--yes]")
		}
		source, ref, yes := args[1], "", false
		for i := 2; i < len(args); {
			switch args[i] {
			case "--yes":
				yes = true
				i++
			case "--ref":
				v, ok := flagValue(args, &i)
				if !ok {
					return usageErr(r, "--ref requires REF")
				}
				ref = v
			default:
				return usageErr(r, "unsupported install flag: "+args[i])
			}
		}
		if !yes && !term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Fprintln(r.errOut(), "noninteractive plugin install requires --yes")
			return 1
		}
		item, err := store.Install(source, ref, func(preview plugin.InstallPreview) bool {
			writePluginInstallPreview(r.errOut(), preview, source)
			if yes {
				return true
			}
			fmt.Fprint(r.errOut(), "Continue? [y/N] ")
			answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			return strings.EqualFold(strings.TrimSpace(answer), "y") || strings.EqualFold(strings.TrimSpace(answer), "yes")
		})
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		if raw, err := r.request("cli:plugin", "plugin.link", map[string]any{"path": item.ManifestPath, "enabled": true, "source": item.Source}); err == nil {
			var resp api.Response
			if json.Unmarshal(raw, &resp) != nil || resp.Err != nil {
				fmt.Fprintln(r.errOut(), string(raw))
				return 1
			}
		}
		b, _ := json.Marshal(api.Success("cli:plugin", map[string]any{"type": "plugin_linked", "plugin": item}))
		fmt.Fprintln(r.out(), string(b))
		return 0
	case "uninstall":
		if len(args) != 2 {
			return usageErr(r, "usage: gtx plugin uninstall <plugin_id|owner/repo[/subdir...]>")
		}
		id, removed, err := store.Uninstall(args[1])
		if err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
		if removed {
			_, _ = r.request("cli:plugin", "plugin.unlink", map[string]any{"plugin_id": id})
		}
		b, _ := json.Marshal(api.Success("cli:plugin", map[string]any{"type": "plugin_unlinked", "plugin_id": id, "removed": removed}))
		fmt.Fprintln(r.out(), string(b))
		return 0
	case "action":
		if len(args) < 2 {
			return usageErr(r, "usage: gtx plugin action <list|invoke>")
		}
		method = "plugin.action." + args[1]
		if args[1] != "list" && args[1] != "invoke" {
			return usageErr(r, "usage: gtx plugin action <list|invoke>")
		}
		start := 2
		if args[1] == "invoke" {
			if len(args) < 3 {
				return usageErr(r, "usage: gtx plugin action invoke <action_id> [--plugin ID]")
			}
			p["action_id"] = args[2]
			p["context"] = map[string]any{"invocation_source": "cli"}
			start = 3
		}
		for i := start; i < len(args); {
			if args[i] != "--plugin" {
				return usageErr(r, "unsupported action flag: "+args[i])
			}
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, "--plugin requires ID")
			}
			p["plugin_id"] = v
		}
		return r.printResult("cli:plugin", method, p)
	case "log":
		if len(args) < 2 || args[1] != "list" {
			return usageErr(r, "usage: gtx plugin log list [--plugin ID] [--limit N]")
		}
		method = "plugin.log.list"
		for i := 2; i < len(args); {
			flag := args[i]
			v, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, flag+" requires a value")
			}
			switch flag {
			case "--plugin":
				p["plugin_id"] = v
			case "--limit":
				n, err := strconv.ParseUint(v, 10, 64)
				if err != nil {
					return usageErr(r, "invalid limit")
				}
				p["limit"] = n
			default:
				return usageErr(r, "unsupported log flag: "+flag)
			}
		}
		return r.printResult("cli:plugin", method, p)
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
	var resp api.Response
	raw, err := r.request("cli:plugin", method, p)
	if err == nil {
		if err = json.Unmarshal(raw, &resp); err != nil {
			fmt.Fprintln(r.errOut(), err)
			return 1
		}
	} else {
		resp, _ = store.RegistryRequest(&api.Request{ID: "cli:plugin", Method: method, Params: api.MarshalParams(p)})
	}
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
