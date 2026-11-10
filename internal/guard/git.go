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
	if i.probePrivate {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	env := environment(i.Env)
	p, e := Resolve(cmd, env)
	if e != nil {
		return ""
	}
	if cmd == "git" && len(i.gitOptions) > 0 {
		a = append(append([]string{}, i.gitOptions...), a...)
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
	s = strings.TrimPrefix(s, "+")
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
	if i.gitDirUnknown {
		// The repository behind an unresolved -C is unknown, so neither the
		// .allow-main permit nor ownership of the current directory applies.
		if destMain {
			return result("block", "git-main", "main/master への push は AI からは禁止です。必要なら人間が手動で実行してください。")
		}
		return result("ask", "ownership", "git -C の場所を確定できないため承認が必要です。")
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

// normalizeGit reads git's global options off the front of the arguments so the
// rules see the real subcommand. -C accumulates into the working directory the
// judgment probes run in; the other options are handed to every git probe.
// It is idempotent: once the subcommand leads, nothing more is stripped.
func normalizeGit(i Input) Input {
	if filepath.Base(i.Command) != "git" {
		return i
	}
	a := i.Args
	n := 0
	var opts []string
	cwd := i.Cwd
	for n < len(a) {
		v := a[n]
		switch {
		case v == "-C":
			if n+1 >= len(a) {
				return i
			}
			if a[n+1] == unresolvedPlaceholder {
				i.gitDirUnknown = true
			}
			if d := a[n+1]; d != "" && d != unresolvedPlaceholder {
				if !filepath.IsAbs(d) {
					d = filepath.Join(cwd, d)
				}
				cwd = d
			}
			n += 2
		case v == "-c" || v == "--git-dir" || v == "--work-tree" || v == "--namespace" || v == "--config-env" || v == "--super-prefix":
			if n+1 >= len(a) {
				return i
			}
			// Only repository-locating options reach the probes: -c and the
			// like could run configured commands before anyone approves.
			if has([]string{"--git-dir", "--work-tree", "--namespace"}, v) {
				opts = append(opts, v, a[n+1])
			}
			if (v == "-c" || v == "--config-env") && strings.HasPrefix(strings.ToLower(a[n+1]), "alias.") {
				i.gitAlias = true
			}
			n += 2
		case strings.HasPrefix(v, "--config-env="):
			if strings.HasPrefix(strings.ToLower(strings.TrimPrefix(v, "--config-env=")), "alias.") {
				i.gitAlias = true
			}
			n++
		case strings.HasPrefix(v, "--git-dir=") || strings.HasPrefix(v, "--work-tree=") || strings.HasPrefix(v, "--namespace="):
			opts = append(opts, v)
			n++
		case strings.HasPrefix(v, "--super-prefix=") || strings.HasPrefix(v, "--exec-path="):
			// Not passed to probes: --exec-path picks which git programs run.
			n++
		case has([]string{"--no-pager", "-P", "-p", "--paginate", "--no-optional-locks", "--bare", "--no-replace-objects", "--literal-pathspecs", "--glob-pathspecs", "--noglob-pathspecs", "--icase-pathspecs", "--no-lazy-fetch", "--no-advice"}, v):
			opts = append(opts, v)
			n++
		default:
			if n == 0 {
				return i
			}
			i.gitStripped = append(append([]string{}, i.gitStripped...), a[:n]...)
			i.Args = append([]string{}, a[n:]...)
			i.Cwd = cwd
			i.gitOptions = append(append([]string{}, i.gitOptions...), opts...)
			return i
		}
	}
	return i
}
