package screenprompt

import (
	"github.com/kazuph/ikesu/internal/api"
	"regexp"
	"strings"
)

var mcpHeader = regexp.MustCompile(`^\s*MCP server ["“]([^"”]+)["”] requests your input\s*$`)
var mcpButtons = regexp.MustCompile(`^[ \t]*([❯›>]?[ \t]*)Accept[ \t]+([❯›>]?[ \t]*)Decline[ \t]*$`)

const mcpOriginSeparator = " / MCP: "

func (p *Prompt) OriginAgent(agent string) string {
	if p.MCPServer != "" {
		return agent + mcpOriginSeparator + p.MCPServer
	}
	return agent
}

func parseMCP(screen []string) *Prompt {
	header, footer, buttons := -1, -1, -1
	server := ""
	cursor := -1
	for i, line := range screen {
		if match := mcpHeader.FindStringSubmatch(line); match != nil {
			header = i
			server = match[1]
		}
	}
	if header < 0 {
		return nil
	}
	for i := header + 1; i < len(screen); i++ {
		line := screen[i]
		if numbered.MatchString(line) {
			return nil
		}
		if match := mcpButtons.FindStringSubmatch(line); match != nil {
			if buttons >= 0 {
				return nil
			}
			buttons = i
			left, right := strings.TrimSpace(match[1]) != "", strings.TrimSpace(match[2]) != ""
			if left == right {
				return nil
			}
			if left {
				cursor = 0
			} else {
				cursor = 1
			}
		}
		if navigationFooter(line) {
			footer = i
		}
	}
	if header < 0 || buttons < header || footer < buttons || cursor < 0 {
		return nil
	}
	for _, line := range screen[footer+1:] {
		if strings.TrimSpace(line) != "" {
			return nil
		}
	}
	var question []string
	for _, line := range screen[header+1 : buttons] {
		trim := strings.TrimSpace(line)
		if trim == "" || solidRule(trim) {
			continue
		}
		if strings.ContainsAny(trim, "☐☑☒") || strings.HasPrefix(trim, "[") || strings.Contains(trim, "Type something") {
			return nil
		}
		question = append(question, line)
	}
	title := joinWrapped(question, screenColumns(screen))
	if title == "" {
		return nil
	}
	return &Prompt{MCPServer: server, Cursor: cursor, Progress: -1, Items: []string{"accept", "decline"}, Keys: map[string]string{}, Checked: map[string]bool{}, Params: api.DecisionCreateParams{Kind: "tool_permission", Title: title, Options: []api.DecisionOption{
		{ID: "accept", Label: "Accept", Role: "approve"}, {ID: "decline", Label: "Decline", Role: "reject"},
	}}}
}
