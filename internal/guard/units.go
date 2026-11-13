package guard

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/kazuph/ikesu/internal/api"
	"mvdan.cc/sh/v3/syntax"
)

// lineJudgement は lineVerdict が命令ごとに出した判定の控え。承認ダイアログで行ごとの判定を出す材料になる。
// allow の命令は記録しない（記録の無い行は allow）。
type lineJudgement struct {
	line        string
	entries     []judgeEntry
	blockOffset int // block で打ち切った命令の位置（blockSet のとき）
	blockSet    bool
}

type judgeEntry struct {
	offset int
	v      Verdict
	target string
	// 行の承認が通す claim（ask の命令自身のものと、スクリプトの中の ask のもの）。
	claim        string
	scriptClaims []string
}

func (j *lineJudgement) add(command Input, v Verdict) {
	if v.Decision == "allow" {
		return
	}
	target := ""
	if command.scriptResult != nil {
		target = command.TriggerCommand
	}
	entry := judgeEntry{offset: command.callOffset, v: v, target: target}
	if v.Decision == "ask" {
		entry.claim, _ = lineClaimHash(command)
		if command.scriptResult != nil {
			entry.scriptClaims = command.scriptResult.claims
		}
	}
	j.entries = append(j.entries, entry)
}

type rawUnit struct {
	op         string
	text       string
	start, end int
	label      string
}

// splitUnits は表示用コマンドを「行」に区切る。最上位の文を順に、各文の && || | |& を左から平らにした葉の文が行になる。
func splitUnits(text string) ([]rawUnit, bool) {
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(strings.NewReader(text), "approval units")
	if err != nil || len(tree.Stmts) == 0 {
		return nil, false
	}
	var units []rawUnit
	var leaf func(stmt *syntax.Stmt, op string)
	leaf = func(stmt *syntax.Stmt, op string) {
		if b, ok := stmt.Cmd.(*syntax.BinaryCmd); ok && !stmt.Negated && !stmt.Background && !stmt.Coprocess && len(stmt.Redirs) == 0 {
			leaf(b.X, op)
			sep := b.Op.String()
			if sep == "|&" {
				sep = "|"
			}
			leaf(b.Y, sep)
			return
		}
		start, end := int(stmt.Pos().Offset()), int(stmt.End().Offset())
		suffix := ""
		for _, r := range stmt.Redirs {
			if (r.Op == syntax.Hdoc || r.Op == syntax.DashHdoc) && r.Hdoc != nil && r.Word != nil {
				if cut := int(r.Word.End().Offset()); cut < end {
					end = cut
				}
				suffix = fmt.Sprintf(" ＋here-doc %d行", strings.Count(strings.TrimRight(r.Hdoc.Lit(), "\n"), "\n")+1)
			}
		}
		if start < 0 || end > len(text) || start >= end {
			return
		}
		body := strings.TrimRight(strings.TrimSpace(text[start:end]), ";")
		units = append(units, rawUnit{op: op, text: strings.TrimSpace(body) + suffix, start: start, end: end, label: unitLabel(stmt)})
	}
	prevEnd := 0
	for n, stmt := range tree.Stmts {
		op := ""
		if n > 0 {
			op = ";"
			if strings.Contains(text[prevEnd:int(stmt.Pos().Offset())], "\n") {
				op = "\n"
			}
		}
		leaf(stmt, op)
		prevEnd = int(stmt.End().Offset())
	}
	if len(units) == 0 {
		return nil, false
	}
	units[0].op = ""
	return units, true
}

// commandLabels は命令名ごとの札。ここに無い命令名は「実行」、確定しない命令名は「不明」。
var commandLabels = map[string]string{
	"cd": "移動", "pushd": "移動", "popd": "移動",
	"cat": "読む", "less": "読む", "head": "読む", "tail": "読む", "grep": "読む", "rg": "読む", "ls": "読む", "find": "読む", "fd": "読む", "jq": "読む",
	"wc": "読む", "sort": "読む", "uniq": "読む", "diff": "読む", "echo": "読む", "printf": "読む", "pwd": "読む", "which": "読む", "stat": "読む", "file": "読む",
	"cp": "書く", "mv": "書く", "mkdir": "書く", "touch": "書く", "tee": "書く", "ln": "書く", "chmod": "書く", "chown": "書く",
	"rm": "消す", "rmdir": "消す", "trash": "消す", "rimraf": "消す", "unlink": "消す",
	"gh": "外へ送る", "wrangler": "外へ送る", "aws": "外へ送る", "gcloud": "外へ送る",
	"ssh": "別マシン", "mosh": "別マシン",
}

var gitLabels = map[string]string{
	"status": "読む", "log": "読む", "diff": "読む", "show": "読む", "branch": "読む", "rev-parse": "読む", "fetch": "読む",
	"add": "書く", "commit": "書く", "checkout": "書く", "switch": "書く", "stash": "書く",
	"clean": "消す", "push": "外へ送る",
}

