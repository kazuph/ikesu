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

func TestReceivedPlacementUsesCursorCropAndSourcePixels(t *testing.T) {
	var placement map[string]any
	receiver := kittyReceiver{clock: func() int64 { return 123 }, cursor: func() (int, int) { return 3, 4 }, cellW: 8, cellH: 16, record: func(v any) {
		e := v.(map[string]any)
		if e["type"] == "image.placed" {
			placement = e
		}
	}}
	receiver.accept([]byte("Ga=t,f=32,s=2,v=2,i=7,q=2;" + base64.StdEncoding.EncodeToString([]byte{255, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0, 255})))
	receiver.accept([]byte("Ga=p,i=7,p=2,x=1,y=0,w=1,h=2,c=1,r=1,X=2,Y=3;"))
	if placement["clock_ns"] != int64(123) {
		t.Fatalf("lost receive time: %v", placement)
	}
	display := placement["display_px"].([]int)
	if display[0] != 26 || display[1] != 67 || display[2] != 8 || display[3] != 16 {
		t.Fatalf("wrong received placement: %v", placement)
	}
	center := placement["image_center_px"].([]float64)
	if center[0] != 26 || center[1] != 75 || placement["visible_alpha_dots"] != 2 {
		t.Fatalf("lost cropped source geometry: %v", placement)
	}
}
