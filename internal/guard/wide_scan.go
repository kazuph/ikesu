package guard

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// wide-scan: 起動した場所より上を丸ごと探すコマンドを、最初は確認なしで失敗させる。
// 申請文が付いた2回目は通常の確認（ask）にする。

const wideScanRule = "wide-scan"

var wideScanAlways = strings.Fields("find fd fdfind rg ag ack du tree locate bfs")
var wideScanRecursive = strings.Fields("grep egrep fgrep ls")
var wideScanFixed = []string{"/", "/Users", "/Volumes", "/System", "/Library"}

// blocksRun は、確認を出さずに止める判定（block と deny）を表す。
func blocksRun(decision string) bool { return decision == "block" || decision == "deny" }

func wideScan(i Input) (Verdict, bool) {
	target, ok := wideScanTarget(i)
	if !ok {
		return Verdict{}, false
	}
	if strings.TrimSpace(requestReason(i)) != "" {
		return result("ask", wideScanRule, "大規模なスキャンを検知: "+target+"。起動した場所（"+i.LaunchCwd+"）より上を丸ごと探しています。"), true
	}
	return result("deny", wideScanRule, "【警告】大規模なスキャンを検知: "+target+"。起動した場所（"+i.LaunchCwd+"）より上を丸ごと探しています。本当に必要なら、範囲を絞るか、AI_GUARD_REQUEST_REASON に理由を書いて、もう一度実行してください。"), true
}

// PrintDeny は、確認なしで失敗させた時の警告を書く。
func PrintDeny(w io.Writer, i Input, v Verdict) {
	fmt.Fprintln(w, v.Reason)
}

func wideScanTarget(i Input) (string, bool) {
	cmd := filepath.Base(i.Command)
	args := i.Args
	switch {
	case cmd == "mdfind":
		scoped := false
		for n := 0; n < len(args); n++ {
			if args[n] == "-onlyin" && n+1 < len(args) {
				scoped = true
				if wideScanHit(i, args[n+1]) {
					return args[n+1], true
				}
				n++
			}
		}
		if !scoped {
			return "/", true
		}
		return "", false
	case cmd == "locate":
		return "/", true
	case has(wideScanRecursive, cmd):
		if !recursiveFlag(cmd, args) {
			return "", false
		}
	case !has(wideScanAlways, cmd):
		return "", false
	}
	for _, path := range scanPaths(cmd, args) {
		if wideScanHit(i, path) {
			return path, true
		}
	}
	return "", false
}

func recursiveFlag(cmd string, args []string) bool {
	for _, a := range args {
		if a == "--" {
			return false
		}
		if a == "--recursive" || a == "--dereference-recursive" {
			return true
		}
		if len(a) > 1 && a[0] == '-' && a[1] != '-' {
			letters := "rR"
			if cmd == "ls" {
				letters = "R"
			}
			if strings.ContainsAny(a[1:], letters) {
				return true
			}
		}
	}
	return false
}

// scanPaths は探す対象にあたる引数を返す。見つからなければ "." を返す。
func scanPaths(cmd string, args []string) []string {
	var paths []string
	if cmd == "find" {
		n := 0
		for n < len(args) && has(strings.Fields("-H -L -P -E -X -x -s -d"), args[n]) {
			n++
		}
		for ; n < len(args); n++ {
			a := args[n]
			if strings.HasPrefix(a, "-") || a == "(" || a == "!" || a == "," {
				break
			}
			paths = append(paths, a)
		}
	} else {
		for _, a := range args {
			if a != "" && !strings.HasPrefix(a, "-") {
				paths = append(paths, a)
			}
		}
	}
	if len(paths) == 0 {
		return []string{"."}
	}
	return paths
}

// wideScanHit は、実在するディレクトリ path が上の階層・ホーム・ルートなどにあたるかを返す。
func wideScanHit(i Input, path string) bool {
	if strings.HasPrefix(path, "~") {
		home := i.Env["HOME"]
		if home == "" {
			return false
		}
		path = home + strings.TrimPrefix(path, "~")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(i.Cwd, path)
	}
	if st, err := os.Stat(path); err != nil || !st.IsDir() {
		return false
	}
	p := physical(path)
	launch := ""
	if i.LaunchCwd != "" {
		launch = physical(i.LaunchCwd)
		if p == launch || strings.HasPrefix(p, strings.TrimSuffix(launch, "/")+"/") {
			return false
		}
		if launch != "/" && strings.HasPrefix(launch, strings.TrimSuffix(p, "/")+"/") || p == "/" {
			return true
		}
	}
	if home := i.Env["HOME"]; home != "" && p == physical(home) {
		return true
	}
	for _, fixed := range wideScanFixed {
		if p == physical(fixed) {
			return true
		}
	}
	return false
}
