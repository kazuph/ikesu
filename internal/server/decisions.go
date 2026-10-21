package server

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"sync"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/dispatch"
)

// The app loop owns records, expiry scheduling and notification order.
// Waiters observe a replace-on-change channel, without polling SQLite.
type decisionHub struct {
	boot     string
	revision uint64
	changed  chan struct{}
	timer    *time.Timer
	close    sync.Once
	stopped  chan struct{}
}

func (s *Server) initDecisionHub() {
	if s.decisions.changed == nil {
		s.decisions.changed = make(chan struct{})
		s.decisions.stopped = make(chan struct{})
	}
}
func (s *Server) stopDecisions() {
	s.stopScreenPromptTimer()
	s.decisions.close.Do(func() {
		if s.decisions.stopped != nil {
			close(s.decisions.stopped)
		}
	})
	if s.decisions.timer != nil {
		s.decisions.timer.Stop()
	}
}
func decisionFailure(id string, err error) api.Response {
	var e *api.Error
	if errors.As(err, &e) {
		return api.Failure(id, e.Code, e.Message)
	}
	return api.Failure(id, "decision_store_error", err.Error())
}
func decisionResult(id string, d api.Decision) api.Response {
	return api.Success(id, api.DecisionResult{Type: "decision", Decision: d})
}
func (s *Server) decisionChanged(kind string, d api.Decision) {
	s.syncDecisionAttention()
	close(s.decisions.changed)
	s.decisions.changed = make(chan struct{})
	s.emitEvent(kind, d)
	s.pushDecisionProjection()
}
func (s *Server) expireDecisions(store *dispatch.Store) error {
	pending, err := store.Decisions("pending")
	if err != nil {
		return err
	}
	if s.decisions.timer != nil {
		s.decisions.timer.Stop()
		s.decisions.timer = nil
	}
	now := time.Now()
	var next *int64
	for _, d := range pending {
		if d.ExpiresUnixMS == nil {
			continue
		}
		if *d.ExpiresUnixMS <= now.UnixMilli() {
			expired, err := store.ExpireDecision(d.DecisionID, now)
			if err != nil {
				return err
			}
			s.decisionChanged("decision_resolved", expired)
		} else if next == nil || *d.ExpiresUnixMS < *next {
			v := *d.ExpiresUnixMS
			next = &v
		}
	}
	if next != nil {
		s.decisions.timer = time.AfterFunc(time.Until(time.UnixMilli(*next)), func() {
			select {
			case <-s.decisions.stopped:
				return
			default:
			}
			s.app.Do(func() {
				select {
				case <-s.decisions.stopped:
					return
				default:
				}
				if err := s.expireDecisions(store); err != nil {
					s.app.SetConfigWarning("decision expiry: " + err.Error())
				}
			})
		})
	}
	return nil
}
func (s *Server) decisionRequest(req *api.Request) api.Response {
	s.initDecisionHub()
	store, err := s.db()
	if err != nil {
		return decisionFailure(req.ID, err)
	}
	if err := s.expireDecisions(store); err != nil {
		return decisionFailure(req.ID, err)
	}
	switch req.Method {
	case api.MethodDecisionCreate:
		var p api.DecisionCreateParams
		if err := req.DecodeParams(&p); err != nil {
			return decisionFailure(req.ID, &api.Error{Code: api.ErrInvalidRequest, Message: err.Error()})
		}
		s.completeDecisionOrigin(p.Origin)
		d, err := store.CreateDecision(p, time.Now())
		if err != nil {
			return decisionFailure(req.ID, err)
		}
		s.decisionChanged("decision_created", d)
		if err := s.expireDecisions(store); err != nil {
			return decisionFailure(req.ID, err)
		}
		return decisionResult(req.ID, d)
	case api.MethodDecisionList:
		var p api.DecisionListParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		ds, err := store.Decisions(p.Status)
		if err != nil {
			return decisionFailure(req.ID, err)
		}
		return api.Success(req.ID, api.DecisionListResult{Type: "decision_list", Decisions: ds})
	case api.MethodDecisionAnswer:
		var p api.DecisionAnswerParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		before, err := store.Decision(p.DecisionID)
		if err != nil {
			return decisionFailure(req.ID, err)
		}
		watch, pid, err := s.validateScreenAnswer(before, p)
		if err != nil {
			return decisionFailure(req.ID, err)
		}
		d, err := store.AnswerDecision(p, time.Now())
		if err != nil {
			return decisionFailure(req.ID, err)
		}
		s.decisionChanged("decision_resolved", d)
		if watch != nil {
			s.startScreenAnswer(pid, watch, *d.Answer)
		}
		_ = s.expireDecisions(store)
		return decisionResult(req.ID, d)
	case api.MethodDecisionGet, api.MethodDecisionCancel:
		var p api.DecisionTargetParams
		if err := req.DecodeParams(&p); err != nil {
			return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
		}
		var d api.Decision
		if req.Method == api.MethodDecisionCancel {
			d, err = store.CancelDecision(p.DecisionID, time.Now())
		} else {
			d, err = store.Decision(p.DecisionID)
		}
		if err != nil {
			return decisionFailure(req.ID, err)
		}
		if req.Method == api.MethodDecisionCancel {
			s.decisionChanged("decision_resolved", d)
			_ = s.expireDecisions(store)
		}
		return decisionResult(req.ID, d)
	}
	return api.Failure(req.ID, api.ErrUnknownMethod, "unknown decision method")
}
func (s *Server) serveDecisionWait(conn net.Conn, req *api.Request) {
	var p api.DecisionWaitParams
	if err := req.DecodeParams(&p); err != nil {
		_ = api.WriteResponse(conn, api.Failure(req.ID, api.ErrInvalidRequest, err.Error()))
		return
	}
	if p.TimeoutMS != nil && *p.TimeoutMS > uint64(math.MaxInt64/int64(time.Millisecond)) {
		_ = api.WriteResponse(conn, api.Failure(req.ID, api.ErrInvalidRequest, "timeout_ms is too large"))
		return
	}
	done := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, conn); close(done) }()
	var timeout <-chan time.Time
	if p.TimeoutMS != nil {
		timer := time.NewTimer(time.Duration(*p.TimeoutMS) * time.Millisecond)
		defer timer.Stop()
		timeout = timer.C
	}
	for {
		var resp api.Response
		var changed, stopped <-chan struct{}
		s.app.Do(func() {
			resp = s.decisionRequest(&api.Request{ID: req.ID, Method: api.MethodDecisionGet, Params: api.MarshalParams(api.DecisionTargetParams{DecisionID: p.DecisionID})})
			changed = s.decisions.changed
			stopped = s.decisions.stopped
		})
		// Do may skip the callback once the app stops. Close the transport
		// so the caller retries the same decision on the successor server.
		if s.closing.Load() != 0 {
			return
		}
		if resp.Err != nil {
			_ = api.WriteResponse(conn, resp)
			return
		}
		var result api.DecisionResult
		if err := json.Unmarshal(resp.Result, &result); err != nil {
			_ = api.WriteResponse(conn, api.Failure(req.ID, api.ErrInvalidRequest, err.Error()))
			return
		}
		if result.Decision.Status != "pending" {
			_ = api.WriteResponse(conn, resp)
			return
		}
		select {
		case <-done:
			return
		case <-stopped:
			return
		case <-s.sup.Done():
			return
		case <-changed:
			continue
		case <-timeout:
			s.app.Do(func() {
				resp = s.decisionRequest(&api.Request{ID: req.ID, Method: api.MethodDecisionGet, Params: api.MarshalParams(api.DecisionTargetParams{DecisionID: p.DecisionID})})
			})
			_ = api.WriteResponse(conn, resp)
			return
		}
	}
}
