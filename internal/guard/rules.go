// Package guard implements SPEC G12 and the authorized ai_guard policy.
package guard

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type Config struct {
	Enabled           *bool    `toml:"enabled"`
	Mode              string   `toml:"mode"`
	Targets           []string `toml:"targets"`
	DangerWords       []string `toml:"danger_words"`
	ProtectedPatterns []string `toml:"protected_patterns"`
	WranglerAllow     []string `toml:"wrangler_allow"`
	AskTimeoutSeconds *int     `toml:"ask_timeout_seconds"`
}

func (c Config) Active() bool { return c.Enabled == nil || *c.Enabled }
func (c Config) EffectiveMode() string {
	if c.Mode == "" {
		return "enforce"
	}
	return c.Mode
}
func (c Config) Timeout() time.Duration {
	if c.AskTimeoutSeconds == nil {
		return 300 * time.Second
	}
	return time.Duration(*c.AskTimeoutSeconds) * time.Second
}
func defaults(v, d []string) []string {
	if v == nil {
		return d
	}
	return v
}
func (c Config) Commands() []string {
	return defaults(c.Targets, strings.Fields("rm rmdir rimraf trash mv dd mkfs fdisk diskutil format parted gparted git gh curl wget http https aws npm npx pnpm pnpx yarn bun bunx deno cargo env arch firebase vercel flyctl fly wrangler sqlite3 psql mysql netlify railway render amplify cdk serverless sls pulumi terraform ikesu herdr touch tee cp ln"))
}

type Input struct {
	VerifiedOutside      *Marker                   `json:"-"`
	ApprovedInvocation   func(string, string) bool `json:"-"`
	ShellParentDirenv    func() (string, string)   `json:"-"`
	runtimeLine          bool
	evalDepth            int
	shellVariables       map[string]string
	shellPID             string
	shellArrays          map[string]map[int]string
	probePrivate         bool
	OpaquePrefix         bool `json:"-"`
	scriptVariables      map[string]bool
	claudeBashEnv        string
	claudeHarness        bool
	scriptScanner        *scriptScanner
	scriptResult         *scriptResult
	scriptBody           bool
	scriptLabel          string
	scriptLine           uint
	scriptKnownArgs      []string
	scriptUnknownCommand bool
	TriggerCommand       string            `json:"-"` // Display only; never used by policy or approval keys.
	CwdUncertain         bool              `json:"-"`
	AuthorizationEnv     map[string]string `json:"-"`
	GuardCommands        []string          `json:"-"`
	GuardRoot            string            `json:"-"`
	ClaudePrefixChanged  bool              `json:"-"`
	ShellEnvChanged      bool              `json:"-"`
	Unresolved           bool              `json:"-"`
	RawLine              string            `json:"-"`
	Command              string            `json:"command"`
	Args                 []string          `json:"args"`
	Cwd                  string            `json:"cwd"`
	LaunchCwd            string            `json:"launch_cwd"`
	Env                  map[string]string `json:"-"`
}
type Verdict struct {
	Decision   string `json:"verdict"`
	Rule       string `json:"rule"`
	Reason     string `json:"reason"`
	TempKey    string `json:"approval_key"`
	DayKey     string `json:"-"`
	Suppressed string `json:"suppressed_by"`
}

func (v Verdict) MarshalJSON() ([]byte, error) {
	var key, suppressed *string
	if v.Decision == "ask" || v.Suppressed != "" {
		if v.TempKey != "" {
			key = &v.TempKey
		}
	}
	if v.Suppressed != "" {
		suppressed = &v.Suppressed
	}
	return json.Marshal(struct {
		ApprovalKey *string `json:"approval_key"`
		Reason      string  `json:"reason"`
		Rule        string  `json:"rule"`
		Suppressed  *string `json:"suppressed_by"`
		Verdict     string  `json:"verdict"`
	}{key, v.Reason, v.Rule, suppressed, v.Decision})
}

