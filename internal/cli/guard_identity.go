package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/kazuph/ikesu/internal/api"
	"github.com/kazuph/ikesu/internal/guard"
)

func decodeGuardPolicy(data []byte) (guard.Config, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return guard.Config{}, err
	}
	for _, name := range []string{"Enabled", "Mode", "Targets", "DangerWords", "ProtectedPatterns", "WranglerAllow", "AskTimeoutSeconds"} {
		if _, ok := fields[name]; !ok {
			return guard.Config{}, fmt.Errorf("incomplete guard policy")
		}
	}
	if len(fields) != 7 {
		return guard.Config{}, fmt.Errorf("unexpected guard policy fields")
	}
	var policy guard.Config
	if err := json.Unmarshal(data, &policy); err != nil {
		return guard.Config{}, err
	}
	if policy.EffectiveMode() != "record" && policy.EffectiveMode() != "enforce" || policy.Timeout() <= 0 {
		return guard.Config{}, fmt.Errorf("invalid guard policy")
	}
	return policy, nil
}

// A nil marker means the server proved that this process belongs to its pane.
// A non-nil marker carries the server's policy without the inherited origin.
func (r *runner) guardIdentity(i guard.Input) (*guard.Marker, *api.GuardIdentityContext, error) {
	pid := os.Getpid()
	_, start, err := guard.ApprovalProcess(pid)
	if err != nil || start == "" {
		return nil, nil, fmt.Errorf("cannot inspect guard process identity")
	}
	deadline := time.Now().Add(requestTimeout)
	var response api.Response
	for {
		data, err := r.requestBefore("cli:guard:identity", api.MethodGuardIdentity, api.GuardIdentityParams{PaneID: i.Env["IKESU_PANE_ID"], PID: pid, StartTime: start}, deadline)
		if err != nil {
			return nil, nil, err
		}
		response = api.Response{}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, nil, err
		}
		if response.ID != "cli:guard:identity" {
			return nil, nil, fmt.Errorf("invalid guard response id")
		}
		if response.Err == nil {
			break
		}
		if response.Err.Code != "server_stopping" {
			return nil, nil, fmt.Errorf("%s: %s", response.Err.Code, response.Err.Message)
		}
		// During handoff the retiring server can return this response.
		// Reconnect only for this response and within the original deadline.
	}
	var wire struct {
		Type         string          `json:"type"`
		Match        *bool           `json:"match"`
		Policy       json.RawMessage `json:"policy"`
		PolicyDigest string          `json:"policy_digest"`
	}
	if err := json.Unmarshal(response.Result, &wire); err != nil {
		return nil, nil, err
	}
	if wire.Type != "guard_identity" || wire.Match == nil || len(wire.Policy) == 0 || string(wire.Policy) == "null" {
		return nil, nil, fmt.Errorf("invalid guard identity response")
	}
	digest := sha256.Sum256(wire.Policy)
	if wire.PolicyDigest != hex.EncodeToString(digest[:]) {
		return nil, nil, fmt.Errorf("invalid guard policy digest")
	}
	policy, err := decodeGuardPolicy(wire.Policy)
	if err != nil {
		return nil, nil, err
	}
	context := &api.GuardIdentityContext{PaneID: i.Env["IKESU_PANE_ID"], Match: *wire.Match, PolicyDigest: wire.PolicyDigest, PID: pid, StartTime: start}
	if *wire.Match {
		marker, active, err := guard.ReadMarker(i)
		if err != nil {
			return nil, nil, err
		}
		if !active || !reflect.DeepEqual(marker.Policy, &policy) {
			return nil, nil, fmt.Errorf("guard marker policy mismatch")
		}
		return nil, context, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	return &guard.Marker{OutsidePane: true, Cwd: cwd, Socket: r.socketPath(), Mode: policy.EffectiveMode(), Policy: &policy}, context, nil
}
