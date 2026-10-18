package api

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestGraphicsFileFrameUsesPathObjectAndUnsignedReceiptValues(t *testing.T) {
	const header = `{"format":"rgba","image_width":2,"image_height":3,"file":{"path":"/private/source/frame.rgba"},"sequence":18446744073709551615,"revision":8}`
	frame, err := ReadGraphicsFrame(bufio.NewReader(strings.NewReader(header + "\n")))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"file":{"path":"/private/source/frame.rgba"}`, `"sequence":18446744073709551615`, `"revision":8`} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("file frame lost %s: %s", field, encoded)
		}
	}
}

func TestGraphicsFileFrameRejectsStringPathAndNegativeReceipt(t *testing.T) {
	for _, header := range []string{`{"file":"/private/source/frame.rgba"}`, `{"sequence":-1}`, `{"revision":-1}`} {
		if _, err := ReadGraphicsFrame(bufio.NewReader(strings.NewReader(header + "\n"))); err == nil {
			t.Fatalf("accepted invalid public file header %s", header)
		}
	}
}

func TestGraphicsFrameHeaderBoundaryAndCompleteLine(t *testing.T) {
	for _, size := range []int{MaxGraphicsFrameHeaderBytes, MaxGraphicsFrameHeaderBytes + 1} {
		header := "{}" + strings.Repeat(" ", size-2) + "\n"
		_, err := ReadGraphicsFrame(bufio.NewReader(strings.NewReader(header)))
		if (err == nil) != (size == MaxGraphicsFrameHeaderBytes) {
			t.Fatalf("header size %d: %v", size, err)
		}
	}
	if _, err := ReadGraphicsFrame(bufio.NewReader(strings.NewReader("{}"))); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("accepted unterminated header: %v", err)
	}
	reader := bufio.NewReader(strings.NewReader("{\"data_length\":3}\nabc{\"data_length\":0}\n"))
	frame, err := ReadGraphicsFrame(reader)
	if err != nil || frame.DataLength == nil || *frame.DataLength != 3 {
		t.Fatal(frame, err)
	}
	body := make([]byte, 3)
	if _, err := io.ReadFull(reader, body); err != nil || string(body) != "abc" {
		t.Fatal("header decoder consumed body bytes", string(body), err)
	}
	frame, err = ReadGraphicsFrame(reader)
	if err != nil || frame.DataLength == nil || *frame.DataLength != 0 {
		t.Fatal("header decoder lost the next frame", frame, err)
	}
}
