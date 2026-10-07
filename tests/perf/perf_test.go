//go:build perf

// The perf tests measure gtx the same way tools/bench does: the binary
// runs in a 160x45 PTY, a pane shell floods it with 2,000,000 python3
// lines, and a vt.Emulator — fed from an unbounded queue that never
// throttles the app — reports when text lands on screen. See SPEC.md
// "P1. 性能要件" for what is measured and the pass/fail limits.
package perf

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
)

// The measurement setup mirrors tools/bench (bench_screen.sh,
// bench_interrupt.sh, bench_rt.sh).
const (
	termCols = 160
	termRows = 45
	lastLine = "1999999 line of throughput test"

	bootTimeout   = 60 * time.Second
	screenTimeout = 300 * time.Second
	fileTimeout   = 300 * time.Second
	intTimeout    = 60 * time.Second

	absRuns = 3 // 絶対値: 3回の中央値
	relRuns = 5 // 相対値: 変更前後を交互に各5回
)

// Absolute limits (SPEC.md "上限（絶対値）").
const (
	limitScreenSec = 3.1
	limitCPUSec    = 5.0
	limitRSSKB     = 40 * 1024
	limitIntSec    = 0.1
	limitLagSec    = 0.25
)

// Relative limits (SPEC.md "悪化の検出（相対値）").
const (
	relScreenCPU = 1.05 // 変更後の中央値 <= 変更前の中央値 x 1.05
	relRSS       = 1.10 // 変更後の最大 <= 変更前の最大 x 1.10
	relIntSec    = 0.1  // 変更後の最大 <= 0.1秒
)

// holdLoad is the 1-minute load average at/above which the absolute check
// is reported as 保留 instead of a verdict (SPEC.md).
const holdLoad = 10.0

// tp.py from tools/bench/bench_rt.sh, byte for byte.
const tpPy = `import time,sys
s=time.time()
w=sys.stdout.write
for i in range(2000000): w("%d line of throughput test\n" % i)
sys.stdout.flush()
open(sys.argv[1],"w").write("%.2f\n" % (time.time()-s))
`

// startMode is a launch style: attach through the server, or in-process.
type startMode struct {
	name string
	args func(sess string) []string
}

var modes = []startMode{
	{"server", func(sess string) []string { return []string{"--session", sess} }},
	{"no-session", func(string) []string { return []string{"--no-session"} }},
}

// rep accumulates the result tables; TestMain prints them and optionally
// writes them to $GTX_PERF_OUT.
var rep strings.Builder

func rprintf(format string, a ...any) { fmt.Fprintf(&rep, format+"\n", a...) }

func TestMain(m *testing.M) {
	host, _ := os.Hostname()
	rprintf("# gtx perf — SPEC.md P1")
	rprintf("# date=%s host=%s go=%s", time.Now().Format(time.RFC3339), host, runtime.Version())
	rprintf("# GTX_PERF_BIN=%s", os.Getenv("GTX_PERF_BIN"))
	rprintf("# GTX_PERF_BASELINE_BIN=%s", os.Getenv("GTX_PERF_BASELINE_BIN"))
	code := m.Run()
	out := rep.String()
	fmt.Print(out)
	if p := os.Getenv("GTX_PERF_OUT"); p != "" {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(out), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "GTX_PERF_OUT:", err)
		}
	}
	os.Exit(code)
}

func perfBin(t *testing.T) string {
	t.Helper()
	b := os.Getenv("GTX_PERF_BIN")
	if b == "" {
		t.Skip("GTX_PERF_BIN not set")
	}
	abs, err := filepath.Abs(b)
	if err != nil {
		t.Fatalf("GTX_PERF_BIN: %v", err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("GTX_PERF_BIN %s: %v", abs, err)
	}
	return abs
}

// TestAbsolute runs the measured binary absRuns times per startup mode and
// checks the medians against the SPEC's absolute limits. When the load
// average at a run's start was >= 10 the verdict is 保留, not a failure.
func TestAbsolute(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("perf tests measure on the macOS mini")
	}
	bin := perfBin(t)
	h := newHarness(t)
	for _, mode := range modes {
		var runs []result
		for i := 0; i < absRuns; i++ {
			runs = append(runs, h.measure(bin, mode))
		}
		reportTable(fmt.Sprintf("絶対値 mode=%s bin=%s", mode.name, bin), runs)
		judgeAbsolute(t, mode.name, runs)
	}
}

