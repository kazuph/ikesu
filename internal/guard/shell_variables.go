package guard

import (
	"io"
	"sort"
	"strings"

	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
)

// WithShellVariables reads declare -p data, never shell code. A present snapshot
// replaces expansion state so unset/unsupported values cannot reuse stale exports.
func WithShellVariables(i Input, r io.Reader) Input {
	i.shellVariables = map[string]string{}
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(r, "shell variables")
	if err != nil {
		return i
	}
	seen := map[string]bool{}
	for _, stmt := range tree.Stmts {
		decl, ok := stmt.Cmd.(*syntax.DeclClause)
		if !ok || decl.Variant.Value != "declare" || len(decl.Args) != 2 || stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) != 0 {
			i.shellVariables = map[string]string{}
			return i
		}
		flags, assignment := decl.Args[0], decl.Args[1]
		if flags.Value == nil || flags.Name != nil || assignment.Name == nil {
			i.shellVariables = map[string]string{}
			return i
		}
		name := assignment.Name.Value
		duplicate := seen[name]
		seen[name] = true
		delete(i.shellVariables, name)
		if duplicate || authorizationIdentityKey(name) || strings.HasPrefix(name, "_ikesu_guard_") {
			continue
		}
		flag := flags.Value.Lit()
		supported := flag == "--" || strings.HasPrefix(flag, "-") && strings.Trim(flag[1:], "rxi") == ""
		if !supported || assignment.Array != nil || assignment.Index != nil || assignment.Append || assignment.Naked || assignment.Value == nil {
			continue
		}
		literal := true
		syntax.Walk(assignment.Value, func(n syntax.Node) bool {
			switch n.(type) {
			case nil, *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted:
			default:
				literal = false
			}
			return literal
		})
		if !literal {
			continue
		}
		value, err := expand.Literal(&expand.Config{NoUnset: true}, assignment.Value)
		if err == nil {
			i.shellVariables[name] = value
		}
	}
	return i
}

func (i Input) shellExpansionInput() Input {
	if i.shellVariables == nil {
		return i
	}
	i.shellAuthorizationEnv = EnvMap(environment(i.Env))
	env := EnvMap(environment(i.shellVariables))
	for name, value := range i.Env {
		// Snapshot data must never change authorization, startup or approval state.
		if authorizationIdentityKey(name) || strings.HasPrefix(name, "IKESU_") || strings.HasPrefix(name, "AI_GUARD_") {
			env[name] = value
		}
	}
	i.Env = env
	return i
}

// Only diagnostics contain analyzer-expanded bytes. Command displays keep RawLine.
func (i Input) shellDiagnostic(text, line string) string {
	var names []string
	for name, value := range i.shellVariables {
		if value != "" && (strings.Contains(line, "$"+name) || strings.Contains(line, "${"+name)) {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(a, b int) bool {
		if len(i.shellVariables[names[a]]) != len(i.shellVariables[names[b]]) {
			return len(i.shellVariables[names[a]]) > len(i.shellVariables[names[b]])
		}
		return names[a] < names[b]
	})
	var pairs []string
	for _, name := range names {
		pairs = append(pairs, i.shellVariables[name], "$"+name)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
