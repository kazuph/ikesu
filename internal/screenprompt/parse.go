// Package screenprompt reads the public terminal forms documented in SPEC 工程6.
// The examples were observed in isolated real Claude Code and Codex sessions.
package screenprompt

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/kazuph/ikesu/internal/api"
)

type Prompt struct {
	MCPServer     string
	CodexQuestion bool
	TextOpen      bool
	Final         bool
	Flow          string
	Question      int
	Total         int
	Params        api.DecisionCreateParams
	Keys          map[string]string
	Checked       map[string]bool
	Items         []string // arrow-navigation order, including the free input and progress row
	Cursor        int
	TextID        string
	Progress      int
	HasPreview    bool
	Preview       *string
}

var numbered = regexp.MustCompile(`^\s*([❯›>]?)\s*(\d+)[.)]\s+(.*)$`)
var checkbox = regexp.MustCompile(`^\[([ ✔✓xX])\]\s*(.*)$`)
var shortcut = regexp.MustCompile(`\((esc|[[:alnum:]])\)`)
var tabs = regexp.MustCompile(`([☐☒☑✔])\s*([^☐☒☑✔←→]+)`)
var toolCall = regexp.MustCompile(`^⏺\s*[[:alnum:]_]+\(`)
var glued = regexp.MustCompile(`\S( {2,})\S`)

// A damaged partial read still has option rows; an idle/empty screen is not a prompt.
func HasOptionRows(lines []string) bool {
	for _, line := range lines {
		if numbered.MatchString(line) {
			return true
		}
	}
	return parseMCP(lines) != nil
}

// Full-width highlight padding can join two painted rows in the logical screen.
func unglue(lines []string) []string {
	var out []string
	for _, line := range lines {
		if at := strings.Index(line, "──"); at >= 0 && solidRule(line[at:]) && numbered.MatchString(line[:at]) {
			out = append(out, line[:at], line[at:])
			continue
		}
		if !numbered.MatchString(line) {
			out = append(out, line)
			continue
		}
		for {
			m := glued.FindStringSubmatchIndex(line)
			if m == nil {
				break
			}
			at, end := m[2], m[3]
			out = append(out, line[:at])
			line = line[end:]
			// Restore indentation so a continuation remains a description.
			if !numbered.MatchString(line) {
				line = "  " + line
			}
		}
		out = append(out, line)
	}
	return out
}

func Parse(agent string, screen []string) *Prompt {
	return ParseWithTextID(agent, screen, "")
}

