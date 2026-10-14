package dispatch

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/kazuph/goterrace/internal/api"
)

const decisionSchema = `CREATE TABLE IF NOT EXISTS decisions (
 decision_id TEXT PRIMARY KEY,
 status TEXT NOT NULL CHECK(status IN ('pending','answered','expired','cancelled')),
 created_unix_ms INTEGER NOT NULL,
 expires_unix_ms INTEGER,
 record TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_decisions_pending_deadline ON decisions(status, expires_unix_ms);`

func decisionError(code, message string) error { return &api.Error{Code: code, Message: message} }
func ValidDecisionStatus(status string) bool {
	switch status {
	case "pending", "answered", "expired", "cancelled":
		return true
	}
	return false
}
func (s *Store) CreateDecision(p api.DecisionCreateParams, now time.Time) (api.Decision, error) {
	var d api.Decision
	switch p.Kind {
	case "command_guard", "tool_permission", "agent_prompt", "ask":
	default:
		return d, decisionError(api.ErrInvalidRequest, "invalid decision kind")
	}
	if strings.TrimSpace(p.Title) == "" || len(p.Options) == 0 {
		return d, decisionError(api.ErrInvalidRequest, "title and at least one option are required")
	}
	ids := map[string]bool{}
	for _, o := range p.Options {
		if strings.TrimSpace(o.ID) == "" || strings.TrimSpace(o.Label) == "" || ids[o.ID] {
			return d, decisionError(api.ErrInvalidRequest, "option ids and labels must be nonempty, ids must be unique")
		}
		switch o.Role {
		case "approve", "reject", "other":
		default:
			return d, decisionError(api.ErrInvalidRequest, "invalid option role")
		}
		ids[o.ID] = true
	}
	ms := now.UnixMilli()
	if p.TimeoutMS != nil && (*p.TimeoutMS > uint64(math.MaxInt64) || int64(*p.TimeoutMS) > math.MaxInt64-ms) {
		return d, decisionError(api.ErrInvalidRequest, "timeout_ms is too large")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return d, err
	}
	d = api.Decision{DecisionID: "dec-" + hex.EncodeToString(id[:]), Kind: p.Kind, Title: p.Title, Body: p.Body, Options: p.Options, AllowText: p.AllowText, MultiSelect: p.MultiSelect, QuestionLabel: p.QuestionLabel, Origin: p.Origin, CreatedUnixMS: ms, Status: "pending"}
	if p.TimeoutMS != nil {
		expiry := ms + int64(*p.TimeoutMS)
		d.ExpiresUnixMS = &expiry
	}
	raw, err := json.Marshal(d)
	if err != nil {
		return d, err
	}
	err = s.exec(`INSERT INTO decisions(decision_id,status,created_unix_ms,expires_unix_ms,record) VALUES(?,?,?,?,?)`, d.DecisionID, d.Status, ms, d.ExpiresUnixMS, string(raw))
	return d, err
}
func (s *Store) decisionLocked(id string) (api.Decision, error) {
	var d api.Decision
	st, err := s.queryLocked(`SELECT record FROM decisions WHERE decision_id=?`, id)
	if err != nil {
		return d, err
	}
	defer st.Close()
	if !st.Step() {
		if err := st.Err(); err != nil {
			return d, err
		}
		return d, decisionError(api.ErrDecisionNotFound, "decision not found: "+id)
	}
	err = json.Unmarshal([]byte(st.ColumnText(0)), &d)
	return d, err
}
func (s *Store) Decision(id string) (api.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.decisionLocked(id)
}
func (s *Store) Decisions(status string) ([]api.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if status != "" && !ValidDecisionStatus(status) {
		return nil, decisionError(api.ErrInvalidRequest, "invalid decision status")
	}
	q := `SELECT record FROM decisions`
	var args []any
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, status)
	}
	q += ` ORDER BY created_unix_ms,decision_id`
	st, err := s.queryLocked(q, args...)
	if err != nil {
		return nil, err
	}
	defer st.Close()
	result := []api.Decision{}
	for st.Step() {
		var d api.Decision
		if err := json.Unmarshal([]byte(st.ColumnText(0)), &d); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, st.Err()
}
func (s *Store) resolveDecision(id, status string, p *api.DecisionAnswerParams, now time.Time) (api.Decision, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.decisionLocked(id)
	if err != nil {
		return d, err
	}
	if d.Status != "pending" {
		return d, decisionError(api.ErrDecisionResolved, "decision already resolved: "+id)
	}
	if status == "expired" && (d.ExpiresUnixMS == nil || *d.ExpiresUnixMS > now.UnixMilli()) {
		return d, fmt.Errorf("decision deadline has not passed")
	}
	if p != nil {
		answer, err := ValidateDecisionAnswer(d, *p)
		if err != nil {
			return d, err
		}
		answer.AnsweredUnixMS = now.UnixMilli()
		d.Answer = &answer
	}
	d.Status = status
	raw, err := json.Marshal(d)
	if err != nil {
		return d, err
	}
	if err = s.execLocked(`UPDATE decisions SET status=?,record=? WHERE decision_id=? AND status='pending'`, status, string(raw), id); err != nil {
		return d, err
	}
	if s.db.Changes() == 0 {
		return api.Decision{}, decisionError(api.ErrDecisionResolved, "decision already resolved: "+id)
	}
	return d, nil
}