// TestRelative alternates GTX_PERF_BIN and GTX_PERF_BASELINE_BIN relRuns
// times each per startup mode and checks the SPEC's regression limits.
func TestRelative(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("perf tests measure on the macOS mini")
	}
	cur := perfBin(t)
	base := os.Getenv("GTX_PERF_BASELINE_BIN")
	if base == "" {
		t.Skip("GTX_PERF_BASELINE_BIN not set")
	}
	base, err := filepath.Abs(base)
	if err != nil {
		t.Fatalf("GTX_PERF_BASELINE_BIN: %v", err)
	}
	if _, err := os.Stat(base); err != nil {
		t.Fatalf("GTX_PERF_BASELINE_BIN %s: %v", base, err)
	}
	h := newHarness(t)
	for _, mode := range modes {
		var curRuns, baseRuns []result
		for i := 0; i < relRuns; i++ {
			curRuns = append(curRuns, h.measure(cur, mode))
			baseRuns = append(baseRuns, h.measure(base, mode))
		}
		reportTable(fmt.Sprintf("相対値 mode=%s cur=%s", mode.name, cur), curRuns)
		reportTable(fmt.Sprintf("相対値 mode=%s base=%s", mode.name, base), baseRuns)
		judgeRelative(t, mode.name, curRuns, baseRuns)
	}
}

// result is one measurement: a flood run plus a Ctrl-C run.
type result struct {
	load      float64 // 1-min load average at measurement start
	screenSec float64 // send -> last line on screen
	lagSec    float64 // python exit -> last line on screen
	cpuSec    float64 // user+sys of every gtx process
	rssKB     int64   // max RSS across the gtx processes
	ctrlcSec  float64 // Ctrl-C -> KeyboardInterrupt on screen
	pySec     float64 // python's own elapsed (informational)
	procs     string  // gtx pids seen (diagnostics)
	err       error
}

