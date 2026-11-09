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
	if !ok || path == "" || strings.HasPrefix(path, "-") {
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
	return filepath.Clean(dir), call, true
}
