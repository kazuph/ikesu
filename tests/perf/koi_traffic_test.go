package perf

import (
	"encoding/base64"
	"encoding/json"
	"github.com/kazuph/ikesu/internal/endpoint"
	"os"
	"path/filepath"
	"testing"
)

// Decode the untouched server stream captured by the isolated real CLI bench.
// Re-encoding both settings on the same input measures configuration overhead,
// without confusing its gob/base64 representation with decoration updates.
func TestAuditKoiTraffic(t *testing.T) {
	dir := os.Getenv("KOI_TRAFFIC_DIR")
	if dir == "" {
		return
	}
	files, err := filepath.Glob(filepath.Join(dir, "*-traffic.json"))
	if err != nil || len(files) == 0 {
		t.Fatal("no traffic records", err)
	}
	var output []map[string]any
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		var messages []struct{ Type, Data string }
		if err := json.Unmarshal(data, &messages); err != nil {
			t.Fatal(err)
		}
		var decoder endpoint.SnapshotDecoder
		var onEncoder, offEncoder endpoint.SnapshotEncoder
		count, overhead := 0, 0
		var steps []int
		var values []int
		previousSpin := -1
		for _, m := range messages {
			if m.Type != "snapshot" {
				continue
			}
			wire, err := base64.StdEncoding.DecodeString(m.Data)
			if err != nil {
				t.Fatal(err)
			}
			in, err := decoder.Decode(wire)
			if err != nil {
				t.Fatal(err)
			}
			if in.Koi || in.KoiTime != 0 || in.KoiMotion != nil || in.KoiRipple != nil || in.KoiGlow != nil {
				t.Fatal("client animation leaked to server snapshot")
			}
			if previousSpin >= 0 {
				steps = append(steps, in.Spin-previousSpin)
			}
			previousSpin = in.Spin
			values = append(values, in.Spin)
			on, off := true, false
			in.SidebarConfig.UI.Koi = &on
			a, err := onEncoder.Encode(in)
			if err != nil {
				t.Fatal(err)
			}
			aLen := base64.StdEncoding.EncodedLen(len(a))
			in.SidebarConfig.UI.Koi = &off
			b, err := offEncoder.Encode(in)
			if err != nil {
				t.Fatal(err)
			}
			overhead += aLen - base64.StdEncoding.EncodedLen(len(b))
			count++
		}
		output = append(output, map[string]any{"file": filepath.Base(file), "snapshots": count, "spinner_steps": steps, "spinner_values": values, "client_animation_snapshots": 0, "same_input_config_on_minus_off_base64_bytes": overhead})
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "attribution.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	t.Log(string(data))
}
