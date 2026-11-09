package guard

import (
	"os"
	"path/filepath"
	"strings"

	"mvdan.cc/sh/v3/syntax"
)

// literalCdChain recognises `cd <literal dir> && ...` where the cd is the first
// element of a single `&&` list and the only directory change on the line. It
// returns the directory the rest of the list runs in and the cd call itself.
func literalCdChain(i Input, tree *syntax.File) (string, *syntax.CallExpr, bool) {
	if i.runtimeLine || i.Cwd == "" || len(tree.Stmts) != 1 {
		return "", nil, false
	}
	top := tree.Stmts[0]
	if top.Background || top.Coprocess {
		return "", nil, false
	}
	stmt := top
	chained := false
	for {
		bin, ok := stmt.Cmd.(*syntax.BinaryCmd)
		if !ok {
			break
		}
		if bin.Op != syntax.AndStmt || stmt.Background || stmt.Negated || len(stmt.Redirs) != 0 {
			return "", nil, false
		}
		chained = true
		stmt = bin.X
	}
	if !chained || stmt.Negated || stmt.Background || stmt.Coprocess || len(stmt.Redirs) != 0 {
		return "", nil, false
	}
	call, ok := stmt.Cmd.(*syntax.CallExpr)
	if !ok || len(call.Assigns) != 0 || len(call.Args) != 2 {
		return "", nil, false
	}
	if name, ok := claudeLiteral(call.Args[0]); !ok || name != "cd" {
		return "", nil, false
	}
	path, ok := claudeLiteral(call.Args[1])
	if !ok || path == "" || strings.HasPrefix(path, "-") || strings.HasPrefix(path, "+") {
		return "", nil, false
	}
	for _, part := range strings.Split(path, "/") {
		if part == ".." {
			return "", nil, false
		}
	}
	if countDirectoryChanges(tree) != 1 || evalNestedChangesDirectory(i, tree) {
		return "", nil, false
	}
	hasFunc := false
	syntax.Walk(tree, func(node syntax.Node) bool {
		if _, ok := node.(*syntax.FuncDecl); ok {
			hasFunc = true
		}
		return !hasFunc
	})
	if hasFunc {
		return "", nil, false
	}
	if path != "." && !strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "./") {
		if i.Env["CDPATH"] != "" || i.shellVariables["CDPATH"] != "" {
			return "", nil, false
		}
	}
	dir := path
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(i.Cwd, dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", nil, false
	}
	dir = filepath.Clean(dir)
	if autoCdWord(tree, call, i.Cwd, dir) {
		return "", nil, false
	}
	return dir, call, true
}

// autoCdWord reports a command word that zsh's auto_cd could treat as a
// directory change (a directory name, "-", or a ~ form). Such a word would move
// the shell somewhere the rest of the list is not judged in, so the line keeps
// the uncertain directory. Words that are not literal are refused too.
func autoCdWord(tree *syntax.File, cd *syntax.CallExpr, cwd, dir string) bool {
	found := false
	syntax.Walk(tree, func(node syntax.Node) bool {
		call, ok := node.(*syntax.CallExpr)
		if found || !ok || call == cd || len(call.Args) == 0 {
			return !found
		}
		word, literal := claudeLiteral(call.Args[0])
		if !literal || word == "" || word == "-" || strings.HasPrefix(word, "~") || strings.HasPrefix(word, "+") {
			found = true
			return false
		}
		for _, base := range []string{dir, cwd} {
			candidate := word
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(base, candidate)
			}
			if st, err := os.Stat(candidate); err == nil && st.IsDir() {
				found = true
				return false
			}
		}
		return true
	})
	return found
}
