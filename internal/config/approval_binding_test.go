package config

import "testing"

func TestGuardApprovalBindingRespectsExplicitExistingKey(t *testing.T) {
	c, err := Decode([]byte("[keys]\nhelp='prefix+a'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.ActionBindings("help"); len(got) != 1 || got[0] != "prefix+a" {
		t.Fatalf("help = %v", got)
	}
	if got := c.ActionBindings("open_guard_approval"); len(got) != 1 || got[0] != "" {
		t.Fatalf("approval stole configured key: %v", got)
	}
}
