package config

import (
	"reflect"
	"testing"
)

func TestTabMultipleDefaultsAndReplacement(t *testing.T) {
	for _, tc := range []struct {
		body           string
		next, previous Bindings
	}{
		{"", Bindings{"prefix+n", "ctrl+tab"}, Bindings{"prefix+p", "ctrl+shift+tab"}},
		{"[keys]\nnext_tab='prefix+n'\nprevious_tab='prefix+p'", Bindings{"prefix+n"}, Bindings{"prefix+p"}},
		{"[keys]\nnext_tab=''\nprevious_tab=''", Bindings{""}, Bindings{""}},
		{"[keys]\nnext_tab=[]\nprevious_tab=[]", Bindings{""}, Bindings{""}},
		{"[keys]\nnext_tab=['cmd+j','cmd+l']\nprevious_tab=['cmd+k']", Bindings{"cmd+j", "cmd+l"}, Bindings{"cmd+k"}},
	} {
		c, err := Decode([]byte(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		if got := c.ActionBindings("next_tab"); !reflect.DeepEqual(got, tc.next) {
			t.Fatalf("%q next=%v want=%v", tc.body, got, tc.next)
		}
		if got := c.ActionBindings("previous_tab"); !reflect.DeepEqual(got, tc.previous) {
			t.Fatalf("%q previous=%v want=%v", tc.body, got, tc.previous)
		}
	}
}
