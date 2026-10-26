package main

import (
	"bytes"
	"encoding/base64"
	"testing"
)

func TestTerminalImageCompletionRequiresActualDecode(t *testing.T) {
	var replies bytes.Buffer
	var events []any
	receiver := kittyReceiver{reply: &replies, clock: monotonicNS, record: func(v any) { events = append(events, v) }}
	receiver.accept([]byte("Ga=t,f=24,s=1,v=1,i=7,m=1;" + base64.StdEncoding.EncodeToString([]byte{1, 2, 3})))
	if replies.Len() != 0 || len(receiver.images) != 0 {
		t.Fatal("incomplete image was acknowledged")
	}
	receiver.accept([]byte("Gm=0;"))
	image, ok := receiver.images[7]
	if !ok || !bytes.Equal(image.Pixels, []byte{1, 2, 3, 255}) {
		t.Fatalf("image was not decoded: %+v", image)
	}
	if replies.String() != "\x1b_Gi=7;OK\x1b\\" {
		t.Fatalf("missing completion: %q", replies.String())
	}
	receiver.accept([]byte("Ga=d,d=a;"))
	receiver.accept([]byte("Ga=p,i=7,p=1,c=1,r=1;"))
	if len(events) != 3 {
		t.Fatalf("expected decode/delete/place records, got %d", len(events))
	}
}
func TestInvalidImageCannotBeAcknowledged(t *testing.T) {
	var replies bytes.Buffer
	receiver := kittyReceiver{reply: &replies, clock: monotonicNS, record: func(any) {}}
	defer func() {
		if recover() == nil {
			t.Fatal("invalid image was accepted")
		}
		if replies.Len() != 0 {
			t.Fatal("invalid image was acknowledged")
		}
	}()
	receiver.accept([]byte("Ga=t,f=24,s=2,v=2,i=1;AQID"))
}
