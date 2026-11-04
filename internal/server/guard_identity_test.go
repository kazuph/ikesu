package server

import "testing"

func TestGuardProcessStartOrder(t *testing.T) {
	for _, tc := range []struct {
		parent, child string
		want          bool
	}{
		{"100:200", "100:201", true}, {"100:200", "101:0", true},
		{"101:0", "100:999999", false}, {"12", "13", true},
		{"13", "12", false}, {"broken", "12", false}, {"12", "broken", false},
	} {
		if got := guardStartBefore(tc.parent, tc.child); got != tc.want {
			t.Errorf("parent %q child %q: got %v want %v", tc.parent, tc.child, got, tc.want)
		}
	}
}
