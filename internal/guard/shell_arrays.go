package guard

import (
	"fmt"
	"mvdan.cc/sh/v3/expand"
	"mvdan.cc/sh/v3/syntax"
	"strconv"
	"strings"
)

func snapshotArray(assignment *syntax.Assign) (map[int]string, bool) {
	array := assignment.Array
	if array == nil && assignment.Value != nil {
		value, err := snapshotLiteral(assignment.Value)
		if err != nil {
			return nil, false
		}
		// Bash 3.2 quotes the array representation; parse it as data, never eval.
		tree, err := syntax.NewParser().Parse(strings.NewReader("A="+value), "")
		if err != nil || len(tree.Stmts) != 1 {
			return nil, false
		}
		statement := tree.Stmts[0]
		call, ok := statement.Cmd.(*syntax.CallExpr)
		if !ok || len(call.Args) != 0 || len(call.Assigns) != 1 || len(statement.Redirs) != 0 || statement.Background || statement.Negated {
			return nil, false
		}
		array = call.Assigns[0].Array
	}
	if array == nil {
		return nil, false
	}
	values := map[int]string{}
	next := 0
	for _, element := range array.Elems {
		index := next
		if element.Index != nil {
			word, ok := element.Index.(*syntax.Word)
			if !ok {
				return nil, false
			}
			var err error
			index, err = strconv.Atoi(word.Lit())
			if err != nil || index < 0 {
				return nil, false
			}
		}
		if element.Value == nil {
			return nil, false
		}
		value, err := snapshotLiteral(element.Value)
		if err != nil {
			return nil, false
		}
		if _, duplicate := values[index]; duplicate {
			return nil, false
		}
		values[index] = value
		next = index + 1
	}
	return values, true
}

func snapshotLiteral(word *syntax.Word) (string, error) {
	if word == nil {
		return "", fmt.Errorf("snapshot contains no word")
	}
	literal := true
	syntax.Walk(word, func(n syntax.Node) bool {
		switch n.(type) {
		case nil, *syntax.Word, *syntax.Lit, *syntax.SglQuoted, *syntax.DblQuoted:
		default:
			literal = false
		}
		return literal
	})
	if !literal {
		return "", fmt.Errorf("snapshot contains nonliteral data")
	}
	return shellLiteral(&expand.Config{NoUnset: true}, word)
}

// Sparse indexed arrays remain maps, avoiding allocation based on an untrusted
// maximum index. Substitute a selected element as an AST literal, never code.
func (i Input) runtimeLiteral(cfg *expand.Config, word *syntax.Word) (string, error) {
	if !i.runtimeLine || len(i.shellArrays) == 0 {
		return shellLiteral(cfg, word)
	}
	var parts func([]syntax.WordPart) ([]syntax.WordPart, error)
	parts = func(original []syntax.WordPart) ([]syntax.WordPart, error) {
		result := make([]syntax.WordPart, 0, len(original))
		for _, part := range original {
			switch n := part.(type) {
			case *syntax.DblQuoted:
				copy := *n
				var err error
				copy.Parts, err = parts(n.Parts)
				if err != nil {
					return nil, err
				}
				part = &copy
			case *syntax.ParamExp:
				if values, exists := i.shellArrays[n.Param.Value]; exists && n.Index != nil {
					if n.Excl || n.Length || n.Slice != nil || n.Repl != nil || n.Exp != nil || n.Names != 0 {
						return nil, fmt.Errorf("配列要素の展開形式を確定できません")
					}
					if !numericArrayIndex(cfg, n.Index) {
						return nil, fmt.Errorf("配列添字の値を確定できません")
					}
					index, err := expand.Arithm(cfg, n.Index)
					if err != nil {
						return nil, fmt.Errorf("配列添字を確定できません")
					}
					if index < 0 {
						last := -1
						for key := range values {
							if key > last {
								last = key
							}
						}
						index = last + 1 + index
					}
					value, exists := values[index]
					if !exists {
						return nil, fmt.Errorf("配列要素が未設定です")
					}
					part = &syntax.Lit{Value: value}
				}
			}
			result = append(result, part)
		}
		return result, nil
	}
	copy := *word
	var err error
	copy.Parts, err = parts(word.Parts)
	if err != nil {
		return "", err
	}
	return shellLiteral(cfg, &copy)
}

func (i Input) privateShellValues() map[string]string {
	values := EnvMap(environment(i.shellVariables))
	for name, array := range i.shellArrays {
		for index, value := range array {
			values[fmt.Sprintf("%s[%d]", name, index)] = value
		}
	}
	return values
}

func numericArrayIndex(cfg *expand.Config, expr syntax.ArithmExpr) bool {
	valid := true
	syntax.Walk(expr, func(node syntax.Node) bool {
		if word, ok := node.(*syntax.Word); ok {
			value, err := shellLiteral(cfg, word)
			if err != nil {
				valid = false
				return false
			}
			seen := map[string]bool{}
			for syntax.ValidName(value) {
				if seen[value] {
					valid = false
					return false
				}
				seen[value] = true
				variable := cfg.Env.Get(value)
				if !variable.IsSet() {
					valid = false
					return false
				}
				value = variable.String()
			}
			if _, err := strconv.ParseInt(value, 0, 64); err != nil {
				if _, err = strconv.ParseInt(value, 10, 64); err != nil {
					valid = false
				}
			}
			return false
		}
		return valid
	})
	return valid
}