// harness owns the throwaway dirs every launch shares.
type harness struct {
	t        *testing.T
	work     string // tp.py and the *.elapsed files
	stateDir string // GTX_STATE_DIR root; one <sess> dir per launch
	zdot     string // minimal ZDOTDIR so the pane prompt is deterministic
	tpPath   string
	seq      int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	work, err := os.MkdirTemp("", "gtx-perf")
	if err != nil {
		t.Fatalf("work dir: %v", err)
	}
	// Short root: unix socket paths must stay under ~104 bytes.
	stateDir, err := os.MkdirTemp("/tmp", "gp")
	if err != nil {
		t.Fatalf("state dir: %v", err)
	}
	zdot, err := os.MkdirTemp("", "gtx-perf-zdot")
	if err != nil {
		t.Fatalf("zdot dir: %v", err)
	}
	h := &harness{t: t, work: work, stateDir: stateDir, zdot: zdot, tpPath: filepath.Join(work, "tp.py")}
	for path, data := range map[string]string{
		filepath.Join(zdot, ".zshrc"): "PS1='❯ '\n",
		h.tpPath:                      tpPy,
	} {
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	t.Cleanup(func() { os.RemoveAll(work); os.RemoveAll(stateDir); os.RemoveAll(zdot) })
	return h
}

// binPaths are the comm strings that count as gtx processes: the binary as
// invoked plus its symlink-resolved form (os.Executable can differ on
// macOS, e.g. /tmp -> /private/tmp).
func binPaths(bin string) map[string]bool {
	m := map[string]bool{bin: true}
	if r, err := filepath.EvalSymlinks(bin); err == nil {
		m[r] = true
	}
	return m
}

// measure runs one flood scenario and one Ctrl-C scenario for bin in mode.
func (h *harness) measure(bin string, mode startMode) result {
	r := result{load: load1()}
	server := mode.name == "server"
	bins := binPaths(bin)

	// Flood: screen time, python->screen lag, CPU, RSS.
	sess := fmt.Sprintf("p%d", h.seq)
	h.seq++
	m := h.start(bin, mode.args(sess))
	samp := startSampler(m.cmd.Process.Pid, bins)
	if _, ok := m.waitText(bootTimeout, "❯"); !ok {
		r.err = fmt.Errorf("no pane prompt; screen:\n%s", m.screen())
		samp.finish()
		h.stop(m, sess, server, bins)
		return r
	}
	elapsed := filepath.Join(h.work, sess+".elapsed")
	t0 := m.send(fmt.Sprintf("python3 %s %s\r", h.tpPath, elapsed))
	onAt, ok := m.waitText(screenTimeout, lastLine)
	if !ok {
		r.err = fmt.Errorf("%s not on screen within %s", lastLine, screenTimeout)
	} else {
		r.screenSec = onAt.Sub(t0).Seconds()
	}
	pyEnd, ok := waitFileMtime(elapsed, fileTimeout)
	if !ok {
		r.err = fmt.Errorf("%s not written within %s", elapsed, fileTimeout)
	} else if r.err == nil {
		r.lagSec = onAt.Sub(pyEnd).Seconds()
	}
	if b, err := os.ReadFile(elapsed); err == nil {
		r.pySec, _ = strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	}
	r.rssKB, r.cpuSec, r.procs = samp.finish()
	h.stop(m, sess, server, bins)

	// Ctrl-C: interrupt response while the flood is in flight.
	sess = fmt.Sprintf("p%d", h.seq)
	h.seq++
	m = h.start(bin, mode.args(sess))
	if _, ok := m.waitText(bootTimeout, "❯"); !ok {
		r.err = fmt.Errorf("no pane prompt (ctrlc); screen:\n%s", m.screen())
		h.stop(m, sess, server, bins)
		return r
	}
	m.send(fmt.Sprintf("python3 %s %s\r", h.tpPath, filepath.Join(h.work, sess+".elapsed")))
	time.Sleep(1 * time.Second)
	t0 = m.send("\x03")
	if at, ok := m.waitText(intTimeout, "KeyboardInterrupt"); !ok {
		r.err = fmt.Errorf("KeyboardInterrupt not on screen within %s", intTimeout)
	} else {
		r.ctrlcSec = at.Sub(t0).Seconds()
	}
	h.stop(m, sess, server, bins)
	return r
}

// start launches bin in a 160x45 PTY with a drained emulator on top,
// exactly like tools/bench/ptyrun.
func (h *harness) start(bin string, args []string) *mux {
	cmd := exec.Command(bin, args...)
	cmd.Dir = h.work
	cmd.Env = childEnv(h.zdot, h.stateDir)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: termCols, Rows: termRows})
	if err != nil {
		h.t.Fatalf("pty start %s %v: %v", bin, args, err)
	}
	m := &mux{cmd: cmd, pt: f, emu: vt.NewEmulator(termCols, termRows)}
	go func() { _, _ = io.Copy(f, m.emu) }() // emulator replies (DA etc.)
	go m.drain()
	go m.feed()
	return m
}

// stop kills the client and, in server mode, asks its server to exit and
// makes sure it did.
func (h *harness) stop(m *mux, sess string, server bool, bins map[string]bool) {
	// The server is a child of the client only while the client lives —
	// collect its pids before killing.
	var servers []int
	if server {
		for pid := range gtxPids(psTable(), m.cmd.Process.Pid, bins) {
			if pid != m.cmd.Process.Pid {
				servers = append(servers, pid)
			}
		}
	}
	if m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
	}
	done := make(chan error, 1)
	go func() { done <- m.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
	_ = m.pt.Close()
	if server {
		if c, err := net.Dial("unix", filepath.Join(h.stateDir, sess, "gtx.sock")); err == nil {
			_, _ = c.Write([]byte(`{"id":"perf","method":"server.stop","params":{}}` + "\n"))
			c.Close()
		}
		waitExit(servers, 5*time.Second)
	}
}

