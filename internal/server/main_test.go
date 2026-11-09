package server

import (
	"context"
	"flag"
	"fmt"
	"github.com/kazuph/ikesu/internal/teststate"

	"os"
	"os/exec"

	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

var earlyIsolatedOnce sync.Once
var earlyIsolatedSlots chan struct{}
var earlyIsolatedWait sync.WaitGroup

// runIsolatedParallel gives slow real-server tests their own process-wide
// environment while retaining the original test and its real timeouts.
func runIsolatedParallel(t *testing.T) bool {
	return runIsolatedParallelNamed(t, t.Name())
}

func runIsolatedParallelEarly(t *testing.T) bool {
	// Filtered or shuffled runs do not guarantee that the final barrier runs.
	// Use the ordinary Go parallel slot for those runs.
	if flag.Lookup("test.run").Value.String() != "" || flag.Lookup("test.skip").Value.String() != "" || flag.Lookup("test.shuffle").Value.String() != "off" || flag.Lookup("test.parallel").Value.String() == "1" {
		return runIsolatedParallel(t)
	}
	return runIsolatedParallelMode(t, t.Name(), true)
}

func runIsolatedParallelNamed(t *testing.T, name string) bool {
	return runIsolatedParallelMode(t, name, false)
}

func runIsolatedParallelMode(t *testing.T, name string, early bool) bool {
	t.Helper()
	const marker = "GO_TEST_SERVER_ISOLATED"
	if os.Getenv(marker) == t.Name() {
		return false
	}
	if early && flag.Lookup("test.parallel").Value.String() == "1" {
		early = false
	}
	if !early {
		t.Parallel()
	} else {
		earlyIsolatedOnce.Do(func() {
			parallel := flag.Lookup("test.parallel")
			if parallel == nil {
				t.Fatal("test.parallel is unavailable")
			}
			limit, err := strconv.Atoi(parallel.Value.String())
			if err != nil || limit < 1 {
				t.Fatalf("invalid test.parallel: %q", parallel.Value.String())
			}
			// One Go test is executing serially while these children run.
			earlyIsolatedSlots = make(chan struct{}, limit-1)
		})
		earlyIsolatedSlots <- struct{}{}
		earlyIsolatedWait.Add(1)
	}
	ctx := t.Context()
	args := []string{"-test.run=" + isolatedRunPattern(flag.Lookup("test.run").Value.String(), t.Name(), name), "-test.skip=" + flag.Lookup("test.skip").Value.String(), "-test.v"}
	if deadline, ok := t.Deadline(); ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadline(ctx, deadline)
		defer cancel()
		args = append(args, "-test.timeout="+time.Until(deadline).String())
	}
	cmd := exec.CommandContext(ctx, os.Args[0], args...)
	cmd.Env = append(os.Environ(), marker+"="+t.Name())
	type result struct {
		output []byte
		err    error
		start  time.Time
		end    time.Time
	}
	finished := make(chan result, 1)
	go func() {
		if early {
			defer func() { <-earlyIsolatedSlots; earlyIsolatedWait.Done() }()
		}
		start := time.Now()
		output, err := cmd.CombinedOutput()
		finished <- result{output: output, err: err, start: start, end: time.Now()}
	}()
	if early {
		// Keep real deadlines while the independent child overlaps serial tests.
		t.Parallel()
	}
	got := <-finished
	output, err := got.output, got.err
	t.Logf("isolated child window: %s %s early=%t", got.start.Format(time.RFC3339Nano), got.end.Format(time.RFC3339Nano), early)
	t.Logf("isolated test:\n%s", output)
	if err != nil {
		t.Fatalf("isolated test failed: %v", err)
	}
	quoted := regexp.QuoteMeta(t.Name())
	if !regexp.MustCompile(`(?m)^=== RUN   `+quoted+`\r?$`).Match(output) ||
		!regexp.MustCompile(`(?m)^--- PASS: `+quoted+` \([^\r\n]*\)\r?$`).Match(output) {
		t.Fatalf("isolated test did not report PASS: %s", t.Name())
	}
	return true
}

// Preserve the selected subtest paths while restricting each alternative to
// the top-level test that launched this child.
func isolatedRunPattern(pattern, parent, child string) string {
	anchored := "^" + regexp.QuoteMeta(child) + "$"
	if pattern == "" {
		return anchored
	}
	var alternatives []string
	start, slash, brackets, parens := 0, -1, 0, 0
	for i := 0; i <= len(pattern); i++ {
		if i == len(pattern) || (pattern[i] == '|' && brackets == 0 && parens == 0) {
			end := i
			if slash >= 0 {
				end = slash
			}
			if matched, _ := regexp.MatchString(pattern[start:end], parent); matched {
				suffix := ""
				if slash >= 0 {
					suffix = pattern[slash:i]
				}
				alternatives = append(alternatives, anchored+suffix)
			}
			start, slash = i+1, -1
			continue
		}
		switch pattern[i] {
		case '\\':
			i++
		case '[':
			brackets++
		case ']':
			if brackets > 0 {
				brackets--
			}
		case '(':
			if brackets == 0 {
				parens++
			}
		case ')':
			if brackets == 0 {
				parens--
			}
		case '/':
			if brackets == 0 && parens == 0 && slash < 0 {
				slash = i
			}
		}
	}
	return strings.Join(alternatives, "|")
}

func TestMain(m *testing.M) {
	if len(os.Args) == 2 && os.Args[1] == jobRunnerArg {
		code, err := RunJobRunner()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ikesu job runner:", err)
		}
		os.Exit(code)
	}
	if len(os.Args) == 2 && os.Args[1] == jobSupervisorArg {
		code, _ := RunJobSupervisor()
		os.Exit(code)
	}
	if os.Getenv("IKESU_EMFILE_CHILD") != "" {
		emfileChild()
	}
	os.Exit(teststate.Run(m.Run))
}
