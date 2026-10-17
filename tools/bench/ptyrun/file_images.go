package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// Observe the actual file before viewer cleanup; terminal replies stay unchanged.
func captureFileImage(out io.Writer, data []byte, offset int64) {
	if len(data) == 0 || data[0] != 'G' {
		return
	}
	header, payload, _ := strings.Cut(string(data[1:]), ";")
	fields := map[string]string{}
	for _, field := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			fields[key] = value
		}
	}
	if fields["t"] != "f" || fields["a"] != "t" {
		return
	}
	record := map[string]any{"control": header, "raw_bytes_end": offset}
	path, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		record["path"] = string(path)
		var pixels []byte
		pixels, err = os.ReadFile(string(path))
		if err == nil {
			record["length"] = len(pixels)
			record["sha256"] = fmt.Sprintf("%x", sha256.Sum256(pixels))
		}
	}
	if err != nil {
		record["error"] = err.Error()
	}
	_ = json.NewEncoder(out).Encode(record)
}
