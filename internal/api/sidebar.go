package api

type WorkspaceDuplicateParams struct {
	WorkspaceID string `json:"workspace_id"`
	Focus       bool   `json:"focus"`
}
type WorkspaceSetSectionParams struct {
	WorkspaceID string `json:"workspace_id"`
	Section     string `json:"section"`
}
type WorkspaceReportMetadataParams struct {
	WorkspaceID string             `json:"workspace_id"`
	Source      string             `json:"source"`
	Tokens      map[string]*string `json:"tokens"`
	Seq         *uint64            `json:"seq,omitempty"`
	TTLms       *uint64            `json:"ttl_ms,omitempty"`
}

// Bounds come from docs/ref/herdr-api-schema.json.
const (
	MetadataReportTokenLimit    = 16
	MetadataWorkspaceTokenLimit = 32
	MetadataTokenNameLimit      = 32
	MetadataTTLMax              = 86400000
)
