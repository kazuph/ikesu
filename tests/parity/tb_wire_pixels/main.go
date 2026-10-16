// tb_wire_pixels checks actual saved-machine captures against producer hashes.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/kazuph/goterrace/internal/endpoint"
)

type record struct {
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
	Format int    `json:"format"`
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
			Type string
			Data []byte
		}
		err := stream.Decode(&message)
		if err == io.EOF {
			return records, nil
		}
		if err != nil {
			return nil, err
		}
		if message.Type != "snapshot" {
			continue
		}
		in, err := decoder.Decode(message.Data)
		if err != nil {
			return nil, err
		}
		for _, pane := range in.Panes {
			for _, placement := range pane.Graphics {
				img := placement.Image
				pixels, err := rgba(img.Data, img.Format, img.Width, img.Height)
				if err != nil {
					return nil, err
				}
				records = append(records, record{len(img.Data), fmt.Sprintf("%x", sha256.Sum256(pixels)), img.Format})
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
		records = append(records, record{count, fmt.Sprintf("%x", sha256.Sum256(canvas.Pix)), 100})
	}
	return records, nil
}

func run(root string) error {
	var expected []struct{ Stage, SHA256 string }
	data, err := os.ReadFile(filepath.Join(root, "remote-image-probe.json"))
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &expected); err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(root, "ssh", "*-from-mini.bin"))
	if err != nil {
		return err
	}
	var records []record
	wireBytes := 0
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
		var rows []record
		if data[0] == '{' {
			rows, err = snapshots(data)
		} else {
			rows, err = patches(data)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		records = append(records, rows...)
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
	if len(expected) == 0 {
		return fmt.Errorf("no producer stages")
	}
	writer := bufio.NewWriter(os.Stdout)
	defer writer.Flush()
	encoder := json.NewEncoder(writer)
	encoder.SetIndent("", "  ")
	return encoder.Encode(struct {
		Scope                  string `json:"scope"`
		WireBytes, ImageBytes  int
		AllProducerStagesMatch bool
		Records                []record
	}{"full connection; image bytes are PNG/raw payloads, excluding encoding and metadata", wireBytes, imageBytes, true, records})
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: tb_wire_pixels CAPTURE_DIRECTORY")
		os.Exit(2)
	}
	if err := run(os.Args[1]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
