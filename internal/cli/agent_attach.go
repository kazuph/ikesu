package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/client"
)

func (r *runner) agentAttachCLI(args []string) int {
	if len(args) == 0 {
		return usageErr(r, "usage: ikesu agent attach <target> [--takeover]")
	}
	target := args[0]
	takeover := false
	for _, flag := range args[1:] {
		if flag != "--takeover" {
			return usageErr(r, "unknown option: "+flag)
		}
		takeover = true
	}
	raw, err := r.request("cli:agent:attach:resolve", api.MethodAgentGet, api.AgentTarget{Target: target})
	if err != nil {
		raw = r.serverNotRunning("cli:agent:attach:resolve")
	}
	var response api.Response
	if err := json.Unmarshal(raw, &response); err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	if response.Err != nil {
		fmt.Fprintln(r.errOut(), string(raw))
		return 1
	}
	var info api.AgentInfoResult
	if err := json.Unmarshal(response.Result, &info); err != nil {
		fmt.Fprintln(r.errOut(), err)
		return 1
	}
	code, err := client.AttachAgent(api.ClientSocketPath(filepath.Dir(r.socketPath())), info.Agent.PaneID, takeover, os.Stdin, os.Stdout)
	if err != nil {
		fmt.Fprintln(r.errOut(), "ikesu:", err)
	}
	return code
}
