//go:build benchstage

package benchstage

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"hash"
	"os"
	"path/filepath"
	"strconv"
	"sync"
)

var outputDirectory = os.Getenv("IKESU_BENCH_OUTPUT_DIR")
var outputs struct {
	sync.Mutex
	panes map[int]*outputHash
}

// Reserved markers are emitted only by the isolated correctness fixture.
// Keep marker fragments across arbitrary PTY chunk boundaries, not the stream.
const outputBegin = "IKESU_BYTE_BEGIN!"
const outputEnd = "IKESU_BYTE_END!"

type outputHash struct {
	started, done bool
	tail          []byte
	hash          hash.Hash
	count         int64
}

func (s *outputHash) consume(data []byte) bool {
	if s.done {
		return false
	}
	s.tail = append(s.tail, data...)
	if !s.started {
		i := bytes.Index(s.tail, []byte(outputBegin))
		if i < 0 {
			n := min(len(s.tail), len(outputBegin)-1)
			copy(s.tail, s.tail[len(s.tail)-n:])
			s.tail = s.tail[:n]
			return false
		}
		s.tail = s.tail[i+len(outputBegin):]
		s.started = true
		s.hash = sha256.New()
	}
	n := bytes.Index(s.tail, []byte(outputEnd))
	if n >= 0 {
		s.done = true
	} else {
		n = max(0, len(s.tail)-len(outputEnd)+1)
	}
	_, _ = s.hash.Write(s.tail[:n])
	s.count += int64(n)
	copy(s.tail, s.tail[n:])
	s.tail = s.tail[:len(s.tail)-n]
	return s.done
}

// Called after parsing. Normal builds and timing runs do not capture output.
func Output(pane int, data []byte) {
	if outputDirectory == "" {
		return
	}
	outputs.Lock()
	defer outputs.Unlock()
	if outputs.panes == nil {
		outputs.panes = make(map[int]*outputHash)
	}
	s := outputs.panes[pane]
	if s == nil {
		s = &outputHash{}
		outputs.panes[pane] = s
	}
	if !s.consume(data) {
		return
	}
	result, err := json.Marshal(struct {
		Bytes  int64  `json:"bytes"`
		SHA256 string `json:"sha256"`
	}{s.count, hex.EncodeToString(s.hash.Sum(nil))})
	if err != nil {
		panic(err)
	}
	if err = os.WriteFile(filepath.Join(outputDirectory, "p"+strconv.Itoa(pane)+".json"), result, 0600); err != nil {
		panic(err)
	}
}
