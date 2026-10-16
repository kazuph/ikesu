// tb_file_terminal is a local terminal emulator for file-frame lifetime probes.
// It replies only after the Kitty store has actually read and validated pixels.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
	"github.com/creack/pty"
	"github.com/kazuph/goterrace/internal/kitty"
)

func run() error {
	out := flag.String("out", "", "owned output directory")
	flag.Parse()
	if *out == "" || len(flag.Args()) == 0 {
		return fmt.Errorf("usage: tb_file_terminal -out DIR -- command args...")
	}
	if err := os.MkdirAll(*out, 0700); err != nil {
		return err
	}
	log, err := os.Create(filepath.Join(*out, "graphics.jsonl"))
	if err != nil {
		return err
	}
	defer log.Close()
	raw, err := os.Create(filepath.Join(*out, "terminal.raw"))
	if err != nil {
		return err
	}
	defer raw.Close()
	cmd := exec.Command(flag.Args()[0], flag.Args()[1:]...)
	cmd.Env = os.Environ()
	terminal, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 160, Rows: 45, X: 1280, Y: 720})
	if err != nil {
		return err
	}
	defer terminal.Close()
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(interrupt)
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-interrupt:
			_ = cmd.Process.Signal(syscall.SIGTERM)
		case <-finished:
		}
	}()
	emulator := vt.NewEmulator(160, 45)
	defer emulator.Close()
	go func() { _, _ = io.Copy(terminal, emulator) }()
	emulator.RegisterCsiHandler('t', func(params ansi.Params) bool {
		op, _, _ := params.Param(0, 0)
		switch op {
		case 16:
			fmt.Fprint(terminal, "\x1b[6;16;8t")
		case 14:
			fmt.Fprint(terminal, "\x1b[4;720;1280t")
		default:
			return false
		}
		return true
	})
	var images kitty.Store
	encoder := json.NewEncoder(log)
	emulator.RegisterApcHandler(func(data []byte) bool {
		position := emulator.CursorPosition()
		reply, _, _, handled := images.Handle(data, position.X, position.Y, 8, 16, false)
		if !handled {
			return false
		}
		header, _, _ := strings.Cut(string(data), ";")
		hashes := map[uint32]string{}
		for id, image := range images.Images {
			pixels := image.Data
			if image.Format == 100 {
				decoded, err := png.Decode(bytes.NewReader(pixels))
				if err != nil {
					continue
				}
				pixels = make([]byte, 0, image.Width*image.Height*4)
				for y := 0; y < image.Height; y++ {
					for x := 0; x < image.Width; x++ {
						c := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
						pixels = append(pixels, c.R, c.G, c.B, c.A)
					}
				}
			}
			hashes[id] = fmt.Sprintf("%x", sha256.Sum256(pixels))
		}
		_ = encoder.Encode(map[string]any{"control": header, "reply": reply, "hashes": hashes, "placements": len(images.Placements)})
		if reply != "" {
			_, _ = io.WriteString(terminal, reply)
		}
		return true
	})
	buffer := make([]byte, 65536)
	for {
		n, err := terminal.Read(buffer)
		if n > 0 {
			_, _ = raw.Write(buffer[:n])
			_, _ = emulator.Write(buffer[:n])
			var screen strings.Builder
			for y := 0; y < 45; y++ {
				for x := 0; x < 160; x++ {
					cell := emulator.CellAt(x, y)
					if cell != nil && cell.Content != "" {
						screen.WriteString(cell.Content)
					} else {
						screen.WriteByte(' ')
					}
				}
				screen.WriteByte('\n')
			}
			_ = os.WriteFile(filepath.Join(*out, "screen.txt"), []byte(screen.String()), 0600)
		}
		if err != nil {
			break
		}
	}
	return cmd.Wait()
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
