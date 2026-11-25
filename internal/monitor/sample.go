// Package monitor samples and evaluates process usage within managed pane trees.
package monitor

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/childenv"
)

type Identity struct {
	PID   int
	Start string
}

type Roots map[int]Identity // pane ID → process identity

type Process struct {
	Identity
	Parent int
	Group  int
	Pane   int
	RSS    int64 // KiB
	Name   string
	CPU    time.Duration // cumulative CPU time
}

// Sample reads process metrics and comm in one invocation. It never reads argv.
func Sample(ctx context.Context, roots Roots) ([]Process, error) {
	if len(roots) == 0 {
		return nil, nil
	}
	read, err := sampleReader()
	if err != nil {
		return nil, err
	}
	cmd := childenv.CommandContext(ctx, "/bin/ps", "-axo", "pid=,ppid=,rss=,time=,comm=")
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	raw, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	rows, err := parseSample(raw)
	if err != nil {
		return nil, err
	}
	return managed(rows, roots, read), nil
}

func parseSample(raw []byte) ([]Process, error) {
	var rows []Process
	scan := bufio.NewScanner(bytes.NewReader(raw))
	for scan.Scan() {
		f := strings.Fields(scan.Text())
		if len(f) < 4 {
			return nil, errors.New("invalid process sample")
		}
		pid, e1 := strconv.Atoi(f[0])
		parent, e2 := strconv.Atoi(f[1])
		rss, e3 := strconv.ParseInt(f[2], 10, 64)
		cpu, e4 := parseCPUTime(f[3])
		if pid < 0 || parent < 0 || rss < 0 || e1 != nil || e2 != nil || e3 != nil || e4 != nil {
			return nil, fmt.Errorf("invalid numeric process sample: %q", f)
		}
		if pid > 1 {
			rows = append(rows, Process{Identity: Identity{PID: pid}, Parent: parent, RSS: rss, CPU: cpu, Name: strings.Join(f[4:], " ")})
		}
	}
	return rows, scan.Err()
}

// Darwin reports m:ss.dd (or h:mm:ss.dd), Linux [d-]hh:mm:ss.
func parseCPUTime(value string) (time.Duration, error) {
	var elapsed time.Duration
	if days, rest, ok := strings.Cut(value, "-"); ok {
		n, err := strconv.Atoi(days)
		if err != nil {
			return 0, err
		}
		elapsed = time.Duration(n) * 24 * time.Hour
		value = rest
	}
	parts := strings.Split(value, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, errors.New("invalid CPU time")
	}
	unit := time.Second
	for i := len(parts) - 1; i >= 0; i-- {
		n, err := strconv.ParseFloat(parts[i], 64)
		if err != nil {
			return 0, err
		}
		elapsed += time.Duration(n * float64(unit))
		unit *= 60
	}
	return elapsed, nil
}

type readProcess func(int) (Process, error)
type readSample func(Process) (Process, error)

func managed(rows []Process, roots Roots, read readSample) []Process {
	children := make(map[int][]int)
	byPID := make(map[int]Process, len(rows))
	rootPIDs := make(map[int]bool, len(roots))
	for _, row := range rows {
		children[row.Parent] = append(children[row.Parent], row.PID)
		byPID[row.PID] = row
	}
	for _, root := range roots {
		rootPIDs[root.PID] = true
	}
	var result []Process
	visited := make(map[int]bool)
	for pane, root := range roots {
		queue := []int{root.PID}
		for len(queue) > 0 {
			pid := queue[0]
			queue = queue[1:]
			row, ok := byPID[pid]
			if !ok || visited[pid] {
				continue
			}
			visited[pid] = true
			current, err := read(row)
			if err != nil || current.PID != pid || current.Start == "" || current.Parent != row.Parent || pid == root.PID && current.Start != root.Start {
				continue
			}
			current.Pane, current.Name = pane, row.Name
			result = append(result, current)
			for _, child := range children[pid] {
				if !rootPIDs[child] {
					queue = append(queue, child)
				}
			}
		}
	}
	return result
}

// bracketSample accepts counters only when the same process and parent
// were observed on both sides of the numeric sample.
func bracketSample(before map[int]Process, read readProcess) readSample {
	return func(row Process) (Process, error) {
		prior := before[row.PID]
		current, err := read(row.PID)
		if err != nil || prior.PID != row.PID || prior.Identity != current.Identity ||
			prior.Parent != row.Parent || current.Parent != row.Parent {
			return Process{}, errOwner
		}
		current.RSS, current.CPU = row.RSS, row.CPU
		return current, nil
	}
}