// ValidateDecisionAnswer also lets screen answers reject invalid choices before PTY input.
func ValidateDecisionAnswer(d api.Decision, p api.DecisionAnswerParams) (api.DecisionAnswer, error) {
	answer := api.DecisionAnswer{Text: p.Text, Responder: p.Responder}
	ids := []string{}
	seen := map[string]bool{}
	choices := append([]string{}, p.OptionIDs...)
	if p.OptionID != nil {
		choices = append(choices, *p.OptionID)
	}
	for _, id := range choices {
		if seen[id] {
			continue
		}
		found := false
		for _, o := range d.Options {
			found = found || o.ID == id
		}
		if !found {
			return answer, decisionError(api.ErrInvalidRequest, "unknown decision option")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 && p.Text == nil {
		return answer, decisionError(api.ErrInvalidRequest, "option_id or text is required")
	}
	if p.Text != nil && !d.AllowText {
		return answer, decisionError(api.ErrInvalidRequest, "text is not allowed")
	}
	if len(ids) > 0 {
		answer.OptionID = &ids[0]
	}
	if len(ids) > 1 {
		answer.OptionIDs = ids
	}
	return answer, nil
}
func (s *Store) AnswerDecision(p api.DecisionAnswerParams, now time.Time) (api.Decision, error) {
	return s.resolveDecision(p.DecisionID, "answered", &p, now)
}
func (s *Store) CancelDecision(id string, now time.Time) (api.Decision, error) {
	return s.resolveDecision(id, "cancelled", nil, now)
}
func (s *Store) ExpireDecision(id string, now time.Time) (api.Decision, error) {
	return s.resolveDecision(id, "expired", nil, now)
}

// UpdatePendingScreenDecision refreshes content without changing identity, deadlines or resolution.
func (s *Store) UpdatePendingScreenDecision(id string, p api.DecisionCreateParams) (api.Decision, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, err := s.decisionLocked(id)
	if err != nil || d.Status != "pending" {
		return d, false, err
	}
	updated := d
	updated.Kind = p.Kind
	updated.Title, updated.Body, updated.Options = p.Title, p.Body, p.Options
	updated.AllowText, updated.MultiSelect, updated.QuestionLabel = p.AllowText, p.MultiSelect, p.QuestionLabel
	old, err := json.Marshal(d)
	if err != nil {
		return d, false, err
	}
	raw, err := json.Marshal(updated)
	if err != nil {
		return d, false, err
	}
	if string(raw) == string(old) {
		return d, false, nil
	}
	err = s.execLocked(`UPDATE decisions SET record=? WHERE decision_id=? AND status='pending'`, string(raw), id)
	return updated, err == nil && s.db.Changes() > 0, err
}
