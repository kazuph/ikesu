package session

import (
	"bufio"
	"crypto/sha256"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type ClaudeQuestion struct {
	SessionID  string
	ToolID     string
	Index      int
	Header     string
	Question   string
	Multi      bool
	Answered   bool
	StartedAt  time.Time
	AnsweredAt time.Time
	Options    []ClaudeQuestionOption
}

type ClaudeQuestionOption struct{ Label, Description string }

// PendingClaudeQuestions refuses ambiguous files and incomplete records.
func PendingClaudeQuestions(root, sessionID string) ([]ClaudeQuestion, bool) {
	return new(ClaudeQuestionCache).Read(root, sessionID)
}

// ClaudeQuestionCache is owned by one scanner; callers must not share it across goroutines.
type ClaudeQuestionCache struct {
	path      string
	sessionID string
	size      int64
	modified  time.Time
	questions []ClaudeQuestion
	valid     bool
	bytesRead int64
	fileInfo  os.FileInfo
	offset    int64
	anchor    [sha256.Size]byte
	fullAt    time.Time
	pending   map[string][]ClaudeQuestion
	answered  map[string]time.Time
	matched   bool
	// noPending: the last ReadAll read the session and found no unanswered question.
	noPending bool
}

const transcriptAnchorBytes int64 = 4096
const transcriptFullReadInterval = 60 * time.Second

func transcriptAnchor(f *os.File, offset int64) ([sha256.Size]byte, int64, error) {
	start := max(int64(0), offset-transcriptAnchorBytes)
	b := make([]byte, offset-start)
	n, err := f.ReadAt(b, start)
	if err != nil && !(err == io.EOF && n == len(b)) {
		return [sha256.Size]byte{}, int64(n), err
	}
	return sha256.Sum256(b), int64(n), nil
}

type countingTranscriptReader struct {
	r io.Reader
	n int64
}

func (r *countingTranscriptReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.n += int64(n)
	return n, err
}

func (cache *ClaudeQuestionCache) Read(root, sessionID string) ([]ClaudeQuestion, bool) {
	all, ok := cache.ReadAll(root, sessionID)
	if !ok {
		return nil, false
	}
	var pending []ClaudeQuestion
	for _, q := range all {
		if !q.Answered {
			pending = append(pending, q)
		}
	}
	return pending, true
}

// LastReadHadNoPending reports whether the last ReadAll read this session's
// transcript and found no unanswered question.
func (cache *ClaudeQuestionCache) LastReadHadNoPending() bool { return cache.noPending }

func (cache *ClaudeQuestionCache) ReadAll(root, sessionID string) ([]ClaudeQuestion, bool) {
	cache.noPending = false
	questions, ok := cache.readAll(root, sessionID)
	if ok {
		cache.noPending = true
		for _, q := range questions {
			if !q.Answered {
				cache.noPending = false
				break
			}
		}
	}
	return questions, ok
}

func (cache *ClaudeQuestionCache) readAll(root, sessionID string) ([]ClaudeQuestion, bool) {
	if !SafeSessionID(sessionID) {
		return nil, false
	}
	var paths []string
	projects := filepath.Join(root, "projects")
	if cache.valid && cache.sessionID == sessionID && strings.HasPrefix(cache.path, projects+string(filepath.Separator)) && time.Since(cache.fullAt) < transcriptFullReadInterval {
		// Between full reads, reuse the found path instead of walking every
		// project directory (thousands of files) on each scan.
		if _, err := os.Stat(cache.path); err == nil {
			paths = []string{cache.path}
		}
	}
	var err error
	if paths == nil {
		err = filepath.WalkDir(projects, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !entry.IsDir() && entry.Name() == sessionID+".jsonl" {
				paths = append(paths, path)
			}
			return nil
		})
	}
	if err != nil || len(paths) != 1 {
		return nil, false
	}
	info, err := os.Stat(paths[0])
	if err != nil {
		return nil, false
	}
	sameFile := cache.valid && cache.path == paths[0] && cache.sessionID == sessionID && os.SameFile(cache.fileInfo, info)
	fullDue := time.Since(cache.fullAt) >= transcriptFullReadInterval
	if sameFile && !fullDue && cache.size == info.Size() && cache.modified.Equal(info.ModTime()) {
		cache.bytesRead = 0
		return cache.questions, true
	}
	f, err := os.Open(paths[0])
	if err != nil {
		return nil, false
	}
	defer f.Close()
	openedInfo, err := f.Stat()
	if err != nil || !os.SameFile(info, openedInfo) {
		return nil, false
	}
	var anchorBytes int64
	incremental := sameFile && !fullDue && info.Size() > cache.size
	if incremental {
		anchor, n, err := transcriptAnchor(f, cache.offset)
		anchorBytes += n
		incremental = err == nil && anchor == cache.anchor
	}
	offset := int64(0)
	if incremental {
		offset = cache.offset
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, false
	}
	type option struct {
		Label       string `json:"label"`
		Description string `json:"description"`
	}
	type question struct {
		Header   string   `json:"header"`
		Question string   `json:"question"`
		Multi    bool     `json:"multiSelect"`
		Options  []option `json:"options"`
	}
	type block struct {
		Type      string          `json:"type"`
		Name      string          `json:"name"`
		ID        string          `json:"id"`
		ToolUseID string          `json:"tool_use_id"`
		Input     json.RawMessage `json:"input"`
	}
	type row struct {
		SessionID string `json:"sessionId"`
		Timestamp string `json:"timestamp"`
		Message   struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	pending := map[string][]ClaudeQuestion{}
	answered := map[string]time.Time{}
	matched := false
	if incremental {
		for id, questions := range cache.pending {
			pending[id] = questions
		}
		for id, value := range cache.answered {
			answered[id] = value
		}
		matched = cache.matched
	}
	counted := &countingTranscriptReader{r: f}
	reader := bufio.NewReader(counted)
	for {
		line, err := reader.ReadBytes('\n')
		if err == io.EOF {
			break
		} // A partial final line is never authoritative.
		if err != nil {
			return nil, false
		}
		offset += int64(len(line))
		var record row
		if json.Unmarshal(line, &record) != nil {
			return nil, false
		}
		if record.SessionID != "" && record.SessionID != sessionID {
			return nil, false
		}
		if record.SessionID == sessionID {
			matched = true
		}
		if len(record.Message.Content) == 0 || !strings.HasPrefix(strings.TrimSpace(string(record.Message.Content)), "[") {
			continue
		}
		var blocks []block
		if json.Unmarshal(record.Message.Content, &blocks) != nil {
			return nil, false
		}
		for _, b := range blocks {
			at, _ := time.Parse(time.RFC3339Nano, record.Timestamp)
			if b.Type == "tool_result" && b.ToolUseID != "" {
				answered[b.ToolUseID] = at
			}
			if b.Type != "tool_use" || b.Name != "AskUserQuestion" || b.ID == "" {
				continue
			}
			var input struct {
				Questions []question `json:"questions"`
			}
			if json.Unmarshal(b.Input, &input) != nil {
				return nil, false
			}
			qs := make([]ClaudeQuestion, 0, len(input.Questions))
			for index, q := range input.Questions {
				item := ClaudeQuestion{SessionID: sessionID, ToolID: b.ID, Index: index, Header: q.Header, Question: q.Question, Multi: q.Multi, StartedAt: at}
				for _, o := range q.Options {
					item.Options = append(item.Options, ClaudeQuestionOption{Label: o.Label, Description: o.Description})
				}
				qs = append(qs, item)
			}
			pending[b.ID] = qs
		}
	}
	var out []ClaudeQuestion
	for id, questions := range pending {
		for _, q := range questions {
			q.AnsweredAt, q.Answered = answered[id]
			out = append(out, q)
		}
	}
	if matched {
		anchor, n, err := transcriptAnchor(f, offset)
		anchorBytes += n
		if err != nil {
			return nil, false
		}
		cache.path, cache.sessionID, cache.size, cache.modified, cache.questions, cache.valid = paths[0], sessionID, info.Size(), info.ModTime(), out, true
		cache.fileInfo, cache.offset, cache.anchor = info, offset, anchor
		cache.pending, cache.answered, cache.matched = pending, answered, matched
		if !incremental {
			cache.fullAt = time.Now()
		}
		cache.bytesRead = counted.n + anchorBytes
	}
	return out, matched
}
