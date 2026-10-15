package screenprompt

import (
	"strings"
	"testing"
)

func TestWrappedQuestionText(t *testing.T) {
	for _, tc := range []struct {
		name    string
		lines   []string
		columns int
		want    string
	}{
		{"Japanese", []string{"かけ", "ら"}, 20, "かけら"},
		{"filename", []string{"report_neck4ey", "e.png"}, 20, "report_neck4eye.png"},
		{"path", []string{"/private/tmp/gtx-ac", "cept/proof.txt"}, 20, "/private/tmp/gtx-accept/proof.txt"},
		{"English", []string{"Which output format", "should the report use?"}, 24, "Which output format should the report use?"},
		{"long word", []string{"abcdefghijklmnopqrst", "uvwxyz"}, 20, "abcdefghijklmnopqrstuvwxyz"},
		{"full Japanese row", []string{"日本語で質問を確認ben", "ch;"}, 21, "日本語で質問を確認bench;"},
		{"not full Japanese row", []string{"日本語で English", "words"}, 30, "日本語で English words"},
		{"sentence", []string{"Read this sentence.", "Another sentence."}, 30, "Read this sentence. Another sentence."},
		{"shell redirection", []string{"printf observation >", "/tmp/proof.txt"}, 30, "printf observation > /tmp/proof.txt"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := joinWrapped(tc.lines, tc.columns); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestWrappedTitleAndDescription(t *testing.T) {
	for _, tc := range []struct {
		lines []string
		want  string
	}{
		{[]string{"report_neck4ey", "e.png"}, "report_neck4eye.png"},
		{[]string{"かけ", "ら"}, "かけら"},
		{[]string{"Which output format", "should the report use?"}, "Which output format should the report use?"},
		{[]string{"abcdefghijklmnopqrstuvwx", "yz"}, "abcdefghijklmnopqrstuvwxyz"},
	} {
		screen := append([]string{strings.Repeat("─", 24)}, tc.lines...)
		screen = append(screen, "❯ 1. Yes", "       report_neck4ey", "       e.png", "  2. No")
		p := Parse("claude", screen)
		if p == nil || p.Params.Title != tc.want || p.Params.Options[0].Description == nil || *p.Params.Options[0].Description != "report_neck4eye.png" {
			t.Fatalf("want %q: %+v", tc.want, p)
		}
	}
}

func TestWrappedPermissionCommand(t *testing.T) {
	screen := []string{strings.Repeat("─", 24), "Run this command?", " │ report_neck4ey", " │ e.png", "❯ 1. Yes", " 2. No"}
	p := Parse("claude", screen)
	if p == nil || p.Params.Title != "Run this command? report_neck4eye.png" {
		t.Fatal(p)
	}
}
