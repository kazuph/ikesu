package server

import (
	"github.com/kazuph/ikesu/internal/platform"
	"github.com/kazuph/ikesu/internal/session"
)

var nativeResumeAgents = session.NativeResumeAgents

func observeCmdline(job platform.ForegroundJob) (string, bool) { return session.ObserveCmdline(job) }
func resumeArgsFor(agent string) []string                      { return session.ResumeArgsFor(agent) }
