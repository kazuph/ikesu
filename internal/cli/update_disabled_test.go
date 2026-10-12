package cli

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestG8UpdateAbsentFromHelp(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"pane", "help"}} {
		var out, err bytes.Buffer
		Run(args, Options{Stdout: &out, Stderr: &err})
		if strings.Contains(strings.ToLower(out.String()+err.String()), "update") {
			t.Fatal("update in help", out.String(), err.String())
		}
	}
}

// Check the executable's own startup code for any Internet transport. Its
// existing net.Dial and net.Listen calls are restricted to Unix sockets.
func TestG8StartupHasNoUpdateNetworkTransport(t *testing.T) {
	root := filepath.Join("..", "..")
	count := 0
	for _, dir := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if e != nil {
				return e
			}
			aliases := map[string]string{}
			for _, imp := range f.Imports {
				name, _ := strconv.Unquote(imp.Path.Value)
				if name == "net/http" || strings.HasPrefix(name, "net/http/") {
					t.Errorf("Internet HTTP dependency in %s", path)
				}
				if name == "net" {
					alias := "net"
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					aliases[alias] = name
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				sel, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				ident, ok := sel.X.(*ast.Ident)
				if !ok || aliases[ident.Name] != "net" {
					return true
				}
				if sel.Sel.Name == "Dial" || sel.Sel.Name == "DialTimeout" || sel.Sel.Name == "Listen" {
					count++
					lit, ok := call.Args[0].(*ast.BasicLit)
					if !ok || lit.Value != "\"unix\"" {
						t.Errorf("non-Unix transport in %s", path)
					}
				} else if (sel.Sel.Name == "FileConn" || sel.Sel.Name == "FileListener") && filepath.ToSlash(path) == "../../internal/server/handoff.go" {
					// Only wraps inherited Unix descriptors; it never opens an Internet connection.
				} else {
					t.Errorf("unreviewed network operation %s in %s", sel.Sel.Name, path)
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if count == 0 {
		t.Fatal("startup socket paths not checked")
	}
}
