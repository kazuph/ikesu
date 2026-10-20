package cli

import (
	"encoding/json"
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
)

func (r *runner) pluginPaneCLI(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: ikesu plugin pane <open|focus|close>")
	}
	p := map[string]any{}
	env := map[string]string{}
	switch args[0] {
	case "focus", "close":
		if len(args) != 2 {
			return usageErr(r, "usage: ikesu plugin pane "+args[0]+" <pane_id>")
		}
		p["pane_id"] = args[1]
	case "open":
		for i := 1; i < len(args); {
			flag := args[i]
			if flag == "--focus" || flag == "--no-focus" {
				p["focus"] = flag == "--focus"
				i++
				continue
			}
			value, ok := flagValue(args, &i)
			if !ok {
				return usageErr(r, flag+" requires a value")
			}
			switch flag {
			case "--plugin":
				p["plugin_id"] = value
			case "--entrypoint":
				p["entrypoint"] = value
			case "--placement":
				p["placement"] = value
			case "--direction":
				p["direction"] = value
			case "--workspace":
				p["workspace_id"] = value
			case "--target-pane":
				p["target_pane_id"] = value
			case "--cwd":
				p["cwd"] = value
			case "--env":
				key, val, err := parseEnvAssignment(value)
				if err != nil {
					return usageErr(r, err.Error())
				}
				env[key] = val
			case "--width", "--height":
				var size api.PopupSize
				raw := []byte(value)
				if len(value) > 0 && value[len(value)-1] == '%' {
					raw, _ = json.Marshal(value)
				}
				if err := json.Unmarshal(raw, &size); err != nil {
					return usageErr(r, err.Error())
				}
				p[flag[2:]] = size
			default:
				return usageErr(r, "unsupported pane flag: "+flag)
			}
		}
		p["env"] = env
		if p["plugin_id"] == nil || p["entrypoint"] == nil {
			return usageErr(r, "--plugin and --entrypoint are required")
		}
	default:
		fmt.Fprintln(r.errOut(), "unknown plugin pane command: "+args[0])
		return 2
	}
	return r.printResult("cli:plugin:pane:"+args[0], "plugin.pane."+args[0], p)
}
