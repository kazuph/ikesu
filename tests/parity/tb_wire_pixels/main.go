// tb_wire_pixels checks actual saved-machine captures against producer hashes.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/kazuph/ikesu/internal/endpoint"
)

type record struct {
	Bytes      int    `json:"bytes"`
	SHA256     string `json:"sha256"`
	Format     int    `json:"format"`
	Patch      bool   `json:"patch"`
	WireEnd    int64  `json:"wire_end"`
	TimeUnixMs int64  `json:"time_unix_ms,omitempty"`
}

func rgba(data []byte, format, width, height int) ([]byte, error) {
	if format == 100 {
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		if img.Bounds().Dx() != width || img.Bounds().Dy() != height {
			return nil, fmt.Errorf("PNG dimensions changed")
		}
		out := make([]byte, width*height*4)
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				copy(out[(y*width+x)*4:], []byte{c.R, c.G, c.B, c.A})
			}
		}
		return out, nil
	}
	if format == 32 && len(data) == width*height*4 {
		return data, nil
	}
	return nil, fmt.Errorf("unsupported raw image format or dimensions: %d", format)
}

func snapshots(data []byte) ([]record, error) {
	var decoder endpoint.SnapshotDecoder
	var records []record
	stream := json.NewDecoder(bytes.NewReader(data))
	for {
		var message struct {
			Type          string
			Data          []byte
			SurfaceCodecs []string `json:"surface_codecs"`
		}
		err := stream.Decode(&message)
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if message.Type == "welcome" {
			decoder.GraphicsPatches = endpoint.HasGraphicsPatches(endpoint.Peer{SurfaceCodecs: message.SurfaceCodecs})
		}
		if message.Type != "snapshot" {
			continue
		}
		in, err := decoder.Decode(message.Data)
		if err != nil {
			return nil, err
		}
		for paneID, pane := range in.Panes {
			for index, placement := range pane.Graphics {
				img := placement.Image
				pixels, err := rgba(img.Data, img.Format, img.Width, img.Height)
				if err != nil {
					return nil, err
				}
				count, exists := decoder.GraphicsPayloadSize(paneID, index)
				if !exists {
					return nil, fmt.Errorf("no wire image accounting for pane %d placement %d", paneID, index)
				}
				patched := false
				for _, item := range in.GraphicsPatches {
					if item.Pane == paneID && item.Placement == index {
						patched = true
					}
				}
				end := stream.InputOffset()
				if end < int64(len(data)) && data[end] == '\n' {
					end++
				}
				records = append(records, record{Bytes: count, SHA256: fmt.Sprintf("%x", sha256.Sum256(pixels)), Format: img.Format, Patch: patched, WireEnd: end})
			}
		}
	}
}

func unsigned(reader *bytes.Reader) (uint64, error) {
	value, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}
	if value < 251 {
		return uint64(value), nil
	}
	width := map[byte]int{251: 2, 252: 4, 253: 8}[value]
	if width == 0 {
		return 0, fmt.Errorf("unsupported length tag %d", value)
	}
	var buf [8]byte
	_, err = io.ReadFull(reader, buf[:width])
	return binary.LittleEndian.Uint64(buf[:]), err
}

func field(reader *bytes.Reader) ([]byte, error) {
	length, err := unsigned(reader)
	if err != nil {
		return nil, err
	}
	if length > uint64(reader.Len()) {
		return nil, io.ErrUnexpectedEOF
	}
	data := make([]byte, int(length))
	_, err = io.ReadFull(reader, data)
	return data, err
}

func patches(data []byte) ([]record, error) {
	reader := bytes.NewReader(bytes.TrimPrefix(data, []byte("herdr-saved-ssh-bridge-ready\n")))
	var records []record
	var canvas *image.NRGBA
	var previousKey json.RawMessage
	for reader.Len() > 0 {
		var size uint32
		if err := binary.Read(reader, binary.LittleEndian, &size); err != nil {
			return nil, err
		}
		if uint64(size) > uint64(reader.Len()) {
			return nil, io.ErrUnexpectedEOF
		}
		body := make([]byte, int(size))
		if _, err := io.ReadFull(reader, body); err != nil {
			return nil, err
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("empty frame")
		}
		if body[0] != 20 {
			continue
		}
		frame := bytes.NewReader(body[1:])
		name, err := field(frame)
		if err != nil {
			return nil, err
		}
		payload, err := field(frame)
		if err != nil {
			return nil, err
		}
		if frame.Len() != 0 {
			return nil, fmt.Errorf("trailing blob bytes")
		}
		if string(name) != "endpoint.graphics.patch.v1" {
			if bytes.HasPrefix(name, []byte("endpoint.graphics.")) {
				return nil, fmt.Errorf("unsupported graphics blob %q; image accounting is incomplete", name)
			}
			continue
		}
		var patch struct {
			Key, Base json.RawMessage
			Rects     []struct {
				X, Y, Width, Height int
				PNG                 []byte
			}
		}
		if err := json.Unmarshal(payload, &patch); err != nil {
			return nil, err
		}
		var key struct {
			Width  int `json:"image_width"`
			Height int `json:"image_height"`
		}
		if err := json.Unmarshal(patch.Key, &key); err != nil {
			return nil, err
		}
		if bytes.Equal(patch.Key, patch.Base) {
			canvas = image.NewNRGBA(image.Rect(0, 0, key.Width, key.Height))
		} else if canvas == nil || !bytes.Equal(previousKey, patch.Base) {
			return nil, fmt.Errorf("patch base unavailable")
		}
		count := 0
		for _, rect := range patch.Rects {
			pixels, err := rgba(rect.PNG, 100, rect.Width, rect.Height)
			if err != nil {
				return nil, err
			}
			if !image.Rect(rect.X, rect.Y, rect.X+rect.Width, rect.Y+rect.Height).In(canvas.Rect) {
				return nil, fmt.Errorf("patch outside image")
			}
			for y := 0; y < rect.Height; y++ {
				copy(canvas.Pix[canvas.PixOffset(rect.X, rect.Y+y):], pixels[y*rect.Width*4:(y+1)*rect.Width*4])
			}
			count += len(rect.PNG)
		}
		previousKey = patch.Key
		records = append(records, record{Bytes: count, SHA256: fmt.Sprintf("%x", sha256.Sum256(canvas.Pix)), Format: 100, Patch: true, WireEnd: int64(len(data) - reader.Len())})
	}
	return records, nil
}

