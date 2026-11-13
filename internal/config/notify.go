package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/kazuph/ikesu/internal/api"
)

// DefaultNotifyReachTimeout は reach_timeout を省いたときの待ち時間。
const DefaultNotifyReachTimeout = 30 * time.Second

// NotifyChannel は [[notify.channel]] の1件。
type NotifyChannel struct {
	ID          string
	Send        string // "now" か "unanswered"
	After       time.Duration
	FallbackFor []string
}

// NotifyConfig は [notify]。Enabled は表が config.toml にあるかどうか。
type NotifyConfig struct {
	Enabled      bool
	ReachTimeout time.Duration
	Channels     []NotifyChannel
}

// NotifyChannelIDs は channel の id として受け付ける値。
var NotifyChannelIDs = api.ReachChannelIDs

// Channel は id の channel を返す。
func (n NotifyConfig) Channel(id string) (NotifyChannel, bool) {
	for _, c := range n.Channels {
		if c.ID == id {
			return c, true
		}
	}
	return NotifyChannel{}, false
}

// NotifyJSON は `ikesu config notify --json` の出力の形。
type NotifyJSON struct {
	Enabled        bool                `json:"enabled"`
	ReachTimeoutMS int64               `json:"reach_timeout_ms"`
	Channels       []NotifyChannelJSON `json:"channels"`
	Warnings       []string            `json:"warnings"`
}
type NotifyChannelJSON struct {
	ID          string   `json:"id"`
	Send        string   `json:"send"`
	AfterMS     int64    `json:"after_ms"`
	FallbackFor []string `json:"fallback_for"`
}

// JSON は設定を出力用の形にする。warnings は設定全体の警告のうち notify. で始まるもの。
func (c Config) NotifyJSON() NotifyJSON {
	out := NotifyJSON{Enabled: c.Notify.Enabled, ReachTimeoutMS: c.Notify.ReachTimeout.Milliseconds(), Channels: []NotifyChannelJSON{}, Warnings: []string{}}
	for _, ch := range c.Notify.Channels {
		ff := append([]string{}, ch.FallbackFor...)
		out.Channels = append(out.Channels, NotifyChannelJSON{ID: ch.ID, Send: ch.Send, AfterMS: ch.After.Milliseconds(), FallbackFor: ff})
	}
	for _, w := range c.Warnings {
		if strings.HasPrefix(w, "notify") {
			out.Warnings = append(out.Warnings, w)
		}
	}
	return out
}

func notifyDuration(v any) (time.Duration, error) {
	s, ok := v.(string)
	if !ok {
		return 0, fmt.Errorf("duration must be a string like \"5m\"")
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if d < 0 {
		return 0, fmt.Errorf("duration must not be negative")
	}
	return d, nil
}

// parseNotify は [notify] を読む。誤りは warnings に載せ、その項目（channel）だけ無視する。
func parseNotify(v any, warnings *[]string) NotifyConfig {
	n := NotifyConfig{ReachTimeout: DefaultNotifyReachTimeout}
	table, ok := v.(map[string]any)
	if !ok {
		if v != nil {
			*warnings = append(*warnings, "notify: must be a table")
		}
		return n
	}
	n.Enabled = true
	warn := func(format string, a ...any) { *warnings = append(*warnings, "notify."+fmt.Sprintf(format, a...)) }
	if raw, exists := table["reach_timeout"]; exists {
		d, err := notifyDuration(raw)
		if err != nil || d == 0 {
			if err == nil {
				err = fmt.Errorf("must be positive")
			}
			warn("reach_timeout: %v; using %s", err, DefaultNotifyReachTimeout)
		} else {
			n.ReachTimeout = d
		}
	}
	list, _ := table["channel"].([]map[string]any)
	if list == nil {
		if items, ok := table["channel"].([]any); ok {
			for _, it := range items {
				if m, ok := it.(map[string]any); ok {
					list = append(list, m)
				}
			}
		} else if table["channel"] != nil {
			warn("channel: must be an array of tables ([[notify.channel]])")
		}
	}
	seen := map[string]bool{}
	for i, t := range list {
		id, _ := t["id"].(string)
		key := fmt.Sprintf("channel[%d]", i)
		if !NotifyChannelIDs[id] {
			warn("%s.id: unknown id %q; ignoring channel", key, id)
			continue
		}
		if seen[id] {
			warn("%s.id: duplicate id %q; ignoring channel", key, id)
			continue
		}
		send, _ := t["send"].(string)
		if send != "now" && send != "unanswered" {
			warn("%s.send: invalid value %v for %q; ignoring channel", key, t["send"], id)
			continue
		}
		ch := NotifyChannel{ID: id, Send: send}
		bad := false
		if raw, exists := t["after"]; exists {
			d, err := notifyDuration(raw)
			if err != nil {
				warn("%s.after: %v; ignoring channel", key, err)
				bad = true
			}
			ch.After = d
		}
		if raw, exists := t["fallback_for"]; exists {
			items, ok := raw.([]any)
			if !ok {
				warn("%s.fallback_for: must be an array of ids; ignoring channel", key)
				bad = true
			}
			for _, it := range items {
				f, _ := it.(string)
				if !NotifyChannelIDs[f] || f == id {
					warn("%s.fallback_for: invalid id %v; ignoring channel", key, it)
					bad = true
					break
				}
				ch.FallbackFor = append(ch.FallbackFor, f)
			}
		}
		if bad {
			continue
		}
		seen[id] = true
		n.Channels = append(n.Channels, ch)
	}
	return n
}