func (i Input) Line() string {
	if i.RawLine != "" {
		return i.RawLine
	}
	return strings.Join(append([]string{i.Command}, i.Args...), " ")
}
func has(a []string, s string) bool {
	for _, v := range a {
		if v == s {
			return true
		}
	}
	return false
}
func arg(a []string, n int) string {
	if n < len(a) {
		return a[n]
	}
	return ""
}
func result(d, r, s string) Verdict { return Verdict{Decision: d, Rule: r, Reason: s} }
func Evaluate(c Config, i Input) Verdict {
	i.GuardCommands = c.Commands()
	return evaluateVerdict(c, i, classify(c, i))
}
func evaluateVerdict(c Config, i Input, v Verdict) Verdict {
	v.DayKey = "day:" + physical(i.Cwd)
	v.TempKey = temporaryKey(c, i)
	if v.Decision == "ask" && v.TempKey != "" {
		f := i.Env["AI_GUARD_TEMP_APPROVAL_FILE"]
		if f == "" {
			f = filepath.Join(i.Env["HOME"], ".ai_guard_temp_approval")
		}
		if validApproval(f, v.DayKey, time.Now()) {
			v.Decision, v.Suppressed = "allow", "day"
		} else if validApproval(f, v.TempKey, time.Now()) {
			v.Decision, v.Suppressed = "allow", "temporary"
		}
	}
	return v
}
func classify(c Config, i Input) Verdict {
	cmd, a := filepath.Base(i.Command), i.Args
	for _, p := range defaults(c.ProtectedPatterns, []string{".allow-main", ".allow_main", "allow-main", "allow_main"}) {
		if p != "" && strings.Contains(i.Line(), p) {
			return result("block", "protected", "AIプロセスによる .allow-main 関連操作は完全に禁止されています")
		}
	}
	if has([]string{"ikesu", "herdr"}, cmd) && wildcard(a) {
		return result("block", "wildcard", "宛先 '*' は --room を付けても room 内に限定されず、全agentへ一斉配信されます。これは承認では解除できません。")
	}
	if reason := commandBoundary(i); reason != "" {
		return result("ask", "guard-boundary", reason)
	}
	if cmd == "git" {
		switch arg(a, 0) {
		case "commit":
			if has(a[1:], "--amend") {
				return result("block", "git-commit-amend", "git commit --amend による履歴上書きは禁止です。修正は追いコミットで積んでください。")
			}
		case "reset":
			return result("block", "git-reset", "git reset による巻き戻しは禁止です。履歴は書き換えず、修正コミットで前に進めてください。")
		case "wt":
			if arg(a, 1) == "list" {
				return result("block", "git-wt-list", "git wt をそのまま実行すれば、worktreeリストが出てきます。")
			}
		case "push":
			return gitPush(i)
		}
	}
	if cmd == "gh" {
		return github(i)
	}
	allow := result("allow", "default", "")
	ask := result("ask", "dangerous", "実行前に承認が必要です。")
	if i.Env["AI_GUARD_DANGER_WORD_ACK"] == "1" {
		return allow
	}
	if cmd == "sqlite3" || localWrangler(i) {
		return allow
	}
	if d1(i) {
		if exactD1Help(i) || readonlySQL(sqlArgument(a)) && !fileSQL(a) {
			return allow
		}
		return result("ask", "d1-sql-write", "D1 への書き込み SQL は確認が必要です")
	}
	if danger(c, i) {
		return result("ask", "danger-word", "deploy/publish 系の操作は確認が必要です")
	}
	switch cmd {
	case "rm", "rmdir", "rimraf", "trash":
		probes := map[string]string{}
		for _, t := range a {
			if !strings.HasPrefix(t, "-") && !safeRemovalWithProbes(i, t, probes) {
				return result("ask", "rm-paths", "削除操作は確認が必要です")
			}
		}
		return allow
	case "mv":
		if strings.Contains(strings.Join(a, " "), "bak") || strings.Contains(strings.Join(a, " "), "backup") {
			return ask
		}
	case "dd", "mkfs", "fdisk", "diskutil", "format", "parted", "gparted":
		return ask
	case "git":
		if arg(a, 0) == "clean" || arg(a, 0) == "stash" && has([]string{"drop", "clear"}, arg(a, 1)) || has([]string{"cherry-pick", "merge"}, arg(a, 0)) && arg(a, 1) == "--abort" {
			return ask
		}
	case "curl", "wget", "http", "https":
		if writeMethod(httpMethod(a, cmd)) && !localHTTPDestinations(i) {
			return result("ask", "http-write", "HTTPの書き込みには承認が必要です。")
		}
	case "aws":
		return result("ask", "aws", "AWSコマンドには承認が必要です。")
	case "wrangler":
		if has(defaults(c.WranglerAllow, []string{"r2 put"}), arg(a, 0)+" "+arg(a, 1)) {
			return allow
		}
		return result("ask", "wrangler", "wrangler の許可ペア外には承認が必要です。")
	case "psql", "mysql":
		if !readonlySQL(sqlArgument(a)) {
			return result("ask", "sql-write", "SQLの書き込みには承認が必要です。")
		}
	}
	return allow
}
func physical(p string) string {
	if v, e := filepath.EvalSymlinks(p); e == nil {
		return v
	}
	return filepath.Clean(p)
}
func safeRemoval(i Input, t string) bool {
	return safeRemovalWithProbes(i, t, map[string]string{})
}
func safeRemovalWithProbes(i Input, t string, probes map[string]string) bool {
	if strings.Contains(t, "..") {
		return false
	}
	p := t
	if !filepath.IsAbs(p) {
		p = filepath.Join(i.Cwd, p)
	}
	if i.LaunchCwd != "" && physical(p) == physical(i.LaunchCwd) {
		return false
	}
	if physical(p) == physical(i.Env["HOME"]) {
		return false
	}
	for _, r := range []string{"/tmp", "/private/tmp", "/var/folders", "/private/var/folders", i.Env["TMPDIR"]} {
		r = strings.TrimRight(r, "/")
		if r != "" && (p == r || strings.HasPrefix(p, r+"/")) {
			return safeTemporaryRemoval(i, p)
		}
	}
	if strings.Contains(p, "/.artifacts/") || strings.HasSuffix(p, "/.artifacts") {
		return safeTemporaryRemoval(i, p)
	}
	d := guardPhysical(p)
	for {
		if st, e := os.Stat(d); e == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	probeInput := removalProbeInput(i)
	if i.probePrivate {
		// Git sees only the caller's cwd and worktree paths from its own list.
		probeInput.probePrivate = false
		if d != guardPhysical(i.Cwd) {
			var ok bool
			d, ok = privateRemovalWorktree(probeInput, d)
			if !ok {
				return false
			}
		}
	}
	gitPath := func(dir, option string) string {
		key := dir + "\x00" + option
		if value, ok := probes[key]; ok {
			return value
		}
		args := []string{"-C", dir, "rev-parse"}
		if option == "--git-common-dir" {
			args = append(args, "--path-format=absolute")
		}
		value := probe(probeInput, "git", append(args, option)...)
		probes[key] = value
		return value
	}
	top := gitPath(d, "--show-toplevel")
	if top != "" && (guardPhysical(p) == physical(top) || filepath.Clean(p) == filepath.Join(filepath.Clean(top), ".git") || guardPhysical(p) == filepath.Join(physical(top), ".git")) {
		return false
	}
	l := physical(i.LaunchCwd)
	p = physical(p)
	if i.LaunchCwd == "" || l == physical(i.Env["HOME"]) || top == "" {
		return false
	}
	launchCommon := gitPath(l, "--git-common-dir")
	targetCommon := gitPath(d, "--git-common-dir")
	return launchCommon != "" && targetCommon != "" && physical(launchCommon) == physical(targetCommon) && strings.HasPrefix(guardPhysical(p), physical(top)+"/")
}

func removalGitRoot(path string) bool {
	if filepath.Base(filepath.Clean(path)) == ".git" {
		return true
	}
	st, err := os.Stat(path)
	if err != nil {
		return !os.IsNotExist(err)
	}
	if !st.IsDir() {
		return false
	}
	_, err = os.Lstat(filepath.Join(path, ".git"))
	return err == nil || !os.IsNotExist(err)
}

func safeTemporaryRemoval(i Input, path string) bool {
	if removalGitRoot(path) {
		return false
	}
	dir := guardPhysical(path)
	for {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
	repoRoot := ""
	for {
		_, err := os.Lstat(filepath.Join(dir, ".git"))
		if err == nil {
			repoRoot = dir
			break
		}
		if !os.IsNotExist(err) {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	if repoRoot == "" {
		return true
	}
	if i.LaunchCwd == "" {
		return false
	}
	public := removalProbeInput(i)
	public.probePrivate = false
	list := probe(public, "git", "-C", i.LaunchCwd, "worktree", "list", "--porcelain", "-z")
	for _, record := range strings.Split(list, "\x00") {
		if strings.HasPrefix(record, "worktree ") && physical(strings.TrimPrefix(record, "worktree ")) == repoRoot {
			return true
		}
	}
	return false
}

func removalProbeInput(i Input) Input {
	i.Env = EnvMap(environment(i.Env))
	for key := range i.Env {
		if has([]string{"GIT_DIR", "GIT_WORK_TREE", "GIT_COMMON_DIR", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_CONFIG"}, key) || strings.HasPrefix(key, "GIT_CONFIG_") {
			delete(i.Env, key)
		}
	}
	return i
}

func privateRemovalWorktree(i Input, targetDir string) (string, bool) {
	list := probe(i, "git", "-C", i.Cwd, "worktree", "list", "--porcelain", "-z")
	root := ""
	for _, record := range strings.Split(list, "\x00") {
		if !strings.HasPrefix(record, "worktree ") {
			continue
		}
		candidate := physical(strings.TrimPrefix(record, "worktree "))
		if (targetDir == candidate || strings.HasPrefix(targetDir, candidate+"/")) && len(candidate) > len(root) {
			root = candidate
		}
	}
	if root == "" {
		return "", false
	}
	rel, err := filepath.Rel(root, targetDir)
	if err != nil {
		return "", false
	}
	current := root
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			if _, err := os.Lstat(filepath.Join(current, ".git")); err == nil || !os.IsNotExist(err) {
				return "", false
			}
		}
	}
	return root, true
}
func wildcard(a []string) bool {
	send, skip := false, false
	mode := ""
	for _, t := range a {
		if send {
			if skip {
				skip = false
				continue
			}
			if t == "--" {
				continue
			}
			if strings.Contains(t, "=") && strings.HasPrefix(t, "-") {
				continue
			}
			if has(strings.Fields("--room --reply-to --from --to --label --project --caller --workspace --tab --cwd --wait --timeout"), t) {
				skip = true
				continue
			}
			if strings.HasPrefix(t, "-") {
				continue
			}
			return t == "*"
		}
		if t == "send" && (mode == "" || mode == "msg" || mode == "agent") {
			send = true
		}
		if t == "msg" || t == "agent" {
			mode = t
		}
	}
	return false
}
func danger(c Config, i Input) bool {
	if i.runtimeLine {
		i.RawLine = ""
		i.Args = i.scriptKnownArgs
	}
	if has([]string{"git", "gh", "ikesu", "herdr"}, filepath.Base(i.Command)) {
		return false
	}
	for _, w := range strings.FieldsFunc(strings.ToLower(i.Line()), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if has(defaults(c.DangerWords, []string{"publish", "deploy"}), w) {
			return true
		}
	}
	return false
}
func temporaryKey(c Config, i Input) string {
	cmd, a := filepath.Base(i.Command), i.Args
	if wildcard(a) && has([]string{"ikesu", "herdr"}, cmd) || has(strings.Fields("dd mkfs fdisk diskutil format parted gparted"), cmd) {
		return ""
	}
	scope := probe(i, "git", "rev-parse", "--show-toplevel")
	if scope == "" {
		scope = physical(i.Cwd)
	}
	if has([]string{"trash", "rmdir", "rimraf", "mv"}, cmd) {
		return "delete:" + cmd + ":" + scope
	}
	if cmd == "git" {
		g := ""
		switch arg(a, 0) {
		case "reset", "restore":
			g = "reset-restore"
		case "clean":
			g = "clean"
		case "push":
			g = "push-review"
			if force(a) {
				g = "push-force"
			}
		case "stash":
			if has([]string{"drop", "clear"}, arg(a, 1)) {
				g = "stash-drop-clear"
			}
		case "merge", "cherry-pick":
			if arg(a, 1) == "--abort" {
				g = arg(a, 0) + "-abort"
			}
		}
		if g != "" {
			return "git:" + g + ":" + scope
		}
	}
	if danger(c, i) {
		return "danger:" + cmd + ":" + deployEnvironment(i) + ":" + scope
	}
	s := arg(a, 0)
	if s == "" {
		s = "-"
	}
	return cmd + ":" + s + ":" + scope
}
func deployEnvironment(i Input) string {
	for n, t := range i.Args {
		if n > 0 && has(strings.Fields("--env -e --stage --target --profile"), i.Args[n-1]) {
			if e := envName(strings.ToLower(t), true); e != "unknown" {
				return e
			}
		}
	}
	return envName(strings.ToLower(i.Line()), false)
}
func envName(s string, exact bool) string {
	if strings.Contains(s, "prod") {
		return "prod"
	}
	for _, g := range []struct {
		words []string
		name  string
	}{{[]string{"stg", "stage", "staging"}, "stg"}, {[]string{"preview", "pre", "canary"}, "preview"}, {[]string{"dev", "development", "local"}, "dev"}} {
		for _, w := range g.words {
			if exact && s == w || !exact && w != "pre" && strings.Contains(s, w) {
				return g.name
			}
		}
	}
	return "unknown"
}
