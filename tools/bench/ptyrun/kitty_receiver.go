package main

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
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
	Points        []image.Point
	Colors        map[string]int
}
type kittyReceiver struct {
	images       map[int]receivedImage
	pending      []byte
	header       map[string]string
	reply        io.Writer
	record       func(any)
	clock        func() int64
	cursor       func() (int, int)
	cellW, cellH int
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
		points := []image.Point{}
		colors := map[string]int{}
		for i := 0; i < len(pixels); i += 4 {
			if pixels[i+3] != 0 {
				points = append(points, image.Pt(i/4%width, i/4/width))
				colors[fmt.Sprintf("%02x%02x%02x%02x", pixels[i], pixels[i+1], pixels[i+2], pixels[i+3])]++
			}
		}
		k.images[id] = receivedImage{Width: width, Height: height, Pixels: pixels, Hash: hash, Points: points, Colors: colors}
		k.record(map[string]any{"type": "image.decoded", "clock_ns": k.clock(), "id": id, "width": width, "height": height, "rgba_bytes": len(pixels), "sha256": hash, "opaque_colors": colors, "parsed_ns": monotonicNS()})
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
	event := map[string]any{"type": "image.placed", "clock_ns": k.clock(), "parsed_ns": monotonicNS(), "id": id, "sha256": image.Hash, "control": fields}
	if k.cursor != nil && k.cellW > 0 && k.cellH > 0 {
		cx, cy := k.cursor()
		value := func(key string, def int) int {
			if text, ok := fields[key]; ok {
				n, err := strconv.Atoi(text)
				if err == nil {
					return n
				}
			}
			return def
		}
		sx, sy := value("x", 0), value("y", 0)
		sw, sh := value("w", image.Width-sx), value("h", image.Height-sy)
		w, h := value("c", 0)*k.cellW, value("r", 0)*k.cellH
		if w == 0 {
			w = sw
		}
		if h == 0 {
			h = sh
		}
		x, y := cx*k.cellW+value("X", 0), cy*k.cellH+value("Y", 0)
		event["cursor_cell"] = []int{cx, cy}
		event["display_px"] = []int{x, y, w, h}
		event["source_px"] = []int{sx, sy, sw, sh}
		if sw > 0 && sh > 0 {
			centerX, centerY := float64(x)+(float64(image.Width)/2-float64(sx))*float64(w)/float64(sw), float64(y)+(float64(image.Height)/2-float64(sy))*float64(h)/float64(sh)
			event["image_center_px"] = []float64{centerX, centerY}
			// For the single-color ripple, reconstruct its actually visible alpha dots,
			// including source crops introduced by the containing terminal.
			if len(image.Colors) == 1 {
				radius := 0.
				bounds := []float64{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
				count := 0
				for _, point := range image.Points {
					if point.X < sx || point.Y < sy || point.X >= sx+sw || point.Y >= sy+sh {
						continue
					}
					px := float64(x) + (float64(point.X-sx)+.5)*float64(w)/float64(sw)
					py := float64(y) + (float64(point.Y-sy)+.5)*float64(h)/float64(sh)
					radius = math.Max(radius, math.Hypot(px-centerX, py-centerY))
					bounds[0] = math.Min(bounds[0], px)
					bounds[1] = math.Min(bounds[1], py)
					bounds[2] = math.Max(bounds[2], px)
					bounds[3] = math.Max(bounds[3], py)
					count++
				}
				if count > 0 {
					event["visible_alpha_box_px"] = bounds
				}
				event["visible_radius_px"] = radius
				event["visible_alpha_dots"] = count
			}
		}
	}
	k.record(event)
}
