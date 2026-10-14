package machine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// SyncProgress observes exit-zero stage boundaries on private descriptors.
// Each stage's stderr is drained before acknowledging success, so a late URL
// cannot recreate a record after that stage has already connected.
type SyncProgress struct {
	Child       []*os.File
	status, ack *os.File
	reads       []*os.File
	drained     []chan error
	done        chan error
	start       sync.Once
	state       *RunApproval
}

func (s *RunApproval) SyncProgress(argv []string) ([]string, *SyncProgress, error) {
	stages, ok := syncStages(argv)
	if !ok {
		return argv, nil, nil
	}
	reader, status, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	ack, writer, err := os.Pipe()
	if err != nil {
		reader.Close()
		status.Close()
		return nil, nil, err
	}
	p := &SyncProgress{Child: []*os.File{status, ack}, status: reader, ack: writer, done: make(chan error, 1), state: s}
	var commands []string
	for i, stage := range stages {
		if i == len(stages)-1 {
			commands = append(commands, "exec "+Command(stage))
			break
		}
		r, w, err := os.Pipe()
		if err != nil {
			for _, f := range append(append(p.Child, p.reads...), reader, writer) {
				f.Close()
			}
			return nil, nil, err
		}
		fd := 3 + len(p.Child)
		p.Child = append(p.Child, w)
		p.reads = append(p.reads, r)
		p.drained = append(p.drained, make(chan error, 1))
		commands = append(commands, fmt.Sprintf("%s 2>&%d %d>&-", Command(stage), fd, fd), fmt.Sprintf("{ exec %d>&-; printf '\\n' >&3 && IFS= read -r _gtx_sync_ack <&4; }", fd))
	}
	observed := append([]string{}, argv...)
	observed[2] = strings.Join(commands, " && ")
	return observed, p, nil
}

func (p *SyncProgress) Started(stderr io.Writer) {
	p.start.Do(func() {
		for i, reader := range p.reads {
			done := p.drained[i]
			go func() { _, err := io.Copy(stderr, reader); reader.Close(); done <- err }()
		}
		go p.observe()
		for _, file := range p.Child {
			file.Close()
		}
	})
}

func (p *SyncProgress) observe() {
	defer p.status.Close()
	defer p.ack.Close()
	scanner := bufio.NewScanner(p.status)
	stage := 0
	for scanner.Scan() {
		if stage >= len(p.drained) {
			p.done <- fmt.Errorf("unexpected sync stage completion")
			return
		}
		err := <-p.drained[stage]
		p.drained[stage] <- err
		stage++
		if err != nil {
			p.done <- err
			return
		}
		p.state.mu.Lock()
		if p.state.timer != nil {
			p.state.timer.Stop()
		}
		p.state.awaiting = false
		err = ClearApproval(p.state.ID)
		p.state.mu.Unlock()
		if err != nil {
			p.done <- err
			return
		}
		if _, err = fmt.Fprintln(p.ack); err != nil {
			p.done <- err
			return
		}
	}
	p.done <- scanner.Err()
}

func (p *SyncProgress) Close() error {
	p.Started(io.Discard)
	p.status.Close()
	p.ack.Close()
	err := <-p.done
	for _, done := range p.drained {
		if readErr := <-done; err == nil {
			err = readErr
		}
	}
	return err
}
