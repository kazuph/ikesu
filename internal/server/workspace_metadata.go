package server

import (
	"fmt"
	"github.com/kazuph/ikesu/internal/api"
	"time"
)

type metadataToken struct {
	Value, Source string
	Expires       time.Time
}
type workspaceMetadata struct {
	Seq    *uint64
	Tokens map[string]metadataToken
}

func validMetadataName(k string) bool {
	if len(k) == 0 || len(k) > api.MetadataTokenNameLimit {
		return false
	}
	for _, r := range k {
		if !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
func (s *Server) workspaceTokens(wid int) map[string]string {
	out := map[string]string{}
	now := time.Now()
	for _, source := range s.workspaceMetadata[wid] {
		for k, v := range source.Tokens {
			if !v.Expires.IsZero() && !now.Before(v.Expires) {
				delete(source.Tokens, k)
				continue
			}
			out[k] = v.Value
		}
	}
	return out
}
func (s *Server) workspaceMetadataRequest(req *api.Request) api.Response {
	var p api.WorkspaceReportMetadataParams
	if e := req.DecodeParams(&p); e != nil {
		return api.Failure(req.ID, api.ErrInvalidRequest, e.Error())
	}
	i := s.findWorkspace(p.WorkspaceID)
	if i < 0 {
		return s.wsNotFound(req.ID, p.WorkspaceID)
	}
	wid := s.app.State().Workspaces[i].ID
	if p.Source == "" || p.Tokens == nil || len(p.Tokens) > api.MetadataReportTokenLimit {
		return api.Failure(req.ID, api.ErrInvalidRequest, "source and tokens are required; at most 16 tokens per report")
	}
	if p.TTLms != nil && (*p.TTLms < 1 || *p.TTLms > api.MetadataTTLMax) {
		return api.Failure(req.ID, api.ErrInvalidRequest, "ttl_ms must be between 1 and 86400000")
	}
	for k := range p.Tokens {
		if !validMetadataName(k) {
			return api.Failure(req.ID, api.ErrInvalidRequest, fmt.Sprintf("invalid token name: %s", k))
		}
	}
	current := s.workspaceTokens(wid)
	for k, v := range p.Tokens {
		if v == nil {
			delete(current, k)
		} else {
			current[k] = *v
		}
	}
	if len(current) > api.MetadataWorkspaceTokenLimit {
		return api.Failure(req.ID, api.ErrInvalidRequest, "workspace tokens exceed 32 entries")
	}
	if s.workspaceMetadata == nil {
		s.workspaceMetadata = map[int]map[string]*workspaceMetadata{}
	}
	if s.workspaceMetadata[wid] == nil {
		s.workspaceMetadata[wid] = map[string]*workspaceMetadata{}
	}
	sources := s.workspaceMetadata[wid]
	source := sources[p.Source]
	if source == nil {
		source = &workspaceMetadata{Tokens: map[string]metadataToken{}}
		sources[p.Source] = source
	}
	if p.Seq != nil && source.Seq != nil && *p.Seq <= *source.Seq {
		return api.Success(req.ID, api.OkResult{Type: "ok"})
	}
	if p.Seq != nil {
		seq := *p.Seq
		source.Seq = &seq
	}
	expires := time.Time{}
	if p.TTLms != nil {
		expires = time.Now().Add(time.Duration(*p.TTLms) * time.Millisecond)
	}
	for k, v := range p.Tokens {
		for _, owner := range sources {
			delete(owner.Tokens, k)
		}
		if v != nil {
			source.Tokens[k] = metadataToken{Value: *v, Source: p.Source, Expires: expires}
		}
	}
	s.emitEvent("workspace_metadata_updated", ev("workspace_metadata_updated", "workspace", s.wsInfo(i)))
	return api.Success(req.ID, api.OkResult{Type: "ok"})
}
