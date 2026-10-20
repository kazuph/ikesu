package server

import (
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/session"
	"github.com/kazuph/ikesu/internal/tui"
)

func observedSessionRecord(agent, id, title string, job platform.ForegroundJob) tui.AgentRecord {
	rec := tui.AgentRecord{Agent: agent, Kind: "id", Value: id, Title: title, Source: "observed:file"}
	if agent == "claude" {
		if _, resumed := session.ObserveCmdline(job); resumed {
			rec.SessionStartSource = "resume"
		}
	}
	return rec
}
