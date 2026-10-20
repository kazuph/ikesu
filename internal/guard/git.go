package guard

import (
	"context"
	"github.com/kazuph/ikesu/internal/childenv"
	"path/filepath"
	"strings"
	"time"
)

// Probes are read-only, use the caller's environment, and remove our own shim.
func probe(i Input, cmd string, a ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env := environment(i.Env)
	p, e := Resolve(cmd, env)
	if e != nil {
		return ""
	}
	c := childenv.CommandContext(ctx, p, a...)
	c.Dir = i.Cwd
	c.Env = childenv.Scrub(env)
	b, e := c.Output()
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func force(a []string) bool {
	for _, v := range a {
		if has([]string{"--force", "--force-with-lease", "-f"}, v) || strings.HasPrefix(v, "--force-with-lease=") {
			return true
		}
	}
	return false
}
func mainBranch(s string) bool {
	return has([]string{"main", "master", "refs/heads/main", "refs/heads/master"}, s)
}
func gitPush(i Input) Verdict {
	a := i.Args[1:]
	if force(a) {
		return result("block", "git-push-force", "git push --force 系は禁止です。履歴を書き換えず通常の push を使ってください。")
	}
	branch := probe(i, "git", "symbolic-ref", "--quiet", "--short", "HEAD")
	destMain := mainBranch(branch)
	count := 0
	remote := "origin"
	for _, v := range a {
		if v == "--" {
			break
		}
		if strings.HasPrefix(v, "-") {
			continue
		}
		count++
		if count == 1 {
			remote = v
			continue
		}
		if strings.Contains(v, ":") {
			v = v[strings.LastIndex(v, ":")+1:]
			if v == "" || v == "HEAD" {
				v = branch
			}
		} else if v == "HEAD" {
			v = branch
		}
		destMain = destMain || mainBranch(v)
	}
	if destMain {
		root := probe(i, "git", "rev-parse", "--show-toplevel")
		if root != "" && regularFile(filepath.Join(root, ".allow-main")) {
			return result("allow", "main-permit", "main/master の許可ファイルあり")
		}
		return result("block", "git-main", "main/master への push は AI からは禁止です。必要なら人間が手動で実行してください。")
	}
	repo := probe(i, "gh", "repo", "view", probe(i, "git", "remote", "get-url", remote), "--json", "nameWithOwner", "--jq", ".nameWithOwner")
	if repo == "" {
		repo = repository(probe(i, "git", "remote", "get-url", remote))
	}
	return ownerVerdict(i, repo, "push")
}
func repository(s string) string {
	if n := strings.Index(s, "github.com"); n >= 0 {
		s = strings.TrimLeft(s[n+len("github.com"):], ":/")
		return strings.TrimSuffix(s, ".git")
	}
	return ""
}
func ownerVerdict(i Input, repo, operation string) Verdict {
	ask := result("ask", "ownership", "リモート・所有者を確認できないため承認が必要です。")
	if !strings.Contains(repo, "/") {
		return ask
	}
	login := probe(i, "gh", "api", "/user", "--jq", ".login")
	if login == "" {
		return ask
	}
	owner, _, _ := strings.Cut(repo, "/")
	if owner == login {
		return result("allow", "owner", "所有リポジトリ")
	}
	typ := probe(i, "gh", "api", "repos/"+repo, "--jq", ".owner.type")
	if typ == "Organization" {
		p := probe(i, "gh", "api", "repos/"+repo, "--jq", ".permissions.push")
		if p == "true" {
			return result("allow", "organization", "組織への書き込み権限あり")
		}
		if p == "false" {
			return result("block", "organization-denied", "組織 ("+owner+") への "+operation+" 権限がないため禁止です。")
		}
		p = probe(i, "gh", "api", "repos/"+repo+"/collaborators/"+login+"/permission", "--jq", ".permission")
		if has([]string{"admin", "maintain", "write"}, p) {
			return result("allow", "organization", "組織への書き込み権限あり")
		}
		if has([]string{"read", "triage"}, p) {
			return result("block", "organization-denied", "組織 ("+owner+") への "+operation+" 権限がないため禁止です。")
		}
		return ask
	}
	if operation == "push" && typ != "" {
		return result("block", "foreign-repo", "非所有リポジトリ ("+owner+") への push は禁止です。")
	}
	if operation == "PR 作成" {
		return result("block", "foreign-repo", "AI による他人所有リポ ("+repo+") への PR 作成は禁止です。")
	}
	return ask
}
