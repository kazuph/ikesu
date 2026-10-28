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
	i.runtimeLine = true
	i.shellVariables = map[string]string{}
	i.shellArrays = map[string]map[int]string{}
	tree, err := syntax.NewParser(syntax.Variant(syntax.LangBash)).Parse(r, "shell variables")
	if err != nil {
		return i
	}
	seen := map[string]bool{}
	for _, stmt := range tree.Stmts {
		decl, ok := stmt.Cmd.(*syntax.DeclClause)
		if !ok || decl.Variant.Value != "declare" || len(decl.Args) != 2 || stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) != 0 {
			i.shellVariables = map[string]string{}
			i.shellArrays = map[string]map[int]string{}
			return i
		}
		flags, assignment := decl.Args[0], decl.Args[1]
		if flags.Value == nil || flags.Name != nil || assignment.Name == nil {
			i.shellVariables = map[string]string{}
			i.shellArrays = map[string]map[int]string{}
			return i
		}
		name := assignment.Name.Value
		duplicate := seen[name]
		seen[name] = true
		delete(i.shellVariables, name)
		delete(i.shellArrays, name)
		if duplicate || snapshotReservedName(name) {
			continue
		}
		flag := flags.Value.Lit()
		if strings.Contains(flag, "a") && strings.Trim(strings.TrimPrefix(flag, "-"), "arx") == "" && !assignment.Append && !assignment.Naked {
			if values, ok := snapshotArray(assignment); ok {
				i.shellArrays[name] = values
				if value, exists := values[0]; exists {
					i.shellVariables[name] = value
				}
			}
			continue
		}
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

func snapshotReservedName(name string) bool {
	return authorizationIdentityKey(name) || strings.HasPrefix(name, "IKESU_") || strings.HasPrefix(name, "AI_GUARD_") || strings.HasPrefix(name, "_ikesu_guard_")
}

// Env remains the exported caller environment. Only the expander sees snapshots.
func (i Input) shellExpansionValues() map[string]string {
	if i.shellVariables == nil {
		return EnvMap(environment(i.Env))
	}
	values := map[string]string{}
	for name, value := range i.shellVariables {
		if !snapshotReservedName(name) {
			values[name] = value
		}
	}
	for name, value := range i.Env {
		if snapshotReservedName(name) {
			values[name] = value
		}
	}
	return values
}

// Compare with expansion using only exported values. A preparatory lookup of
// IFS by the parser must not label a literal argument as private.
func (i Input) privateExpansion(word *syntax.Word, value string) bool {
	if i.shellVariables == nil {
		return false
	}
	exported, err := shellLiteral(&expand.Config{Env: expand.ListEnviron(environment(i.Env)...), NoUnset: true}, word)
	return err != nil || exported != value
}

// Only diagnostics contain analyzer-expanded bytes. Command displays keep RawLine.
func (i Input) shellDiagnostic(text, line string) string {
	values := i.privateShellValues()
	var names []string
	for name, value := range i.privateShellValues() {
		if value != "" && (strings.Contains(line, "$"+strings.Split(name, "[")[0]) || strings.Contains(line, "${"+strings.Split(name, "[")[0])) {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(a, b int) bool {
		if len(values[names[a]]) != len(values[names[b]]) {
			return len(values[names[a]]) > len(values[names[b]])
		}
		return names[a] < names[b]
	})
	var pairs []string
	for _, name := range names {
		pairs = append(pairs, values[name], "$"+name)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