// During input delivery a previously observed free row can display entered
// text instead of its placeholder; that row retains its original identity.
func ParseWithTextID(agent string, screen []string, textID string) *Prompt {
	if agent != "claude" && agent != "codex" && agent != "devin" {
		return nil
	}
	if agent == "claude" {
		if p := parseMCP(screen); p != nil {
			return p
		}
	}
	codexLabel, codexBody := "", ""
	if agent == "codex" {
		screen, codexLabel, codexBody = codexQuestionScreen(screen)
	}
	screen = currentQuestionLines(screen)
	screen, notice := cleanNotice(screen)
	columns := screenColumns(screen)
	plain, preview, hasPreview := extractPreview(screen)
	lines := unglue(mergeCommandRows(plain, columns))
	positional, chatID := positionalTextIDs(lines)
	if textID == "" {
		textID = positional
	}
	first := -1
	for i, line := range lines {
		if m := numbered.FindStringSubmatch(line); m != nil && (first < 0 || m[2] == "1") {
			first = i
		}
	}
	if first < 0 {
		return nil
	}
	start := 0
	for i := 0; i < first; i++ {
		if solidRule(lines[i]) {
			start = i + 1
		}
	}
	p := &Prompt{Keys: map[string]string{}, Checked: map[string]bool{}, Cursor: -1, Progress: -1, HasPreview: hasPreview, Preview: preview}
	p.Params.Kind = "agent_prompt"
	askUserQuestion := codexLabel != ""
	if codexLabel != "" {
		p.CodexQuestion = true
		p.TextOpen = codexNotesOpen(screen)
		p.Params.QuestionLabel = &codexLabel
		if codexBody != "" {
			p.Params.Body = &codexBody
		}
	}
	var title []string
	for _, line := range lines[start:first] {
		physical := line
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, "←") && strings.Contains(line, "Submit") {
			askUserQuestion = true
			p.Params.QuestionLabel = questionLabel(line)
			p.Params.Body = p.Params.QuestionLabel
			p.Flow, p.Question, p.Total = flowTabs(line)
			continue
		}
		if strings.HasPrefix(line, "☐ ") || strings.HasPrefix(line, "☒ ") {
			askUserQuestion = true
			continue
		}
		if strings.HasPrefix(line, "⏺") {
			continue
		}
		title = append(title, physical)
	}
	p.Params.Title = joinWrapped(title, columns)
	if p.Params.Title == "" {
		return nil
	}
	lowerTitle := strings.ToLower(p.Params.Title)
	for _, word := range []string{"proceed", "permission", "trust", "run the following", "run this command", "allow", "確認", "承認", "許可"} {
		if !askUserQuestion && strings.Contains(lowerTitle, word) {
			p.Params.Kind = "tool_permission"
		}
	}
	if notice != "" {
		body := notice
		if p.Params.Body != nil {
			body = *p.Params.Body + "\n" + body
		}
		p.Params.Body = &body
	}
	lastOption := -1
	previousDescription := ""
	seen := map[string]bool{}
	for _, line := range lines[first:] {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "Notes: press") {
			lastOption = -1
			continue
		}
		m := numbered.FindStringSubmatch(line)
		if m != nil {
			id, label := m[2], strings.TrimSpace(m[3])
			if seen[id] {
				return nil
			}
			seen[id] = true
			checked := checkbox.FindStringSubmatch(label)
			if checked != nil {
				p.Params.MultiSelect = true
				p.Checked[id] = checked[1] != " "
				label = checked[2]
			}
			free := p.CodexQuestion && strings.HasPrefix(strings.ToLower(label), "none of the above") || id == textID || id == chatID || strings.HasPrefix(strings.ToLower(label), "type something") || strings.HasPrefix(strings.ToLower(label), "chat about this")
			lastOption = -1
			if free {
				p.Params.AllowText = true
				askUserQuestion = true
				if p.TextID == "" {
					p.TextID = id
					p.Items = append(p.Items, id)
				}
			} else {
				role := "other"
				if strings.EqualFold(label, "Cancel") {
					role = "reject"
				}
				if p.Params.Kind == "tool_permission" {
					l := strings.ToLower(label)
					if strings.HasPrefix(l, "yes") || strings.HasPrefix(l, "allow") || strings.HasPrefix(l, "accept") {
						role = "approve"
					}
					if strings.HasPrefix(l, "no") || strings.HasPrefix(l, "deny") || strings.HasPrefix(l, "reject") {
						role = "reject"
					}
				}
				p.Params.Options = append(p.Params.Options, api.DecisionOption{ID: id, Label: label, Role: role})
				lastOption = len(p.Params.Options) - 1
				p.Items = append(p.Items, id)
			}
			if m[1] != "" {
				p.Cursor = len(p.Items) - 1
			}
			continue
		}
		progress := strings.TrimSpace(strings.TrimLeft(trim, "❯›>"))
		if progress == "Next" || progress == "Submit" {
			p.Progress = len(p.Items)
			p.Items = append(p.Items, "progress")
			lastOption = -1
			if strings.HasPrefix(trim, "❯") || strings.HasPrefix(trim, "›") || strings.HasPrefix(trim, ">") {
				p.Cursor = p.Progress
			}
			continue
		}
		if strings.Contains(strings.ToLower(trim), "enter to") || strings.Contains(strings.ToLower(trim), "esc to") {
			break
		}
		if trim == "" || solidRule(trim) || strings.HasPrefix(trim, "╌") {
			lastOption = -1
			continue
		}
		if lastOption >= 0 && len(line) > 0 && unicode.IsSpace([]rune(line)[0]) {
			o := &p.Params.Options[lastOption]
			value := trim
			if o.Description != nil {
				value = *o.Description + wrappedSeparator(previousDescription, line, columns) + trim
			}
			o.Description = &value
			previousDescription = line
		}
	}
	if len(p.Params.Options) == 0 || p.CodexQuestion && (p.TextID == "" || !codexQuestionComplete(screen)) {
		return nil
	}
	if askUserQuestion {
		p.Params.Kind = "agent_prompt"
	} else if p.Params.Kind == "tool_permission" {
		for i := first - 1; i >= 0; i-- {
			if strings.HasPrefix(strings.TrimSpace(lines[i]), "⏺") {
				if toolCall.MatchString(strings.TrimSpace(lines[i])) {
					p.Params.Title = strings.TrimSpace(lines[i]) + " " + p.Params.Title
				}
				break
			}
		}
	}
	p.Final = finalPrompt(p)
	for _, o := range p.Params.Options {
		text := o.Label
		if o.Description != nil {
			text += " " + *o.Description
		}
		key := o.ID
		if m := shortcut.FindStringSubmatch(strings.ToLower(text)); m != nil {
			key = m[1]
			if key == "esc" {
				key = "\x1b"
			}
		}
		p.Keys[o.ID] = key
	}
	if p.HasPreview && p.Cursor >= 0 && p.Cursor < len(p.Items) {
		for i := range p.Params.Options {
			o := &p.Params.Options[i]
			if o.ID == p.Items[p.Cursor] {
				o.Preview = p.Preview
			} else {
				o.PreviewUnavailable = true
			}
		}
	}
	return p
}

