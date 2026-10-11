package guard

import (
	"regexp"
	"strings"
)

var replyEndpoint = regexp.MustCompile(`^repos/[^/]+/[^/]+/pulls/(comments/[0-9]+|[0-9]+/comments/[0-9]+)/replies$`)
var commentEndpoint = regexp.MustCompile(`^repos/[^/]+/[^/]+/(issues|pulls)/comments/[0-9]+$`)

func github(i Input) Verdict {
	a := i.Args
	allow := result("allow", "gh-safe", "GitHubの確認不要操作")
	ask := result("ask", "gh-write", "GitHubの書き込みには承認が必要です。")
	if arg(a, 0) == "api" {
		method := "GET"
		endpoint := ""
		skip := false
		for n, t := range a[1:] {
			if skip {
				skip = false
				continue
			}
			if has(strings.Fields("-X --method -H --header -f --raw-field -F --field -q --jq --hostname --input --preview -R --repo --cache"), t) {
				if t == "-X" || t == "--method" {
					method = strings.ToUpper(arg(a, n+2))
				}
				skip = true
				continue
			}
			if strings.HasPrefix(t, "--method=") {
				method = strings.ToUpper(strings.TrimPrefix(t, "--method="))
			}
			if strings.HasPrefix(t, "-X") && len(t) > 2 {
				method = strings.ToUpper(t[2:])
			}
			if !strings.HasPrefix(t, "-") && endpoint == "" {
				endpoint = t
			}
		}
		endpoint = strings.TrimPrefix(strings.TrimPrefix(endpoint, "https://api.github.com/"), "http://api.github.com/")
		endpoint = strings.TrimPrefix(endpoint, "/")
		if !writeMethod(method) || method == "PATCH" && commentEndpoint.MatchString(endpoint) || method == "POST" && replyEndpoint.MatchString(endpoint) {
			return allow
		}
		return ask
	}
	reads := map[string]string{"alias": "list", "auth": "status", "cache": "list", "config": "get list", "gist": "list view", "issue": "list status view", "label": "list", "org": "list", "pr": "checks diff list status view", "release": "list view", "repo": "list view", "run": "list view", "workflow": "list view"}
	if arg(a, 0) == "search" || has(strings.Fields(reads[arg(a, 0)]), arg(a, 1)) {
		return allow
	}
	if arg(a, 0) == "pr" && arg(a, 1) == "create" {
		repo := ""
		for n, t := range a {
			if t == "--repo" || t == "-R" {
				repo = arg(a, n+1)
			}
			if strings.HasPrefix(t, "--repo=") {
				repo = strings.TrimPrefix(t, "--repo=")
			}
			if strings.HasPrefix(t, "-R") && len(t) > 2 {
				repo = t[2:]
			}
		}
		if repo == "" {
			repo = probe(i, "gh", "repo", "view", "--json", "nameWithOwner", "--jq", ".nameWithOwner")
		}
		if repo == "" {
			repo = repository(probe(i, "git", "remote", "get-url", "origin"))
		}
		if !strings.Contains(repo, "/") {
			return result("block", "gh-target", "PR の target repo を特定できません。--repo を明示してください。")
		}
		if probe(i, "gh", "api", "/user", "--jq", ".login") == "" {
			return result("block", "gh-auth", "gh 認証情報が取得できないため block。gh auth status を確認してください。")
		}
		return ownerVerdict(i, repo, "PR 作成")
	}
	// SPEC requires foreign repository operations to ask; explicit repository
	// selection is checked before command-specific write classification.
	for n, t := range a {
		repo := ""
		if t == "--repo" || t == "-R" {
			repo = arg(a, n+1)
		}
		if strings.HasPrefix(t, "--repo=") {
			repo = strings.TrimPrefix(t, "--repo=")
		}
		if repo != "" && ownerVerdict(i, repo, "操作").Decision != "allow" {
			return ask
		}
	}
	writes := map[string]string{"auth": "login logout refresh setup-git", "gist": "create edit delete", "label": "clone create delete edit", "release": "create delete delete-asset edit upload", "repo": "create delete edit", "secret": "delete set", "variable": "delete set", "workflow": "disable enable run"}
	if has(strings.Fields(writes[arg(a, 0)]), arg(a, 1)) {
		return ask
	}
	return allow
}