var labelPrefixes = map[string]bool{"env": true, "sudo": true, "doas": true, "nice": true, "time": true, "command": true, "nohup": true, "builtin": true, "exec": true}

func unitLabel(stmt *syntax.Stmt) string {
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok {
		return "実行"
	}
	writes := false
	for _, r := range stmt.Redirs {
		switch r.Op {
		case syntax.RdrOut, syntax.AppOut, syntax.RdrAll, syntax.AppAll, syntax.ClbOut:
			if t, ok := literalWord(r.Word); !ok || t != "/dev/null" {
				writes = true
			}
		}
	}
	if len(call.Args) == 0 {
		return "書く"
	}
	words := make([]string, len(call.Args))
	known := make([]bool, len(call.Args))
	for n, w := range call.Args {
		words[n], known[n] = literalWord(w)
	}
	for len(words) > 0 && known[0] && labelPrefixes[filepath.Base(words[0])] {
		prefix := filepath.Base(words[0])
		words, known = words[1:], known[1:]
		for len(words) > 0 && known[0] {
			w := words[0]
			if prefix == "env" && strings.Contains(w, "=") || strings.HasPrefix(w, "-") || (prefix == "nice" && len(words) > 1 && words[0] == "-n") {
				if prefix == "nice" && w == "-n" && len(words) > 1 {
					words, known = words[1:], known[1:]
				}
				words, known = words[1:], known[1:]
				continue
			}
			break
		}
	}
	if len(words) == 0 {
		return "実行"
	}
	if !known[0] {
		return "不明"
	}
	label := commandLabel(filepath.Base(words[0]), words[1:])
	if writes && label == "読む" {
		return "書く"
	}
	return label
}

func commandLabel(name string, args []string) string {
	anyArg := func(f func(string) bool) bool {
		for _, a := range args {
			if f(a) {
				return true
			}
		}
		return false
	}
	switch name {
	case "git":
		sub, hard := "", false
		for n := 0; n < len(args); n++ {
			if args[n] == "-C" || args[n] == "-c" {
				n++
				continue
			}
			if !strings.HasPrefix(args[n], "-") {
				sub = args[n]
				for _, a := range args[n+1:] {
					hard = hard || a == "--hard"
				}
				break
			}
		}
		if sub == "reset" && hard {
			return "消す"
		}
		rest := []string{}
		for n, a := range args {
			if a == sub {
				rest = args[n+1:]
				break
			}
		}
		switch sub {
		case "branch":
			for _, a := range rest {
				if a == "-D" || a == "-d" || a == "--delete" {
					return "消す"
				}
			}
		case "stash":
			if len(rest) > 0 && (rest[0] == "drop" || rest[0] == "clear") {
				return "消す"
			}
		}
		if l, ok := gitLabels[sub]; ok {
			return l
		}
		return "実行"
	case "find":
		for n, a := range args {
			if a == "-delete" {
				return "消す"
			}
			if (a == "-exec" || a == "-execdir" || a == "-ok" || a == "-okdir") && n+1 < len(args) {
				switch filepath.Base(args[n+1]) {
				case "rm", "rmdir", "unlink", "trash":
					return "消す"
				}
			}
		}
		return "読む"
	case "gh":
		return ghLabel(args)
	case "sed":
		if anyArg(func(a string) bool {
			return a == "--in-place" || strings.HasPrefix(a, "--in-place=") || (strings.HasPrefix(a, "-") && !strings.HasPrefix(a, "--") && strings.Contains(a, "i"))
		}) {
			return "書く"
		}
		return "実行"
	case "curl":
		for n, a := range args {
			method := ""
			switch {
			case (a == "-X" || a == "--request") && n+1 < len(args):
				method = args[n+1]
			case strings.HasPrefix(a, "--request="):
				method = strings.TrimPrefix(a, "--request=")
			case strings.HasPrefix(a, "-X") && len(a) > 2:
				method = a[2:]
			}
			if method != "" && !strings.EqualFold(method, "GET") && !strings.EqualFold(method, "HEAD") {
				return "外へ送る"
			}
			if a == "-d" || a == "-F" || a == "-T" || a == "--json" || strings.HasPrefix(a, "--data") || strings.HasPrefix(a, "--form") || strings.HasPrefix(a, "--upload-file") || strings.HasPrefix(a, "--json=") {
				return "外へ送る"
			}
		}
		return "実行"
	case "wget":
		if anyArg(func(a string) bool { return strings.HasPrefix(a, "--post-") || a == "--method=POST" }) {
			return "外へ送る"
		}
		return "実行"
	case "npm":
		if len(args) > 0 && args[0] == "publish" {
			return "外へ送る"
		}
		return "実行"
	case "docker":
		if len(args) > 0 && args[0] == "push" {
			return "外へ送る"
		}
		return "実行"
	case "scp", "rsync":
		if anyArg(func(a string) bool {
			host, _, found := strings.Cut(a, ":")
			return found && !strings.HasPrefix(a, "-") && host != "" && !strings.Contains(host, "/")
		}) {
			return "別マシン"
		}
		return "実行"
	}
	if l, ok := commandLabels[name]; ok {
		return l
	}
	return "実行"
}