func waitExit(pids []int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		tbl := psTable()
		alive := false
		for _, pid := range pids {
			if _, ok := tbl[pid]; ok {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
}

// mux drives one gtx in a PTY. It is the ptyrun drain design: reads go to
// an unbounded queue and a feeder writes them to the emulator, so emulator
// speed never throttles the app under test.
type mux struct {
	cmd   *exec.Cmd
	pt    *os.File
	emu   *vt.Emulator
	mu    sync.Mutex // emu
	qmu   sync.Mutex // queue
	queue [][]byte
}

func (m *mux) drain() {
	buf := make([]byte, 1<<16)
	for {
		n, err := m.pt.Read(buf)
		if n > 0 {
			c := make([]byte, n)
			copy(c, buf[:n])
			m.qmu.Lock()
			m.queue = append(m.queue, c)
			m.qmu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (m *mux) feed() {
	for {
		m.qmu.Lock()
		q := m.queue
		m.queue = nil
		m.qmu.Unlock()
		if len(q) == 0 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		m.mu.Lock()
		for _, c := range q {
			_, _ = m.emu.Write(c)
		}
		m.mu.Unlock()
	}
}

// settle waits until the emulator caught up with everything read so far.
func (m *mux) settle() {
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		m.qmu.Lock()
		empty := len(m.queue) == 0
		m.qmu.Unlock()
		if empty {
			time.Sleep(50 * time.Millisecond)
			m.qmu.Lock()
			empty = len(m.queue) == 0
			m.qmu.Unlock()
			if empty {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (m *mux) screen() string {
	m.settle()
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	for y := 0; y < termRows; y++ {
		var line strings.Builder
		for x := 0; x < termCols; x++ {
			c := m.emu.CellAt(x, y)
			if c == nil || c.Content == "" {
				if c != nil && c.Width == 0 {
					continue
				}
				line.WriteByte(' ')
				continue
			}
			line.WriteString(c.Content)
		}
		b.WriteString(strings.TrimRight(line.String(), " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// send writes to the PTY and returns when the write was done.
func (m *mux) send(s string) time.Time {
	_, _ = io.WriteString(m.pt, s)
	return time.Now()
}

// waitText polls the settled screen until text is on it or the deadline
// hits; the returned time is when the text was seen.
func (m *mux) waitText(timeout time.Duration, text string) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if strings.Contains(m.screen(), text) {
			return time.Now(), true
		}
		if time.Now().After(deadline) {
			return time.Time{}, false
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// childEnv is the launch environment: pane-sentinels stripped so the client
// does not think it is nested, plus the deterministic shell setup.
func childEnv(zdot, stateDir string) []string {
	drop := map[string]bool{
		"GOTERRACE": true, "GOTERRACE_PANE_ID": true,
		"GTX_SOCKET_PATH": true, "GTX_PANE_ID": true, "GTX_STATE_DIR": true,
		"TERM": true, "SHELL": true, "ZDOTDIR": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		k, _, _ := strings.Cut(kv, "=")
		if drop[k] || strings.HasPrefix(k, "HERDR_") {
			continue
		}
		env = append(env, kv)
	}
	return append(env,
		"TERM=xterm-256color",
		"SHELL=/bin/zsh",
		"ZDOTDIR="+zdot,
		"GTX_STATE_DIR="+stateDir,
	)
}

// waitFileMtime waits for path to exist with content and returns its mtime
// — the moment python finished writing the flood's last byte.
func waitFileMtime(path string, timeout time.Duration) (time.Time, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if st, err := os.Stat(path); err == nil && st.Size() > 0 {
			return st.ModTime(), true
		}
		if time.Now().After(deadline) {
			return time.Time{}, false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// procInfo is one row of ps output.
type procInfo struct {
	ppid   int
	rssKB  int64
	cpuSec float64
	comm   string
}

func psTable() map[int]procInfo {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=,time=,comm=").Output()
	if err != nil {
		return nil
	}
	tbl := map[int]procInfo{}
	for _, ln := range strings.Split(string(out), "\n") {
		f := strings.Fields(ln)
		if len(f) < 5 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		rss, err3 := strconv.ParseInt(f[2], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		tbl[pid] = procInfo{ppid: ppid, rssKB: rss, cpuSec: parseCPUTime(f[3]), comm: strings.Join(f[4:], " ")}
	}
	return tbl
}

// parseCPUTime reads ps's [[hh:]mm:]ss.frac cumulative CPU time.
func parseCPUTime(s string) float64 {
	parts := strings.Split(s, ":")
	sec, mult := 0.0, 1.0
	for i := len(parts) - 1; i >= 0; i-- {
		v, _ := strconv.ParseFloat(parts[i], 64)
		sec += v * mult
		mult *= 60
	}
	return sec
}

// gtxPids returns root plus every descendant whose executable is the gtx
// binary (the spawned `gtx server`). Pane shells and python are not gtx
// processes.
func gtxPids(tbl map[int]procInfo, root int, bins map[string]bool) map[int]bool {
	children := map[int][]int{}
	for pid, pi := range tbl {
		children[pi.ppid] = append(children[pi.ppid], pid)
	}
	res := map[int]bool{root: true}
	queue := []int{root}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range children[p] {
			queue = append(queue, c)
			if bins[tbl[c].comm] {
				res[c] = true
			}
		}
	}
	return res
}

// sampler polls ps during a run and accumulates the per-pid max RSS and
// latest cumulative CPU for the gtx processes under root.
type sampler struct {
	root   int
	bins   map[string]bool
	done   chan struct{}
	wg     sync.WaitGroup
	mu     sync.Mutex
	maxRSS map[int]int64
	cpu    map[int]float64
}

func startSampler(root int, bins map[string]bool) *sampler {
	s := &sampler{root: root, bins: bins, done: make(chan struct{}), maxRSS: map[int]int64{}, cpu: map[int]float64{}}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			select {
			case <-s.done:
				return
			default:
			}
			s.sample()
			time.Sleep(100 * time.Millisecond)
		}
	}()
	return s
}

func (s *sampler) sample() {
	tbl := psTable()
	if tbl == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for pid := range gtxPids(tbl, s.root, s.bins) {
		pi := tbl[pid]
		if pi.rssKB > s.maxRSS[pid] {
			s.maxRSS[pid] = pi.rssKB
		}
		s.cpu[pid] = pi.cpuSec
	}
}

// finish stops sampling, takes one last snapshot, and returns the max RSS
// across gtx processes, the summed CPU time, and the pids seen.
func (s *sampler) finish() (rssKB int64, cpuSec float64, procs string) {
	close(s.done)
	s.wg.Wait()
	s.sample()
	s.mu.Lock()
	defer s.mu.Unlock()
	var pids []int
	for pid, rss := range s.maxRSS {
		pids = append(pids, pid)
		if rss > rssKB {
			rssKB = rss
		}
	}
	for _, c := range s.cpu {
		cpuSec += c
	}
	sort.Ints(pids)
	ss := make([]string, len(pids))
	for i, p := range pids {
		ss[i] = strconv.Itoa(p)
	}
	return rssKB, cpuSec, strings.Join(ss, "+")
}

// load1 is the 1-minute load average via sysctl (macOS).
func load1() float64 {
	out, err := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	if err != nil {
		return -1
	}
	f := strings.Fields(strings.Trim(string(out), "{} \t\n"))
	if len(f) == 0 {
		return -1
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil {
		return -1
	}
	return v
}

func median(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	n := len(s)
	if n%2 == 1 {
		return s[n/2]
	}
	return (s[n/2-1] + s[n/2]) / 2
}

func maxOf(v []float64) float64 {
	m := v[0]
	for _, x := range v[1:] {
		if x > m {
			m = x
		}
	}
	return m
}

func reportTable(title string, runs []result) {
	rprintf("## %s", title)
	rprintf("%-4s %-6s %-9s %-9s %-8s %-9s %-8s %s", "run", "load1", "screen_s", "lag_s", "cpu_s", "maxrss_mb", "ctrlc_s", "procs")
	screen, lag, cpu, rss, ctrlc := cols(runs)
	for i, r := range runs {
		mark := ""
		if r.err != nil {
			mark = " ERR:" + r.err.Error()
		}
		rprintf("%-4d %-6.2f %-9.3f %-9.3f %-8.3f %-9.1f %-8.3f %s%s",
			i+1, r.load, r.screenSec, r.lagSec, r.cpuSec, float64(r.rssKB)/1024, r.ctrlcSec, r.procs, mark)
	}
	rprintf("%-4s %-6s %-9.3f %-9.3f %-8.3f %-9.1f %-8.3f",
		"med", "", median(screen), median(lag), median(cpu), median(rss)/1024, median(ctrlc))
	rprintf("%-4s %-6s %-9s %-9s %-8s %-9.1f %-8.3f",
		"max", "", "", "", "", maxOf(rss)/1024, maxOf(ctrlc))
}

// cols extracts the metric columns from a run set.
func cols(runs []result) (screen, lag, cpu, rss, ctrlc []float64) {
	for _, r := range runs {
		screen = append(screen, r.screenSec)
		lag = append(lag, r.lagSec)
		cpu = append(cpu, r.cpuSec)
		rss = append(rss, float64(r.rssKB))
		ctrlc = append(ctrlc, r.ctrlcSec)
	}
	return
}

func judgeAbsolute(t *testing.T, mode string, runs []result) {
	bad := false
	var loads []float64
	for i, r := range runs {
		loads = append(loads, r.load)
		if r.err != nil {
			t.Errorf("mode=%s run %d: %v", mode, i+1, r.err)
			bad = true
		}
	}
	if bad {
		return
	}
	if hi := maxOf(loads); hi >= holdLoad {
		rprintf("判定(絶対値 mode=%s): 保留 — 計測開始時の負荷 %.2f >= %.0f", mode, hi, holdLoad)
		return
	}
	screen, lag, cpu, rss, ctrlc := cols(runs)
	check(t, mode, "画面に出るまで", median(screen), limitScreenSec)
	check(t, mode, "pythonの終了からの遅れ", median(lag), limitLagSec)
	check(t, mode, "CPU時間", median(cpu), limitCPUSec)
	check(t, mode, "メモリ(MB)", median(rss)/1024, float64(limitRSSKB)/1024)
	check(t, mode, "Ctrl-Cの反応", median(ctrlc), limitIntSec)
}

func judgeRelative(t *testing.T, mode string, cur, base []result) {
	bad := false
	for tag, runs := range map[string][]result{"cur": cur, "base": base} {
		for i, r := range runs {
			if r.err != nil {
				t.Errorf("mode=%s %s run %d: %v", mode, tag, i+1, r.err)
				bad = true
			}
		}
	}
	if bad {
		return
	}
	curScreen, _, curCPU, curRSS, curInt := cols(cur)
	baseScreen, _, baseCPU, baseRSS, _ := cols(base)
	checkRel(t, mode, "画面に出るまで", median(curScreen), median(baseScreen), relScreenCPU)
	checkRel(t, mode, "CPU時間", median(curCPU), median(baseCPU), relScreenCPU)
	checkRel(t, mode, "メモリ(MB)", maxOf(curRSS)/1024, maxOf(baseRSS)/1024, relRSS)
	// Ctrl-C: 変更後の最大 <= 0.1秒 (絶対値)
	check(t, mode, "Ctrl-Cの反応(変更後の最大)", maxOf(curInt), relIntSec)
}

func check(t *testing.T, mode, name string, got, limit float64) {
	ok := got <= limit
	verdict := "PASS"
	if !ok {
		verdict = "FAIL"
	}
	rprintf("判定(mode=%s %s): %.3f <= %.3f %s", mode, name, got, limit, verdict)
	if !ok {
		t.Errorf("mode=%s %s: %.3f > limit %.3f", mode, name, got, limit)
	}
}

func checkRel(t *testing.T, mode, name string, cur, base, factor float64) {
	limit := base * factor
	ok := cur <= limit
	verdict := "PASS"
	if !ok {
		verdict = "FAIL"
	}
	rprintf("判定(相対 mode=%s %s): cur %.3f <= base %.3f x %.2f (= %.3f) %s", mode, name, cur, base, factor, limit, verdict)
	if !ok {
		t.Errorf("mode=%s %s: cur %.3f > base %.3f x %.2f", mode, name, cur, base, factor)
	}
}
