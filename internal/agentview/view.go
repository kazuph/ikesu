// Package agentview describes display-only projections of agent rows.
package agentview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
)

type View struct {
	Source string  `json:"source"`
	Label  *string `json:"label"`
	Filter *Filter `json:"filter"`
	Sort   []Sort  `json:"sort"`
}
type Filter struct {
	Op      string            `json:"op"`
	Filters []Filter          `json:"filters"`
	Filter  *Filter           `json:"filter"`
	Field   json.RawMessage   `json:"field"`
	Value   json.RawMessage   `json:"value"`
	Values  []json.RawMessage `json:"values"`
}
type Sort struct {
	Field json.RawMessage `json:"field"`
	Order string          `json:"order"`
}

func FieldName(raw json.RawMessage, sorting bool) (string, error) {
	var name string
	if err := json.Unmarshal(raw, &name); err == nil {
		allowed := map[string]bool{"status": true, "agent": true, "seen": true, "state_change_seq": true}
		if sorting {
			allowed["workspace_order"] = true
			allowed["tab_order"] = true
			allowed["pane_order"] = true
			allowed["attention"] = true
		} else {
			allowed["workspace_id"] = true
			allowed["tab_id"] = true
			allowed["pane_id"] = true
		}
		if !allowed[name] {
			return "", fmt.Errorf("unsupported agent view field %q", name)
		}
		return name, nil
	}
	var token struct {
		Token *string `json:"token"`
	}
	if err := json.Unmarshal(raw, &token); err != nil || token.Token == nil {
		return "", fmt.Errorf("agent view field requires a builtin name or token")
	}
	return "token." + *token.Token, nil
}
func Value(raw json.RawMessage, context map[string]string) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	switch value := v.(type) {
	case string, bool:
		return value, nil
	case json.Number:
		n, err := strconv.ParseUint(string(value), 10, 64)
		return n, err
	case map[string]any:
		name, ok := value["context"].(string)
		if !ok || (name != "current_workspace_id" && name != "current_tab_id") {
			return nil, fmt.Errorf("unsupported agent view context")
		}
		if context == nil {
			return name, nil
		}
		return context[name], nil
	default:
		return nil, fmt.Errorf("agent view value requires string, bool, uint64 or context")
	}
}
func (v View) Validate() error {
	if v.Source == "" {
		return fmt.Errorf("agent view source is required")
	}
	if v.Filter != nil {
		if err := v.Filter.Validate(); err != nil {
			return err
		}
	}
	for _, s := range v.Sort {
		if _, err := FieldName(s.Field, true); err != nil {
			return err
		}
		if s.Order != "" && s.Order != "asc" && s.Order != "desc" {
			return fmt.Errorf("invalid agent view sort order %q", s.Order)
		}
	}
	return nil
}
func (f Filter) Validate() error {
	switch f.Op {
	case "all", "any":
		if f.Filters == nil {
			return fmt.Errorf("%s requires filters", f.Op)
		}
		for _, child := range f.Filters {
			if err := child.Validate(); err != nil {
				return err
			}
		}
		return nil
	case "not":
		if f.Filter == nil {
			return fmt.Errorf("not requires filter")
		}
		return f.Filter.Validate()
	case "eq", "in", "exists":
		if _, err := FieldName(f.Field, false); err != nil {
			return err
		}
		if f.Op == "eq" {
			_, err := Value(f.Value, nil)
			return err
		}
		if f.Op == "in" {
			if f.Values == nil {
				return fmt.Errorf("in requires values")
			}
			for _, value := range f.Values {
				if _, err := Value(value, nil); err != nil {
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported agent view operator %q", f.Op)
	}
}
