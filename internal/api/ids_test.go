package api

import "testing"

func TestParsePaneRef(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"p12", 12, true},
		{"p_12", 12, true},
		{"%12", 12, true},
		{"12", 12, true},
		{"p0", 0, false},
		{"0", 0, false},
		{"px", 0, false},
		{"", 0, false},
		{"w1", 0, false},
	} {
		got, ok := ParsePaneRef(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParsePaneRef(%q) = %d,%v; want %d,%v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

func TestParseWorkspaceRef(t *testing.T) {
	for _, tc := range []struct {
		in     string
		number int
		index  int
		ok     bool
	}{
		{"s3", 3, 0, true},
		{"w3", 3, 0, true},  // w<n> is a legacy alias for s<n>
		{"w_2", 0, 2, true}, // positional
		{"2", 0, 2, true},   // bare number = position
		{"s0", 0, 0, false},
		{"s", 0, 0, false},
		{"x1", 0, 0, false},
	} {
		got, ok := ParseWorkspaceRef(tc.in)
		if ok != tc.ok || got.Number != tc.number || got.Index != tc.index {
			t.Errorf("ParseWorkspaceRef(%q) = %+v,%v; want number=%d index=%d ok=%v",
				tc.in, got, ok, tc.number, tc.index, tc.ok)
		}
	}
}

func TestParseTabRef(t *testing.T) {
	for _, tc := range []struct {
		in       string
		wsNumber int
		wsIndex  int
		number   int
		index    int
		ok       bool
	}{
		{"s1:t2", 1, 0, 2, 0, true},  // by number
		{"s1:2", 1, 0, 0, 2, true},   // positional
		{"t_s1_2", 1, 0, 0, 2, true}, // positional
		{"w_1:t3", 0, 1, 3, 0, true},
		{"s1", 0, 0, 0, 0, false},
		{"t_x_1", 0, 0, 0, 0, false},
		{"s1:t0", 0, 0, 0, 0, false},
	} {
		got, ok := ParseTabRef(tc.in)
		if ok != tc.ok || got.Workspace.Number != tc.wsNumber || got.Workspace.Index != tc.wsIndex ||
			got.Number != tc.number || got.Index != tc.index {
			t.Errorf("ParseTabRef(%q) = %+v,%v", tc.in, got, ok)
		}
	}
}

func TestFormatIDs(t *testing.T) {
	if FormatPaneID(7) != "p7" {
		t.Fatal("pane id")
	}
	if FormatWorkspaceID(3) != "s3" {
		t.Fatal("workspace id")
	}
	if FormatTabID(3, 2) != "s3:t2" {
		t.Fatal("tab id")
	}
}

func TestValidSessionName(t *testing.T) {
	for _, tc := range []struct {
		name string
		ok   bool
	}{
		{"default", true}, {"work-1", true}, {"a.b", true},
		{"", false}, {".", false}, {"..", false}, {"a/b", false}, {"a\\b", false},
	} {
		if got := ValidSessionName(tc.name); got != tc.ok {
			t.Errorf("ValidSessionName(%q) = %v; want %v", tc.name, got, tc.ok)
		}
	}
}
