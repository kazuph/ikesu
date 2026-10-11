package notify

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/kazuph/goterrace/internal/api"
	"github.com/kazuph/goterrace/internal/config"
	"github.com/kazuph/goterrace/internal/endpoint"
)

func TestBundledSoundsNumericWAVAndPlaybackPaths(t *testing.T) {
	t.Setenv("GTX_DISABLE_SOUND", "")
	cache := t.TempDir()
	for _, tc := range []struct {
		kind    string
		data    []byte
		rate    int
		seconds float64
	}{{"finished", doneSound, 48000, 1.08}, {"needs_attention", requestSound, 44100, 1.462844}} {
		b := tc.data
		if len(b) < 44 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || binary.LittleEndian.Uint16(b[20:22]) != 1 || binary.LittleEndian.Uint16(b[22:24]) != 1 || binary.LittleEndian.Uint16(b[34:36]) != 16 {
			t.Fatal("not mono PCM16 WAV")
		}
		rate := int(binary.LittleEndian.Uint32(b[24:28]))
		frames := int(binary.LittleEndian.Uint32(b[40:44])) / 2
		if rate != tc.rate || frames != int(math.Round(tc.seconds*float64(rate))) || frames*2 != len(b)-44 {
			t.Fatal("sample rate or duration mismatch", rate, frames)
		}
		var squares, peak float64
		for i := 44; i < len(b); i += 2 {
			v := float64(int16(binary.LittleEndian.Uint16(b[i:i+2]))) / 32768
			squares += v * v
			peak = math.Max(peak, math.Abs(v))
		}
		if squares == 0 || peak >= 1 {
			t.Fatal("silent or clipped sound")
		}
		t.Logf("%s: %d Hz, %.9f seconds, RMS %.3f dBFS, peak %.3f dBFS", tc.kind, rate, float64(frames)/float64(rate), 20*math.Log10(math.Sqrt(squares/float64(frames))), 20*math.Log10(peak))
		var calls [][]string
		d := Desktop{Executable: "/tmp/gtx-next", CacheDir: func() (string, error) { return cache, nil }, Run: func(name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		}}
		if err := d.Deliver(endpoint.Notification{Kind: tc.kind, Agent: "codex"}, config.Config{}, "local", ""); err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(cache, api.ExecutableNamespace(d.Executable), "sounds", fmt.Sprintf("%x.wav", sha256.Sum256(b)))
		if len(calls) != 1 || len(calls[0]) != 2 || calls[0][0] != "/usr/bin/afplay" || calls[0][1] != want {
			t.Fatal(calls, want)
		}
		got, err := os.ReadFile(want)
		if err != nil || !bytes.Equal(got, b) {
			t.Fatal("wrong materialized sound", err)
		}
		for _, path := range []string{filepath.Join(cache, "gtx-next"), filepath.Dir(want), want} {
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			mode := os.FileMode(0600)
			if info.IsDir() {
				mode = 0700
			}
			if info.Mode().Perm() != mode {
				t.Fatal(path, info.Mode())
			}
		}
		stamp := time.Unix(1, 0)
		if err := os.Chtimes(want, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		if _, err := d.defaultSound(tc.kind); err != nil {
			t.Fatal(err)
		}
		info, _ := os.Stat(want)
		if !info.ModTime().Equal(stamp) {
			t.Fatal("existing hash rewritten")
		}
		other := d
		other.Executable = "/tmp/gtx"
		otherPath, err := other.defaultSound(tc.kind)
		if err != nil || otherPath == want || filepath.Dir(otherPath) != filepath.Join(cache, "gtx", "sounds") {
			t.Fatal("namespace collision", otherPath, err)
		}
	}
}

func TestDefaultSoundCacheFailureKeepsNotificationAndWarnsOnce(t *testing.T) {
	t.Setenv("GTX_DISABLE_SOUND", "")
	base := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(base, []byte("occupied"), 0600); err != nil {
		t.Fatal(err)
	}
	var warnings, calls int
	d := Desktop{CacheDir: func() (string, error) { return base, nil }, soundWarning: &sync.Once{}, Warn: func(error) { warnings++ }, LookPath: func(string) (string, error) { return "/test/terminal-notifier", nil }, Run: func(name string, args ...string) error {
		if name != "/test/terminal-notifier" {
			t.Fatal("unexpected playback", name, args)
		}
		calls++
		return nil
	}}
	for _, kind := range []string{"finished", "needs_attention"} {
		if err := d.Deliver(endpoint.Notification{Kind: kind, SystemToast: true, TargetPaneID: "p1"}, config.Config{}, "test", ""); err != nil {
			t.Fatal("cache failure escaped notification handler", err)
		}
	}
	if warnings != 1 || calls != 2 {
		t.Fatal(warnings, calls)
	}
}

func TestDefaultSoundConcurrentCachePublication(t *testing.T) {
	base := t.TempDir()
	d := Desktop{CacheDir: func() (string, error) { return base, nil }}
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path, err := d.defaultSound("finished")
			if err != nil {
				t.Error(err)
				return
			}
			b, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(b, doneSound) {
				t.Error("partial publication", err)
			}
		}()
	}
	wg.Wait()
}
