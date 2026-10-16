package kitty

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"fmt"
	"image/png"
	"io"
	"strconv"
	"strings"
)

// Images retain source pixels; placements retain terminal cell coordinates.
// Outer terminal IDs are assigned by the renderer, never copied from a pane.
type Image struct {
	ID                    uint32
	Format, Width, Height int
	Data                  []byte
}
type Placement struct {
	Image               Image
	ID                  uint32
	X, Y, Cols, Rows, Z int
	Alternate           bool
	Virtual             bool
}
type Store struct {
	Images     map[uint32]Image
	Placements []Placement
	NextID     uint32
	ChunkKeys  map[string]string
	ChunkData  []byte
}

func number(keys map[string]string, key string, fallback int) int {
	if value, ok := keys[key]; ok {
		n, err := strconv.Atoi(value)
		if err == nil {
			return n
		}
		return -1
	}
	return fallback
}

// Handle consumes one complete graphics APC. Unsupported procedures return
// ENOTSUP instead of accepting a command that cannot be rendered faithfully.
func (s *Store) Handle(data []byte, x, y, cellW, cellH int, alt bool) (string, int, int, bool) {
	if len(data) == 0 || data[0] != 'G' {
		return "", 0, 0, false
	}
	header, encoded, _ := bytes.Cut(data[1:], []byte(";"))
	keys := map[string]string{}
	for _, field := range strings.Split(string(header), ",") {
		k, v, ok := strings.Cut(field, "=")
		if !ok || len(k) != 1 {
			return "", 0, 0, true
		}
		keys[k] = v
	}
	if s.ChunkKeys != nil {
		for k, v := range keys {
			if k != "m" && k != "q" && s.ChunkKeys[k] != v {
				s.ChunkKeys = nil
				s.ChunkData = nil
				return reply(keys, "EINVAL:chunk metadata changed"), 0, 0, true
			}
		}
		for k, v := range s.ChunkKeys {
			if _, ok := keys[k]; !ok {
				keys[k] = v
			}
		}
	}
	payload, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		s.ChunkKeys = nil
		s.ChunkData = nil
		return reply(keys, "EINVAL:invalid base64"), 0, 0, true
	}
	payload = append(s.ChunkData, payload...)
	if number(keys, "m", 0) == 1 {
		delete(keys, "m")
		s.ChunkKeys, s.ChunkData = keys, payload
		return "", 0, 0, true
	}
	s.ChunkKeys, s.ChunkData = nil, nil
	for k := range keys {
		if !strings.Contains("atifvsmqpcrzCdoU", k) {
			return reply(keys, "ENOTSUP:unsupported graphics key "+k), 0, 0, true
		}
		if strings.Contains("ifvsmqpcrCU", k) {
			if _, err := strconv.ParseUint(keys[k], 10, 32); err != nil {
				return reply(keys, "EINVAL:invalid numeric graphics key "+k), 0, 0, true
			}
		}
		if k == "z" {
			if _, err := strconv.ParseInt(keys[k], 10, 32); err != nil {
				return reply(keys, "EINVAL:invalid z index"), 0, 0, true
			}
		}
	}
	action := keys["a"]
	if action == "" {
		action = "t"
	}
	id64, err := strconv.ParseUint(keys["i"], 10, 32)
	if keys["i"] == "" {
		err = nil
	}
	if err != nil {
		return reply(keys, "EINVAL:invalid image id"), 0, 0, true
	}
	id := uint32(id64)
	if s.Images == nil {
		s.Images = map[uint32]Image{}
	}
	if action == "d" {
		mode := keys["d"]
		if mode == "" {
			mode = "a"
		}
		if mode != "a" && mode != "A" && mode != "i" && mode != "I" {
			return reply(keys, "ENOTSUP:unsupported deletion"), 0, 0, true
		}
		pid := uint32(number(keys, "p", 0))
		kept := s.Placements[:0]
		for _, p := range s.Placements {
			remove := (mode == "a" || mode == "A") && !p.Virtual && p.Alternate == alt || (mode == "i" || mode == "I") && p.Image.ID == id && (pid == 0 || p.ID == pid)
			if !remove {
				kept = append(kept, p)
			}
		}
		s.Placements = kept
		if mode == "I" {
			delete(s.Images, id)
		}
		if mode == "A" {
			for imageID := range s.Images {
				used := false
				for _, p := range kept {
					used = used || p.Image.ID == imageID
				}
				if !used {
					delete(s.Images, imageID)
				}
			}
		}
		return "", 0, 0, true
	}
	if action != "t" && action != "T" && action != "p" && action != "q" {
		return reply(keys, "ENOTSUP:unsupported graphics action"), 0, 0, true
	}
	image := s.Images[id]
	if action != "p" {
		if medium := keys["t"]; medium != "" && medium != "d" {
			return reply(keys, "ENOTSUP:only direct image data is supported"), 0, 0, true
		}
		if compression := keys["o"]; compression != "" {
			if compression != "z" {
				return reply(keys, "ENOTSUP:unsupported compression"), 0, 0, true
			}
			reader, e := zlib.NewReader(bytes.NewReader(payload))
			if e != nil {
				return reply(keys, "EINVAL:invalid zlib data"), 0, 0, true
			}
			payload, err = io.ReadAll(reader)
			reader.Close()
			if err != nil {
				return reply(keys, "EINVAL:invalid zlib data"), 0, 0, true
			}
		}
		image = Image{ID: id, Format: number(keys, "f", 32), Width: number(keys, "s", 0), Height: number(keys, "v", 0), Data: payload}
		if image.Format == 100 {
			decoded, e := png.Decode(bytes.NewReader(payload))
			if e != nil {
				return reply(keys, "EINVAL:invalid PNG"), 0, 0, true
			}
			image.Width, image.Height = decoded.Bounds().Dx(), decoded.Bounds().Dy()
		} else if image.Format != 24 && image.Format != 32 {
			return reply(keys, "ENOTSUP:unsupported image format"), 0, 0, true
		}
		if image.Width <= 0 || image.Height <= 0 {
			return reply(keys, "EINVAL:missing image dimensions"), 0, 0, true
		}
		if image.Format != 100 && (len(payload)%(image.Format/8) != 0 || uint64(image.Width)*uint64(image.Height) != uint64(len(payload)/(image.Format/8))) {
			return reply(keys, "EINVAL:incorrect pixel data size"), 0, 0, true
		}
	}
	if action == "q" {
		return reply(keys, "OK"), 0, 0, true
	}
	if action == "p" && image.Width == 0 {
		return reply(keys, "ENOENT:image not found"), 0, 0, true
	}
	cols, rows := number(keys, "c", 0), number(keys, "r", 0)
	virtual := number(keys, "U", 0)
	if virtual < 0 || virtual > 1 {
		return reply(keys, "EINVAL:invalid virtual placement"), 0, 0, true
	}
	if action == "T" || action == "p" {
		if cols < 0 || rows < 0 || number(keys, "C", 0) < 0 || number(keys, "C", 0) > 1 {
			return reply(keys, "EINVAL:invalid placement"), 0, 0, true
		}
		if cols == 0 || rows == 0 {
			if cellW <= 0 || cellH <= 0 {
				return reply(keys, "ENOTSUP:placement requires cell pixel dimensions or both c and r"), 0, 0, true
			}
			if cols == 0 && rows == 0 {
				cols = (image.Width + cellW - 1) / cellW
				rows = (image.Height + cellH - 1) / cellH
			} else if cols == 0 {
				cols = (rows*cellH*image.Width + image.Height*cellW - 1) / (image.Height * cellW)
			} else {
				rows = (cols*cellW*image.Height + image.Width*cellH - 1) / (image.Width * cellH)
			}
		}
	}
	if action != "p" {
		if id == 0 {
			s.NextID++
			for s.NextID == 0 || s.Images[s.NextID].Width != 0 {
				s.NextID++
			}
			id = s.NextID
			image.ID = id
		}
		kept := s.Placements[:0]
		for _, p := range s.Placements {
			if p.Image.ID != id {
				kept = append(kept, p)
			} else if action == "t" {
				p.Image = image
				kept = append(kept, p)
			}
		}
		s.Placements = kept
		s.Images[id] = image
	}
	if action == "T" || action == "p" {
		pid := uint32(number(keys, "p", 0))
		if pid != 0 {
			kept := s.Placements[:0]
			for _, p := range s.Placements {
				if p.Image.ID != id || p.ID != pid {
					kept = append(kept, p)
				}
			}
			s.Placements = kept
		}
		s.Placements = append(s.Placements, Placement{Image: image, ID: pid, X: x, Y: y, Cols: cols, Rows: rows, Z: number(keys, "z", 0), Alternate: alt, Virtual: virtual == 1})
		if virtual == 0 && number(keys, "C", 0) == 0 {
			return reply(keys, "OK"), cols, rows, true
		}
	}
	return reply(keys, "OK"), 0, 0, true
}

