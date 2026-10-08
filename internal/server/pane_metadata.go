package server

import (
	"fmt"
	"github.com/kazuph/goterrace/internal/api"
	"sort"
	"strings"
	"time"
)

type paneMetadata struct {
	Sequences     map[string]uint64
	Presentations map[string]*panePresentation
	NextOrder     uint64
	Tokens        map[string]metadataToken
}

type panePresentation struct {
	Agent, AppliesToSource string
	Title, DisplayAgent    metadataToken
	Labels                 map[string]metadataToken
	Order                  uint64
}

func (s *Server) paneMetadataView(pid int) api.PaneMetadataView {
	m := s.paneMetadata[pid]
	if m == nil {
		return api.PaneMetadataView{}
	}
	now := time.Now()
	alive := func(v metadataToken) bool { return v.Expires.IsZero() || now.Before(v.Expires) }
	agent, _, _ := s.app.Reported(pid)
	if agent == "" {
		if p := s.app.Supervisor().Get(pid); p != nil {
			agent = p.Snapshot().AgentID
		}
	}
	out := api.PaneMetadataView{StateLabels: map[string]string{}, Tokens: map[string]string{}}
	var presentations []*panePresentation
	for _, v := range m.Presentations {
		presentations = append(presentations, v)
	}
	sort.Slice(presentations, func(i, j int) bool { return presentations[i].Order < presentations[j].Order })
	for _, v := range presentations {
		if v.Agent != "" && v.Agent != agent || v.AppliesToSource != "" && v.AppliesToSource != s.reportSources[pid] {
			continue
		}
		if alive(v.Title) && v.Title.Value != "" {
			out.Title = v.Title.Value
		}
		if alive(v.DisplayAgent) && v.DisplayAgent.Value != "" {
			out.DisplayAgent = v.DisplayAgent.Value
		}
		for k, label := range v.Labels {
			if alive(label) {
				out.StateLabels[k] = label.Value
			}
		}
	}
	for k, v := range m.Tokens {
		if alive(v) {
			out.Tokens[k] = v.Value
		} else {
			delete(m.Tokens, k)
		}
	}
	return out
}

func (s *Server) paneMetadataRequest(req *api.Request) api.Response {
	var p api.PaneReportMetadataParams
	if err := req.DecodeParams(&p); err != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, err.Error())
	}
	pid, _, _, ok := s.findPane(p.PaneID)
	if !ok {
		return s.paneNotFound(req.ID, p.PaneID)
	}
	if strings.TrimSpace(p.Source) == "" || len(p.Tokens) > api.MetadataReportTokenLimit {
		return api.Failure(req.ID, api.ErrInvalidRequest, "source is required; at most 16 tokens per report")
	}
	if p.TTLms != nil && (*p.TTLms < 1 || *p.TTLms > api.MetadataTTLMax) {
		return api.Failure(req.ID, api.ErrInvalidRequest, "ttl_ms must be between 1 and 86400000")
	}
	for k := range p.Tokens {
		if !validMetadataName(k) {
			return api.Failure(req.ID, api.ErrInvalidRequest, fmt.Sprintf("invalid token name: %s", k))
		}
	}
	current := s.paneMetadataView(pid).Tokens
	if current == nil {
		current = map[string]string{}
	}
	for k, v := range p.Tokens {
		if v == nil {
			delete(current, k)
		} else {
			current[k] = *v
		}
	}
	// PaneInfo.tokens has maxProperties 32 in the API schema.
	if len(current) > api.MetadataWorkspaceTokenLimit {
		return api.Failure(req.ID, api.ErrInvalidRequest, "pane tokens exceed 32 entries")
	}
	if s.paneMetadata == nil {
		s.paneMetadata = map[int]*paneMetadata{}
	}
	m := s.paneMetadata[pid]
	if m == nil {
		m = &paneMetadata{Sequences: map[string]uint64{}, Presentations: map[string]*panePresentation{}, Tokens: map[string]metadataToken{}}
		s.paneMetadata[pid] = m
	}
	if p.Seq != nil {
		if previous, exists := m.Sequences[p.Source]; exists && *p.Seq <= previous {
			return api.Success(req.ID, api.OkResult{Type: "ok"})
		}
		m.Sequences[p.Source] = *p.Seq
	}
	expires := time.Time{}
	if p.TTLms != nil {
		expires = time.Now().Add(time.Duration(*p.TTLms) * time.Millisecond)
	}
	value := func(text string) metadataToken { return metadataToken{Value: text, Source: p.Source, Expires: expires} }
	presentation := m.Presentations[p.Source]
	if presentation == nil {
		presentation = &panePresentation{Labels: map[string]metadataToken{}}
		m.Presentations[p.Source] = presentation
	}
	m.NextOrder++
	presentation.Order = m.NextOrder
	if p.Agent != nil {
		presentation.Agent = *p.Agent
	}
	if p.AppliesToSource != nil {
		presentation.AppliesToSource = *p.AppliesToSource
	}
	if p.ClearTitle {
		presentation.Title = metadataToken{}
	} else if p.Title != nil {
		presentation.Title = value(strings.TrimSpace(*p.Title))
	}
	if p.ClearDisplayAgent {
		presentation.DisplayAgent = metadataToken{}
	} else if p.DisplayAgent != nil {
		presentation.DisplayAgent = value(strings.TrimSpace(*p.DisplayAgent))
	}
	if p.ClearStateLabels {
		presentation.Labels = map[string]metadataToken{}
	}
	for k, v := range p.StateLabels {
		presentation.Labels[k] = value(v)
	}
	for k, v := range p.Tokens {
		if v == nil {
			delete(m.Tokens, k)
		} else {
			m.Tokens[k] = value(*v)
		}
	}
	s.emitEvent("pane_metadata_updated", ev("pane_metadata_updated", "pane_id", p.PaneID))
	return api.Success(req.ID, api.OkResult{Type: "ok"})
}
