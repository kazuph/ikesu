package session

import (
	"github.com/kazuph/goterrace/internal/detect"
	"github.com/kazuph/goterrace/internal/platform"
	"strings"
)

type NativeResumeSpec struct{ Flag, Source string }

var NativeResumeAgents = map[string]NativeResumeSpec{
	"grok":  {Flag: "--resume", Source: "gtx:grok"},
	"qwen":  {Flag: "--resume", Source: "gtx:qwen"},
	"agy":   {Flag: "--conversation", Source: "gtx:antigravity_cli"},
	"letta": {Flag: "--conversation", Source: "gtx:letta"},
}

// Only the documented native launch flag is session evidence for these agents.
func ObserveNativeResume(pr platform.ForegroundProcess) (id string, native, ok bool) {
	args := pr.Argv
	argv0 := ""
	if len(args) > 0 {
		argv0 = args[0]
	}
	agent := detect.Identify(pr.Name, argv0)
	spec, native := NativeResumeAgents[agent]
	if !native {
		return "", false, false
	}
	for i, arg := range args {
		value := ""
		if arg == spec.Flag {
			if i+1 >= len(args) {
				return "", true, false
			}
			value = args[i+1]
		} else if strings.HasPrefix(arg, spec.Flag+"=") {
			value = strings.TrimPrefix(arg, spec.Flag+"=")
		} else {
			continue
		}
		if !SafeSessionID(value) || id != "" && id != value {
			return "", true, false
		}
		id = value
	}
	return id, true, true
}
