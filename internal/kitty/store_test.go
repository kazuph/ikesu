package kitty

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestDirectChunkedPNGPlacementAndDeletion(t *testing.T) {
	picture := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	picture.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	picture.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 128})
	var data bytes.Buffer
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	encoded := base64.StdEncoding.EncodeToString(data.Bytes())
	cut := len(encoded) / 8 * 4
	var store Store
	response, _, _, _ := store.Handle([]byte("Ga=T,f=100,i=42,p=3,c=4,r=2,C=1,m=1;"+encoded[:cut]), 6, 4, 0, 0, false)
	if response != "" || len(store.Placements) != 0 {
		t.Fatal("incomplete chunk was acknowledged or displayed")
	}
	response, _, _, _ = store.Handle([]byte("Gm=0;"+encoded[cut:]), 6, 4, 0, 0, false)
	if response != "\x1b_Gi=42,p=3;OK\x1b\\" || len(store.Placements) != 1 || !bytes.Equal(store.Placements[0].Image.Data, data.Bytes()) {
		t.Fatalf("%q %+v", response, store)
	}
	store.Scroll(false, 0, 0, 80, 24, -5)
	store.Scroll(false, 0, 0, 80, 24, -2)
	if store.Placements[0].Y != -3 {
		t.Fatal("scrollback image anchor stopped moving", store.Placements)
	}
	store.Handle([]byte("Ga=d,d=i,i=42,p=3;"), 0, 0, 0, 0, false)
	if len(store.Placements) != 0 || len(store.Images) != 1 {
		t.Fatal("placement deletion discarded stored source", store)
	}
	store.Handle([]byte("Ga=p,i=42,c=4,r=2,C=1;"), 10, 3, 0, 0, true)
	if len(store.Visible(false)) != 0 || len(store.Visible(true)) != 1 {
		t.Fatal("alternate placement leaked")
	}
	store.Handle([]byte("Ga=d,d=I,i=42;"), 0, 0, 0, 0, true)
	if len(store.Placements) != 0 || len(store.Images) != 0 {
		t.Fatal("image deletion retained pixels", store)
	}
}

func TestUnsupportedGraphicsNeverAcknowledged(t *testing.T) {
	for _, command := range []string{
		"Ga=q,t=f,f=24,s=1,v=1,i=31;AP8A",
		"Ga=T,P=9,f=24,s=1,v=1,i=31,c=3,r=2;AP8A",
		"Ga=T,f=24,s=1,v=1,i=31,x=1,c=3,r=2;AP8A",
		"Ga=T,f=24,s=1,v=1,i=31;AP8A",
	} {
		var store Store
		response, _, _, _ := store.Handle([]byte(command), 0, 0, 0, 0, false)
		if !strings.Contains(response, "ENOTSUP:") || len(store.Images) != 0 || len(store.Placements) != 0 {
			t.Fatalf("unsupported accepted: %s %q %+v", command, response, store)
		}
	}
	if response := Reject([]byte("Ga=q,i=31,q=1;AP8A"), "ENOTSUP:disabled"); !strings.Contains(response, "ENOTSUP:disabled") {
		t.Fatal(response)
	}
	if response := Reject([]byte("Ga=q,i=31,q=2;AP8A"), "ENOTSUP:disabled"); response != "" {
		t.Fatal(response)
	}
}