func reply(keys map[string]string, message string) string {
	if keys["q"] == "2" || keys["q"] == "1" && message == "OK" {
		return ""
	}
	id := keys["i"]
	if id == "" || id == "0" {
		return ""
	}
	fields := "i=" + id
	if keys["p"] != "" {
		fields += ",p=" + keys["p"]
	}
	return fmt.Sprintf("\x1b_G%s;%s\x1b\\", fields, message)
}

func Reject(data []byte, message string) string {
	if len(data) == 0 || data[0] != 'G' {
		return ""
	}
	header, _, _ := bytes.Cut(data[1:], []byte(";"))
	keys := map[string]string{}
	for _, field := range strings.Split(string(header), ",") {
		k, v, ok := strings.Cut(field, "=")
		if ok {
			keys[k] = v
		}
	}
	return reply(keys, message)
}

func (s *Store) Scroll(alt bool, left, top, right, bottom, lines int) {
	for i := range s.Placements {
		p := &s.Placements[i]
		if !p.Virtual && p.Alternate == alt && p.X >= left && p.X < right && (p.Y >= top || top == 0 && left == 0 && !alt) && p.Y < bottom {
			p.Y += lines
		}
	}
}

func (s *Store) Visible(alt bool) []Placement {
	var out []Placement
	for _, p := range s.Placements {
		if p.Virtual || p.Alternate == alt {
			out = append(out, p)
		}
	}
	return out
}
