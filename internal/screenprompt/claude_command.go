package screenprompt

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
)

var claudeCommandHeader = regexp.MustCompile(`^([[:alnum:]_]+) command$`)
var claudeDeadline = regexp.MustCompile(`(?i)automatically\s+(?:deny|block)\s+this\s+request\s+in\s+(\d+):(\d{2})`)

// Only expose structured commands when both frame boundaries are visible.
func claudeCommand(screen []string) (*api.DecisionOrigin, *uint64) {
	header := -1
	for i, line := range screen {
		if claudeCommandHeader.MatchString(strings.TrimSpace(line)) {
			header = i
		}
	}
	if header < 0 {
		return nil, nil
	}
	var rules []int
	for i := header + 1; i < len(screen); i++ {
		t := strings.TrimSpace(screen[i])
		if strings.HasPrefix(t, "╌╌") && strings.Trim(t, "╌") == "" {
			rules = append(rules, i)
		}
		if len(rules) == 2 {
			break
		}
	}
	if len(rules) != 2 || rules[1] <= rules[0]+1 {
		return nil, nil
	}
	var reasons []string
	for i := header + 1; i < rules[0]; i++ {
		t := strings.TrimSpace(screen[i])
		if strings.HasPrefix(t, "Tip:") {
			for i+1 < rules[0] && (strings.HasPrefix(strings.TrimSpace(screen[i+1]), "choose ") || strings.TrimSpace(screen[i+1]) == "below") {
				i++
			}
			continue
		}
		if t != "" {
			reasons = append(reasons, screen[i])
		}
	}
	if len(reasons) == 0 {
		return nil, nil
	}
	var command []string
	for _, row := range screen[rules[0]+1 : rules[1]] {
		t := strings.TrimSpace(row)
		t = strings.TrimSpace(strings.TrimPrefix(t, "│"))
		if t != "" {
			command = append(command, t)
		}
	}
	if len(command) == 0 {
		return nil, nil
	}
	var warnings []string
	for _, row := range screen[rules[1]+1:] {
		t := strings.TrimSpace(row)
		if strings.HasPrefix(t, "│") {
			warnings = append(warnings, strings.TrimSpace(strings.TrimPrefix(t, "│")))
		}
		if numbered.MatchString(row) {
			break
		}
	}
	o := &api.DecisionOrigin{Tool: strings.TrimSpace(screen[header]), Explanation: joinWrapped(reasons, screenColumns(screen)), Command: joinWrapped(command, screenColumns(screen)), Warning: joinWrapped(warnings, screenColumns(screen))}
	var timeout *uint64
	joined := strings.Join(strings.Fields(strings.Join(screen, " ")), " ")
	if m := claudeDeadline.FindStringSubmatch(joined); m != nil {
		min, e1 := strconv.ParseUint(m[1], 10, 32)
		sec, e2 := strconv.ParseUint(m[2], 10, 8)
		if e1 == nil && e2 == nil && sec < 60 {
			ms := (min*60 + sec) * 1000
			timeout = &ms
		}
	}
	return o, timeout
}
