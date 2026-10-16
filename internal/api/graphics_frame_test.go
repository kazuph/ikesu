package api

import (
	"bufio"
	"encoding/json"
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