func run(root string, actualTB, windowed bool) error {
	var expected []struct{ Stage, SHA256 string }
	var data []byte
	if actualTB {
		input, err := os.ReadFile(filepath.Join(root, "inputs.json"))
		if err != nil {
			return err
		}
		var parameters struct {
			Measure    bool
			ImageProbe bool `json:"image_probe"`
		}
		if err := json.Unmarshal(input, &parameters); err != nil {
			return err
		}
		if !parameters.Measure || parameters.ImageProbe {
			return fmt.Errorf("actual-tb accounting requires an actual tb measurement")
		}
	} else {
		var err error
		data, err = os.ReadFile(filepath.Join(root, "remote-image-probe.json"))
		if err != nil {
			return err
		}
		if err = json.Unmarshal(data, &expected); err != nil {
			return err
		}
	}
	files, err := filepath.Glob(filepath.Join(root, "ssh", "*-from-mini.bin"))
	if err != nil {
		return err
	}
	var records []record
	wireBytes := 0
	capturedImages := 0
	tailBytes := 0
	var window measureWindow
	if windowed {
		window, err = readWindow(root)
		if err != nil {
			return err
		}
	}
	for _, file := range files {
		data, err = os.ReadFile(file)
		if err != nil {
			return err
		}
		if len(data) == 0 {
			continue
		}
		// The SSH observer also records real platform/path/session discovery.
		// Only the display connection has a welcome message or bridge marker.
		if !bytes.HasPrefix(data, []byte("herdr-saved-ssh-bridge-ready\n")) {
			first, _, _ := bytes.Cut(data, []byte{'\n'})
			var hello struct{ Type string }
			if json.Unmarshal(first, &hello) != nil || hello.Type != "welcome" {
				continue
			}
		}
		wireBytes += len(data)
		var chunks []captureChunk
		if windowed {
			chunks, err = readCaptureClock(file)
			if err != nil {
				return err
			}
			var tail int
			data, tail, err = windowPrefix(data, chunks, window.End)
			if err != nil {
				return err
			}
			tailBytes += tail
			if len(data) == 0 {
				return fmt.Errorf("no complete display message by measurement end")
			}
		}
		var rows []record
		if data[0] == '{' {
			rows, err = snapshots(data)
		} else {
			rows, err = patches(data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		capturedImages += len(rows)
		for _, row := range rows {
			if windowed {
				if err := recordTime(&row, chunks); err != nil {
					return err
				}
				if row.TimeUnixMs < window.Start || row.TimeUnixMs > window.End {
					continue
				}
			}
			records = append(records, row)
		}
	}
	seen := map[string]bool{}
	imageBytes := 0
	for _, row := range records {
		seen[row.SHA256] = true
		imageBytes += row.Bytes
	}
	for _, want := range expected {
		if !seen[want.SHA256] {
			return fmt.Errorf("producer stage %s pixels missing: %s", want.Stage, want.SHA256)
		}
	}
	if !actualTB && len(expected) == 0 {
		return fmt.Errorf("no producer stages")
	}
	if capturedImages == 0 {
		return fmt.Errorf("no decoded image records")
	}
	var verified *bool
	if !actualTB {
		value := true
		verified = &value
	}
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	scope := "full connection; image bytes are PNG/raw payloads, excluding encoding and metadata; actual tb has no producer hash assertion"
	if !actualTB {
		scope = "full connection; image bytes are PNG/raw payloads, excluding encoding and metadata; every expected producer stage is verified by its RGBA SHA256"
	}
	if windowed {
		scope = "completed image payloads received during scroll/switch measurement; incomplete messages excluded; WireBytes counts the full capture; no producer hash assertion"
	}
	return encoder.Encode(struct {
		Scope                       string `json:"scope"`
		WireBytes, ImageBytes       int
		AllProducerStagesMatch      *bool
		Records                     []record
		ExcludedIncompleteTailBytes int
		Window                      measureWindow
	}{scope, wireBytes, imageBytes, verified, records, tailBytes, window})
}

func main() {
	actualTB := flag.Bool("actual-tb", false, "count actual tb capture without claiming known producer pixels")
	windowed := flag.Bool("measurement-window", false, "count only completed image payloads in the measured scroll/switch window")
	flag.Parse()
	if *windowed && !*actualTB {
		fmt.Fprintln(os.Stderr, "measurement-window requires actual-tb")
		os.Exit(2)
	}
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: tb_wire_pixels [-actual-tb [-measurement-window]] CAPTURE_DIRECTORY")
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *actualTB, *windowed); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