func solidRule(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "─") && strings.Trim(s, "─") == ""
}

func questionLabel(line string) *string {
	matches := tabs.FindAllStringSubmatch(line, -1)
	var names []string
	current := -1
	for _, m := range matches {
		name := strings.TrimSpace(m[2])
		if name == "Submit" {
			continue
		}
		if current < 0 && m[1] == "☐" {
			current = len(names)
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	label := "最終確認"
	if current >= 0 {
		label = "質問 " + strconv.Itoa(current+1) + "/" + strconv.Itoa(len(names)) + " " + names[current]
	}
	return &label
}

func (p *Prompt) Signature() string {
	type option struct {
		ID, Label   string
		Description *string
	}
	options := []option{}
	for _, o := range p.Params.Options {
		var description *string
		if o.Description != nil {
			value := stableText(*o.Description)
			description = &value
		}
		options = append(options, option{o.ID, stableText(o.Label), description})
	}
	var label *string
	if p.Params.QuestionLabel != nil && codexQuestionHeader.MatchString(*p.Params.QuestionLabel) {
		label = p.Params.QuestionLabel
	}
	data, _ := json.Marshal(struct {
		MCPServer              string  `json:",omitempty"`
		QuestionLabel          *string `json:",omitempty"`
		Title                  string
		Options                []option
		AllowText, MultiSelect bool
	}{p.MCPServer, label, stableText(p.Params.Title), options, p.Params.AllowText, p.Params.MultiSelect})
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:])
}

func FromDecision(d api.Decision) string {
	server := ""
	if d.Origin != nil {
		_, server, _ = strings.Cut(d.Origin.Agent, mcpOriginSeparator)
		server = strings.TrimSuffix(server, "”")
	}
	return (&Prompt{MCPServer: server, Params: api.DecisionCreateParams{Title: d.Title, Options: d.Options, AllowText: d.AllowText, MultiSelect: d.MultiSelect, QuestionLabel: d.QuestionLabel}}).Signature()
}
