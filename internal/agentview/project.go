package agentview

import (
	"reflect"
	"sort"
	"strings"
)

type Row struct {
	Pane   int
	Fields map[string]any
}

func (f *Filter) Matches(fields map[string]any, context map[string]string) bool {
	if f == nil {
		return true
	}
	switch f.Op {
	case "all":
		for i := range f.Filters {
			if !f.Filters[i].Matches(fields, context) {
				return false
			}
		}
		return true
	case "any":
		for i := range f.Filters {
			if f.Filters[i].Matches(fields, context) {
				return true
			}
		}
		return false
	case "not":
		return !f.Filter.Matches(fields, context)
	}
	name, _ := FieldName(f.Field, false)
	actual, exists := fields[name]
	if f.Op == "exists" {
		return exists
	}
	if !exists {
		return false
	}
	if f.Op == "eq" {
		expected, _ := Value(f.Value, context)
		return reflect.DeepEqual(actual, expected)
	}
	for _, raw := range f.Values {
		expected, _ := Value(raw, context)
		if reflect.DeepEqual(actual, expected) {
			return true
		}
	}
	return false
}
func (v *View) Project(rows []Row, context map[string]string) []Row {
	if v == nil {
		return rows
	}
	out := make([]Row, 0, len(rows))
	for _, row := range rows {
		if v.Filter.Matches(row.Fields, context) {
			out = append(out, row)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		for _, rule := range v.Sort {
			name, _ := FieldName(rule.Field, true)
			comparison := compare(out[i].Fields[name], out[j].Fields[name])
			if comparison != 0 {
				if rule.Order == "desc" {
					return comparison > 0
				}
				return comparison < 0
			}
		}
		return false
	})
	return out
}
func compare(a, b any) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	switch left := a.(type) {
	case string:
		if right, ok := b.(string); ok {
			return strings.Compare(left, right)
		}
	case uint64:
		if right, ok := b.(uint64); ok {
			if left < right {
				return -1
			}
			if left > right {
				return 1
			}
		}
	case bool:
		if right, ok := b.(bool); ok {
			if !left && right {
				return -1
			}
			if left && !right {
				return 1
			}
		}
	}
	return 0
}
