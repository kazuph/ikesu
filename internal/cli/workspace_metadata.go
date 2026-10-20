package cli

import (
	"github.com/kazuph/ikesu/internal/api"
	"strconv"
	"strings"
)

func (r *runner) workspaceReportMetadata(args []string) int {
	if len(args) < 1 {
		return usageErr(r, "usage: ikesu workspace report-metadata <workspace_id> --source ID [--token NAME=VALUE] [--clear-token NAME] [--seq N] [--ttl-ms N]")
	}
	p := api.WorkspaceReportMetadataParams{WorkspaceID: args[0], Tokens: map[string]*string{}}
	for i := 1; i < len(args); {
		flag := args[i]
		v, ok := flagValue(args, &i)
		if !ok {
			return usageErr(r, "missing value for "+flag)
		}
		switch flag {
		case "--source":
			p.Source = v
		case "--token":
			k, val, ok := strings.Cut(v, "=")
			if !ok {
				return usageErr(r, "--token requires NAME=VALUE")
			}
			p.Tokens[k] = &val
		case "--clear-token":
			p.Tokens[v] = nil
		case "--seq", "--ttl-ms":
			n, e := strconv.ParseUint(v, 10, 64)
			if e != nil {
				return usageErr(r, "invalid number for "+flag)
			}
			if flag == "--seq" {
				p.Seq = &n
			} else {
				p.TTLms = &n
			}
		default:
			return usageErr(r, "unknown option: "+flag)
		}
	}
	if p.Source == "" {
		return usageErr(r, "--source is required")
	}
	return r.sendOK(api.MethodWorkspaceReportMetadata, p)
}
