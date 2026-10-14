package api

// This additive endpoint leaves all existing API contracts unchanged.
const MethodSessionImportHerdr = "session.import_herdr"

type SessionImportHerdrParams struct {
	Source       string `json:"source"`
	Digest       string `json:"digest"`
	HerdrRunning bool   `json:"herdr_running"`
}
