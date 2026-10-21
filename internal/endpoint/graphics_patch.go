package endpoint

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"

	"github.com/kazuph/ikesu/internal/kitty"
	"github.com/kazuph/ikesu/internal/render"
)

const GraphicsPatchCodec = "graphics.patch.v1"
const GraphicsPatchAck = "endpoint.graphics.patch-ack.v1"
const GraphicsPatchMiss = "endpoint.graphics.patch-miss.v1"

// P4b sends a complete image when the changed rectangle exceeds 60%.
const graphicsFullAreaPercent = 60

func canonicalGraphics(source kitty.Image) (kitty.Image, error) {
	if source.Format == 100 {
		config, err := png.DecodeConfig(bytes.NewReader(source.Data))
		if err != nil {
			return kitty.Image{}, err
		}
		if source.Width <= 0 || source.Height <= 0 || config.Width != source.Width || config.Height != source.Height || source.Width > int(^uint(0)>>1)/4/source.Height {
			return kitty.Image{}, fmt.Errorf("invalid PNG graphics dimensions")
		}
		decoded, err := png.Decode(bytes.NewReader(source.Data))
		if err != nil {
			return kitty.Image{}, err
		}
		if decoded.Bounds().Dx() != source.Width || decoded.Bounds().Dy() != source.Height {
			return kitty.Image{}, fmt.Errorf("PNG dimensions differ from image")
		}
		rgba := image.NewNRGBA(image.Rect(0, 0, source.Width, source.Height))
		for y := 0; y < source.Height; y++ {
			for x := 0; x < source.Width; x++ {
				c := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
				copy(rgba.Pix[rgba.PixOffset(x, y):], []byte{c.R, c.G, c.B, c.A})
			}
		}
		source.Format, source.Data = 32, rgba.Pix
		return source, nil
	}
	if source.Format != 24 && source.Format != 32 {
		return kitty.Image{}, fmt.Errorf("unsupported graphics format %d", source.Format)
	}
	channels := source.Format / 8
	pixels := len(source.Data) / channels
	if source.Width <= 0 || source.Height <= 0 || len(source.Data)%channels != 0 || source.Width > pixels/source.Height || source.Width*source.Height != pixels || pixels > int(^uint(0)>>1)/4 {
		return kitty.Image{}, fmt.Errorf("invalid raw graphics dimensions")
	}
	if channels == 4 {
		source.Data = bytes.Clone(source.Data)
		return source, nil
	}
	data := make([]byte, pixels*4)
	for i, j := 0, 0; i < len(source.Data); i, j = i+3, j+4 {
		copy(data[j:j+3], source.Data[i:i+3])
		data[j+3] = 255
	}
	source.Format, source.Data = 32, data
	return source, nil
}

func graphicsKey(pane int, source kitty.Image) render.GraphicsImageKey {
	digest := sha256.Sum256(source.Data)
	return render.GraphicsImageKey{Source: json.RawMessage(fmt.Sprintf(`{"Terminal":{"target":{"Pane":{"pane_id":"p%d"}},"image_id":%d}}`, pane, source.ID)), Width: source.Width, Height: source.Height, Format: "Rgba", DataLength: len(source.Data), Fingerprint: binary.LittleEndian.Uint64(digest[:8])}
}

func sameGraphicsKey(a, b render.GraphicsImageKey) bool {
	return bytes.Equal(a.Source, b.Source) && a.Width == b.Width && a.Height == b.Height && a.Format == b.Format && a.DataLength == b.DataLength && a.Fingerprint == b.Fingerprint
}

// makeGraphicsPatch uses a bounding rectangle over exact RGBA differences.
// A nil patch requires an ordinary full-image transmission.
func makeGraphicsPatch(pane int, previous, current kitty.Image) (*render.GraphicsImagePatch, error) {
	if current.Format != 24 && current.Format != 32 || len(current.Data) < imageCompressionMinimum {
		return nil, nil
	}
	if previous.Width != current.Width || previous.Height != current.Height || previous.ID != current.ID {
		return nil, nil
	}
	base, err := canonicalGraphics(previous)
	if err != nil {
		return nil, err
	}
	next, err := canonicalGraphics(current)
	if err != nil {
		return nil, err
	}
	left, top, right, bottom := next.Width, next.Height, 0, 0
	for y := 0; y < next.Height; y++ {
		for x := 0; x < next.Width; x++ {
			i := (y*next.Width + x) * 4
			if !bytes.Equal(base.Data[i:i+4], next.Data[i:i+4]) {
				left = min(left, x)
				top = min(top, y)
				right = max(right, x+1)
				bottom = max(bottom, y+1)
			}
		}
	}
	patch := &render.GraphicsImagePatch{Key: graphicsKey(pane, next), Base: graphicsKey(pane, base), Rects: []render.GraphicsPatchRect{}}
	if right == 0 {
		return patch, nil
	}
	area, pixels := (right-left)*(bottom-top), next.Width*next.Height
	if area > pixels/100*graphicsFullAreaPercent+pixels%100*graphicsFullAreaPercent/100 {
		return nil, nil
	}
	rgba := &image.NRGBA{Pix: next.Data, Stride: next.Width * 4, Rect: image.Rect(0, 0, next.Width, next.Height)}
	var data bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&data, rgba.SubImage(image.Rect(left, top, right, bottom))); err != nil {
		return nil, err
	}
	patch.Rects = append(patch.Rects, render.GraphicsPatchRect{X: left, Y: top, Width: right - left, Height: bottom - top, PNG: data.Bytes()})
	return patch, nil
}

func applyGraphicsPatch(pane int, previous kitty.Image, patch render.GraphicsImagePatch) (kitty.Image, error) {
	base, err := canonicalGraphics(previous)
	if err != nil {
		return kitty.Image{}, err
	}
	if !sameGraphicsKey(graphicsKey(pane, base), patch.Base) {
		return kitty.Image{}, fmt.Errorf("graphics patch base unavailable")
	}
	if patch.Key.Width != base.Width || patch.Key.Height != base.Height || patch.Key.Format != "Rgba" || patch.Key.DataLength != len(base.Data) || !bytes.Equal(patch.Key.Source, patch.Base.Source) {
		return kitty.Image{}, fmt.Errorf("graphics patch key dimensions or source changed")
	}
	for _, rect := range patch.Rects {
		if rect.X < 0 || rect.Y < 0 || rect.Width <= 0 || rect.Height <= 0 || rect.X > base.Width || rect.Y > base.Height || rect.Width > base.Width-rect.X || rect.Height > base.Height-rect.Y {
			return kitty.Image{}, fmt.Errorf("graphics patch rectangle outside image")
		}
		decoded, err := canonicalGraphics(kitty.Image{Format: 100, Width: rect.Width, Height: rect.Height, Data: rect.PNG})
		if err != nil {
			return kitty.Image{}, err
		}
		for y := 0; y < rect.Height; y++ {
			offset := ((rect.Y+y)*base.Width + rect.X) * 4
			copy(base.Data[offset:offset+rect.Width*4], decoded.Data[y*rect.Width*4:(y+1)*rect.Width*4])
		}
	}
	if !sameGraphicsKey(graphicsKey(pane, base), patch.Key) {
		return kitty.Image{}, fmt.Errorf("graphics patch pixels differ from key")
	}
	return base, nil
}
