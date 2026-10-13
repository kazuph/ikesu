package config

import "testing"

func TestScreenPromptsDefaultAndDisabled(t *testing.T) {
	for _, in := range []string{"", "[decisions]\nscreen_prompts = true", "[decisions]\nscreen_prompts = false"} {
		c, err := Decode([]byte(in))
		if err != nil {
			t.Fatal(err)
		}
		if c.ScreenPrompts() != (in != "[decisions]\nscreen_prompts = false") {
			t.Fatal(c)
		}
	}
}
