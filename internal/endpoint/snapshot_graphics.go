package endpoint

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/png"

	"github.com/kazuph/goterrace/internal/kitty"
	"github.com/kazuph/goterrace/internal/render"
)

// P4b starts lossless compression at 512 KiB; non-patch viewers keep raw
// pixels unless the PNG is less than half the original size (stage8 T1).
const imageCompressionMinimum = 512 * 1024

type graphicCacheKey struct {
	pane  int
	image uint32
}

type compressedGraphic struct {
	fingerprint           [32]byte
	width, height, format int
	wire                  kitty.Image
}

func wireGraphics(cache *map[graphicCacheKey]compressedGraphic, pane int, graphics []kitty.Placement, alive map[graphicCacheKey]bool) ([]kitty.Placement, error) {
	result := graphics
	copied := false
	for i, placement := range graphics {
		source := placement.Image
		if len(source.Data) < imageCompressionMinimum || source.Format != 24 && source.Format != 32 {
			continue
		}
		key := graphicCacheKey{pane: pane, image: source.ID}
		alive[key] = true
		fingerprint := sha256.Sum256(source.Data)
		cached, exists := (*cache)[key]
		if !exists || cached.fingerprint != fingerprint || cached.width != source.Width || cached.height != source.Height || cached.format != source.Format {
			wire, err := compressGraphic(source)
			if err != nil {
				return nil, err
			}
			cached = compressedGraphic{fingerprint: fingerprint, width: source.Width, height: source.Height, format: source.Format, wire: wire}
			if *cache == nil {
				*cache = map[graphicCacheKey]compressedGraphic{}
			}
			(*cache)[key] = cached
		}
		if cached.wire.Format != source.Format {
			if !copied {
				result = append([]kitty.Placement(nil), graphics...)
				copied = true
			}
			result[i].Image = cached.wire
		}
	}
	return result, nil
}

// PrepareGraphicsForDisplay runs on the endpoint reader, after reconstruction.
// Canonical patch bases remain raw; the terminal receives the same lossless
// compression policy as the wire, without recompressing unchanged images.
func (d *SnapshotDecoder) PrepareGraphicsForDisplay(in *render.Input) error {
	alive := map[graphicCacheKey]bool{}
	for pane, snapshot := range in.Panes {
		graphics, err := wireGraphics(&d.displayGraphics, pane, snapshot.Graphics, alive)
		if err != nil {
			return err
		}
		snapshot.Graphics = graphics
	}
	for key := range d.displayGraphics {
		if !alive[key] {
			delete(d.displayGraphics, key)
		}
	}
	return nil
}

func compressGraphic(source kitty.Image) (kitty.Image, error) {
	channels := source.Format / 8
	pixels := len(source.Data) / channels
	if source.Width <= 0 || source.Height <= 0 || len(source.Data)%channels != 0 || source.Width > pixels/source.Height || source.Width*source.Height != pixels || pixels > int(^uint(0)>>1)/4 {
		return kitty.Image{}, fmt.Errorf("invalid raw graphics dimensions")
	}
	rgba := &image.NRGBA{Pix: source.Data, Stride: source.Width * 4, Rect: image.Rect(0, 0, source.Width, source.Height)}
	if channels == 3 {
		rgba = image.NewNRGBA(rgba.Rect)
		for input, output := 0, 0; input < len(source.Data); input, output = input+3, output+4 {
			copy(rgba.Pix[output:output+3], source.Data[input:input+3])
			rgba.Pix[output+3] = 255
		}
	}
	var encoded bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&encoded, rgba); err != nil {
		return kitty.Image{}, err
	}
	if uint64(encoded.Len())*2 < uint64(len(source.Data)) {
		source.Format, source.Data = 100, encoded.Bytes()
	}
	return source, nil
}