// ghLabel は読むだけの gh 下位コマンドを「読む」、それ以外を「外へ送る」にする。
func ghLabel(args []string) string {
	words := []string{}
	for _, a := range args {
		if !strings.HasPrefix(a, "-") {
			words = append(words, a)
		}
	}
	if len(words) == 0 {
		return "外へ送る"
	}
	pick := func(m ...string) bool {
		if len(words) < 2 {
			return false
		}
		for _, x := range m {
			if words[1] == x {
				return true
			}
		}
		return false
	}
	switch words[0] {
	case "pr":
		if pick("list", "view", "diff", "checks", "status") {
			return "読む"
		}
	case "issue":
		if pick("list", "view", "status") {
			return "読む"
		}
	case "run":
		if pick("list", "view", "watch") {
			return "読む"
		}
	case "repo":
		if pick("view") {
			return "読む"
		}
	case "release":
		if pick("list", "view") {
			return "読む"
		}
	case "search":
		return "読む"
	case "auth":
		if pick("status") {
			return "読む"
		}
	case "api":
		for n, a := range args {
			switch {
			case (a == "-X" || a == "--method") && n+1 < len(args):
				if !strings.EqualFold(args[n+1], "GET") {
					return "外へ送る"
				}
			case strings.HasPrefix(a, "--method="):
				if !strings.EqualFold(strings.TrimPrefix(a, "--method="), "GET") {
					return "外へ送る"
				}
			case strings.HasPrefix(a, "-X") && len(a) > 2:
				if !strings.EqualFold(a[2:], "GET") {
					return "外へ送る"
				}
			case a == "-f" || a == "-F" || a == "--input" || a == "--field" || a == "--raw-field" || strings.HasPrefix(a, "--input=") || strings.HasPrefix(a, "--field=") || strings.HasPrefix(a, "--raw-field="):
				return "外へ送る"
			}
		}
		return "読む"
	}
	return "外へ送る"
}

func literalWord(w *syntax.Word) (string, bool) {
	if w == nil {
		return "", false
	}
	var b strings.Builder
	for _, part := range w.Parts {
		switch p := part.(type) {
		case *syntax.Lit:
			b.WriteString(p.Value)
		case *syntax.SglQuoted:
			b.WriteString(p.Value)
		case *syntax.DblQuoted:
			for _, q := range p.Parts {
				lit, ok := q.(*syntax.Lit)
				if !ok {
					return "", false
				}
				b.WriteString(lit.Value)
			}
		default:
			return "", false
		}
	}
	return b.String(), true
}

func verdictRank(decision string) int {
	switch decision {
	case "block", "deny":
		return 3
	case "ask":
		return 2
	}
	return 1
}

// commandUnits は表示用コマンド（command）を行に区切り、行ごとの判定を付ける。
// 伏せ字前と伏せ字後で区切りが一致しない、または判定の材料が無い・位置が合わないときは nil（今の表示に戻す）。
func (i Input) commandUnits(command string) []api.CommandUnit {
	j := i.judgement
	if j == nil || command == "" {
		return nil
	}
	start := strings.Index(j.line, command)
	if start < 0 {
		return nil
	}
	raw, ok := splitUnits(command)
	if !ok {
		return nil
	}
	safe, ok := splitUnits(i.redact(command))
	if !ok || len(safe) != len(raw) {
		return nil
	}
	for n := range raw {
		if raw[n].op != safe[n].op {
			return nil
		}
	}
	unitAt := func(offset int) int {
		rel, found := offset-start, 0
		for n, u := range raw {
			if u.start <= rel {
				found = n
			}
		}
		return found
	}
	units := make([]api.CommandUnit, len(raw))
	for n := range raw {
		units[n] = api.CommandUnit{Op: raw[n].op, Text: safe[n].text, Label: raw[n].label, Verdict: "allow"}
	}
	rank := make([]int, len(raw))
	for n := range rank {
		rank[n] = 1
	}
	for _, e := range j.entries {
		n := unitAt(e.offset)
		if r := verdictRank(e.v.Decision); r > rank[n] {
			rank[n] = r
			verdict := e.v.Decision
			if verdict == "deny" {
				verdict = "block"
			}
			units[n].Verdict, units[n].Rule, units[n].Reason, units[n].Target = verdict, e.v.Rule, i.redact(e.v.Reason), i.redact(e.target)
		}
	}
	if j.blockSet {
		for n := unitAt(j.blockOffset) + 1; n < len(units); n++ {
			units[n].Verdict = "unjudged"
		}
	}
	return units
}
