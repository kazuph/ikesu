package client

import (
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/kazuph/goterrace/internal/config"
	"testing"
)

func TestRemoteImagePasteConfiguredBindings(t *testing.T) {
	for _, tc := range []struct {
		binding      string
		key          uv.Key
		prefix, want bool
	}{
		{"ctrl+p", uv.Key{Code: 'p', Mod: uv.ModCtrl}, false, true},
		{"ctrl+p", uv.Key{Code: 'v', Mod: uv.ModCtrl}, false, false},
		{"prefix+i", uv.Key{Code: 'i'}, true, true},
		{"prefix+i", uv.Key{Code: 'i'}, false, false},
		{"", uv.Key{Code: 'v', Mod: uv.ModCtrl}, false, false},
	} {
		c, err := config.Decode([]byte("[keys]\nremote_image_paste='" + tc.binding + "'\n"))
		if err != nil {
			t.Fatal(err)
		}
		if got := remoteImagePasteKey(c, tc.key, tc.prefix); got != tc.want {
			t.Fatal(tc, got)
		}
	}
}
