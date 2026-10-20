package cli

import (
	"github.com/kazuph/ikesu/internal/api"
	"strconv"
	"strings"
)

func (r *runner) paneReportMetadata(args []string) int {
	if len(args) < 1 {
		return usageErr(r, "usage: ikesu pane report-metadata <pane_id> --source ID [--agent LABEL] [--applies-to-source ID] [--title TEXT|--clear-title] [--display-agent TEXT|--clear-display-agent] [--state-label STATUS=TEXT] [--clear-state-labels] [--token NAME=VALUE] [--clear-token NAME] [--seq N] [--ttl-ms N]")
	}
	p := api.PaneReportMetadataParams{PaneID: args[0], Tokens: map[string]*string{}, StateLabels: map[string]string{}}
	for i := 1; i < len(args); {
		flag := args[i]
		switch flag {
		case "--clear-title":
			p.ClearTitle = true
			i++
			continue
		case "--clear-display-agent":
			p.ClearDisplayAgent = true
			i++
			continue
		case "--clear-state-labels":
			p.ClearStateLabels = true
			i++
			continue
		}
		value, ok := flagValue(args, &i)
		if !ok {
			return usageErr(r, "missing value for "+flag)
		}
		switch flag {
		case "--source":
			p.Source = value
		case "--agent":
			p.Agent = &value
		case "--applies-to-source":
			p.AppliesToSource = &value
		case "--title":
			p.Title = &value
		case "--display-agent":
			p.DisplayAgent = &value
		case "--state-label", "--token":
			k, v, ok := strings.Cut(value, "=")
			if !ok {
				return usageErr(r, flag+" requires NAME=VALUE")
			}
			if flag == "--token" {
				p.Tokens[k] = &v
			} else {
				p.StateLabels[k] = v
			}
		case "--clear-token":
			p.Tokens[value] = nil
		case "--seq", "--ttl-ms":
			n, e := strconv.ParseUint(value, 10, 64)
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
	if p.Title != nil && p.ClearTitle || p.DisplayAgent != nil && p.ClearDisplayAgent {
		return usageErr(r, "value and clear flags conflict")
	}
	return r.sendOK("pane.report_metadata", p)
}
