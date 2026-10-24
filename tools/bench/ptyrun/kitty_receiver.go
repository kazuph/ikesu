package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"os"
	"strconv"
	"strings"
)

// This terminal receiver validates and retains real image bytes before issuing
// completion replies. Unknown transports/formats fail explicitly; consuming an
// escape sequence alone must not masquerade as image processing completion.
type receivedImage struct {
	Width, Height int
	Pixels        []byte
	Hash          string
}
type kittyReceiver struct {
	images  map[int]receivedImage
	pending []byte
	header  map[string]string
	reply   io.Writer
	record  func(any)
	clock   func() int64
}

func (k *kittyReceiver) accept(data []byte) bool {
	if len(data) == 0 || data[0] != 'G' {
		return false
	}
	header, payload, _ := strings.Cut(string(data[1:]), ";")
	fields := map[string]string{}
	for _, field := range strings.Split(header, ",") {
		key, value, ok := strings.Cut(field, "=")
		if ok {
			fields[key] = value
		}
	}
	number := func(key string) int { n, _ := strconv.Atoi(fields[key]); return n }
	action := fields["a"]
	if action == "" {
		action = "t"
	}
	if k.header != nil {
		for key, value := range k.header {
			if _, ok := fields[key]; !ok && key != "m" {
				fields[key] = value
			}
		}
		action = fields["a"]
		if action == "" {
			action = "t"
		}
	}
	id := number("i")
	if k.images == nil {
		k.images = map[int]receivedImage{}
	}
	switch action {
	case "t", "T":
		chunk, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			panic(fmt.Errorf("invalid terminal image base64: %w", err))
		}
		k.pending = append(k.pending, chunk...)
		if fields["m"] == "1" {
			k.header = fields
			return true
		}
		encoded := k.pending
		k.pending = nil
		k.header = nil
		transport := fields["t"]
		if transport != "" && transport != "d" {
			if transport != "f" {
				panic("UNSUPPORTED terminal image transport: " + transport)
			}
			encoded, err = os.ReadFile(string(encoded))
			if err != nil {
				panic(err)
			}
		}
		if fields["o"] != "" {
			if fields["o"] != "z" {
				panic("UNSUPPORTED terminal image compression")
			}
			reader, err := zlib.NewReader(bytes.NewReader(encoded))
			if err != nil {
				panic(err)
			}
			encoded, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				panic(err)
			}
		}
		width, height := number("s"), number("v")
		format := number("f")
		if format == 0 {
			format = 32
		}
		var pixels []byte
		switch format {
		case 24, 32:
			channels := format / 8
			if width <= 0 || height <= 0 || len(encoded) != width*height*channels {
				panic("invalid terminal image dimensions/length")
			}
			pixels = make([]byte, width*height*4)
			for i := 0; i < width*height; i++ {
				copy(pixels[i*4:i*4+channels], encoded[i*channels:(i+1)*channels])
				if channels == 3 {
					pixels[i*4+3] = 255
				}
			}
		case 100:
			decoded, err := png.Decode(bytes.NewReader(encoded))
			if err != nil {
				panic(err)
			}
			width, height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
			pixels = make([]byte, width*height*4)
			for y := 0; y < height; y++ {
				for x := 0; x < width; x++ {
					c := color.NRGBAModel.Convert(decoded.At(x+decoded.Bounds().Min.X, y+decoded.Bounds().Min.Y)).(color.NRGBA)
					copy(pixels[(y*width+x)*4:], []byte{c.R, c.G, c.B, c.A})
				}
			}
		default:
			panic(fmt.Sprintf("UNSUPPORTED terminal image format: %d", format))
		}
		if id <= 0 {
			panic("UNSUPPORTED terminal image without explicit ID")
		}
		hash := fmt.Sprintf("%x", sha256.Sum256(pixels))
		k.images[id] = receivedImage{width, height, pixels, hash}
		k.record(map[string]any{"type": "image.decoded", "clock_ns": k.clock(), "id": id, "width": width, "height": height, "rgba_bytes": len(pixels), "sha256": hash})
		if action == "T" {
			k.place(id, fields)
		}
		if (fields["q"] == "" || fields["q"] == "0") && k.reply != nil {
			fmt.Fprintf(k.reply, "\x1b_Gi=%d;OK\x1b\\", id)
		}
	case "p":
		k.place(id, fields)
	case "d":
		if fields["d"] == "A" {
			clear(k.images)
		} else if fields["d"] == "I" {
			delete(k.images, id)
		}
		k.record(map[string]any{"type": "image.deleted", "clock_ns": k.clock(), "control": fields})
	default:
		panic("UNSUPPORTED terminal image action: " + action)
	}
	return true
}
func (k *kittyReceiver) place(id int, fields map[string]string) {
	image, ok := k.images[id]
	if !ok {
		panic(fmt.Sprintf("placement of unknown terminal image %d", id))
	}
	k.record(map[string]any{"type": "image.placed", "clock_ns": k.clock(), "id": id, "sha256": image.Hash, "control": fields})
}
