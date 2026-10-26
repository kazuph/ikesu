//go:build benchstage

package benchstage

import (
	"bytes"
	"crypto/sha256"
	"testing"
)

func TestOutputHashPreservesAllBytesAcrossEveryMarkerSplit(t *testing.T) {
	body := []byte("line\r\n\x1b[31m界\x1b[0m\r\n")
	stream := append([]byte("shell noise"+outputBegin), body...)
	stream = append(stream, []byte(outputEnd+"prompt")...)
	want := sha256.Sum256(body)
	for split := 0; split <= len(stream); split++ {
		var s outputHash
		s.consume(stream[:split])
		s.consume(stream[split:])
		if !s.done || s.count != int64(len(body)) || !bytes.Equal(s.hash.Sum(nil), want[:]) {
			t.Fatalf("split %d lost bytes", split)
		}
	}
	var s outputHash
	for _, b := range stream {
		s.consume([]byte{b})
	}
	if !s.done || s.count != int64(len(body)) || !bytes.Equal(s.hash.Sum(nil), want[:]) {
		t.Fatal("bytewise stream lost bytes")
	}
}
