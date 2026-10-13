package runtime

import (
	"reflect"
	"strings"
	"testing"
)

func TestOSCTapUTF8AcrossChunks(t *testing.T) {
	const body = "本 ✳ 通知本文"
	for _, command := range []string{"0", "2", "9", "777"} {
		payload := body
		if command == "777" {
			payload = "notify;" + body
		}
		for _, end := range []string{"\a", "\x1b\\"} {
			stream := []byte("\x1b]" + command + ";" + payload + end)
			check := func(chunks [][]byte) {
				t.Helper()
				var tap oscTap
				var got [][2]string
				for _, chunk := range chunks {
					got = append(got, tap.observe(chunk)...)
				}
				want := [][2]string{{command, payload}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("command %s end %q: got=%q want=%q", command, end, got, want)
				}
			}
			for split := 0; split <= len(stream); split++ {
				check([][]byte{stream[:split], stream[split:]})
			}
			var chunks [][]byte
			for i := range stream {
				chunks = append(chunks, stream[i:i+1])
			}
			check(chunks)
		}
	}
}

func TestOSCTapIgnoreUTF8DoesNotExposeNestedOSC(t *testing.T) {
	for _, prefix := range []string{"\x1bPq", "\x1b_", "\x1b^", "\x1bX"} {
		for _, end := range []string{"\a", "\x1b\\"} {
			stream := []byte(prefix + "本 ✳ \x1b]9;INJECTED" + end + "\x1b]9;本 ✳ 通知\a")
			check := func(chunks [][]byte) {
				t.Helper()
				var tap oscTap
				var got [][2]string
				for _, chunk := range chunks {
					got = append(got, tap.observe(chunk)...)
				}
				want := [][2]string{{"9", "本 ✳ 通知"}}
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("prefix %q end %q: got=%q want=%q", prefix, end, got, want)
				}
			}
			for split := 0; split <= len(stream); split++ {
				check([][]byte{stream[:split], stream[split:]})
			}
			var chunks [][]byte
			for i := range stream {
				chunks = append(chunks, stream[i:i+1])
			}
			check(chunks)
		}
	}
}

func TestRecordedUTF8ProgressDoesNotLeakAcrossChunks(t *testing.T) {
	const body = "本 ✳ 通知本文"
	for _, end := range []string{"\a", "\x1b\\"} {
		stream := []byte("KEEP\x1b]9;" + body + end + "\x1b]777;notify;" + body + end)
		check := func(chunks [][]byte) {
			t.Helper()
			p := NewSupervisor().newPane(1, "/tmp", 80, 4)
			for _, chunk := range chunks {
				p.flushPTYBatch(chunk)
			}
			if got := strings.TrimSpace(p.ScreenText()); got != "KEEP" || p.Snapshot().OSCProgress != body {
				t.Fatalf("end %q screen=%q progress=%q", end, got, p.Snapshot().OSCProgress)
			}
		}
		for split := 0; split <= len(stream); split++ {
			check([][]byte{stream[:split], stream[split:]})
		}
		var chunks [][]byte
		for i := range stream {
			chunks = append(chunks, stream[i:i+1])
		}
		check(chunks)
	}
}
