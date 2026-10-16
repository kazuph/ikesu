package input

import "testing"

func TestPlainModifiersObservedInHerdr056(t *testing.T) {
	var f Framer
	var got []byte
	for _, e := range f.Push([]byte("\x1b[97;3u\x1b[97;7u\x1b[97:65;6u")) {
		if e.Kind == KindKey {
			got = append(got, EncodeKey(e.Key, e.Raw, 0, false, false, false, false)...)
		}
	}
	if string(got) != "\x1ba\x1b\x01\x01" {
		t.Fatalf("got %q", got)
	}
}

func TestPaneProtocolBytesObservedInHerdr056(t *testing.T) {
	raw := "\x1b[97u\x1b[97;5u\x1b[97:65;2;65u\x1b[13;2u\x1b[A\x1bOA\x1b[1;2A\x1b[97;1:2u\x1b[97;1:3u\x1b[57441u\x1b[57441;1:3u\x1b[5;2~日END"
	for _, tc := range []struct {
		name     string
		flags    int
		endpoint bool
		want     string
	}{
		{"plain", 0, false, "a\x01A\x1b[27;2;13~\x1b[A\x1b[A\x1b[1;2Aa\x1b[5;2~日END"},
		{"kitty1", 1, false, "a\x1b[97;5uA\x1b[13;2u\x1b[A\x1b[A\x1b[1;2Aa\x1b[5;2~日END"},
		{"kitty3", 3, false, "a\x1b[97;5:1uA\x1b[13;2u\x1b[1;1:1A\x1b[1;1:1A\x1b[1;2:1Aa\x1b[5;2~日END"},
		{"local31", 31, false, "a\x1b[97;5:1uA\x1b[13;2u\x1b[1;1:1A\x1b[1;1:1A\x1b[1;2:1Aa\x1b[5;2~日END"},
		{"endpoint31", 31, true, "\x1b[97;1:1;97u\x1b[97;5:1u\x1b[97:65;2:1;65u\x1b[13;2:1u\x1b[57419;1:1u\x1b[57419;1:1u\x1b[57419;2:1u\x1b[97;1:2;97u\x1b[97;1:3u\x1b[57421;2:1u\x1b[26085;1:1;26085u\x1b[101:69;2:1;69u\x1b[110:78;2:1;78u\x1b[100:68;2:1;68u"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var f Framer
			var got []byte
			for _, e := range f.Push([]byte(raw)) {
				if e.Kind == KindKey || e.Kind == KindKeyRelease {
					got = append(got, EncodeKey(e.Key, e.Raw, tc.flags, tc.flags != 0, false, tc.endpoint, e.Kind == KindKeyRelease)...)
				}
			}
			if string(got) != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}
