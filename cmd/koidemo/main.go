// SPIKE (feat/koi-branding): draws the high-resolution koi pond into the
// current terminal with the kitty graphics protocol, to check that real
// terminals (and ikesu panes) show it moving. Not shipped.
//
// -mode full:   re-encode and send the whole pond every frame (first try).
// -mode sprite: send water and every koi frame once, then only move
//
//	placements each tick.
package main

import (
	"bytes"
	"encoding/base64"
	"flag"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"time"

	"github.com/kazuph/ikesu/internal/render"
	"golang.org/x/sys/unix"
)

const wagFrames = 8

func sendImage(out *bytes.Buffer, id int, img image.Image) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	data := base64.StdEncoding.EncodeToString(buf.Bytes())
	for off := 0; off < len(data); off += 4096 {
		end := min(off+4096, len(data))
		more := 0
		if end < len(data) {
			more = 1
		}
		if off == 0 {
			fmt.Fprintf(out, "\x1b_Ga=t,f=100,i=%d,q=2,m=%d;%s\x1b\\", id, more, data[off:end])
		} else {
			fmt.Fprintf(out, "\x1b_Gm=%d;%s\x1b\\", more, data[off:end])
		}
	}
}

func main() {
	cols := flag.Int("cols", 40, "pond width in cells")
	rows := flag.Int("rows", 10, "pond height in cells")
	koi := flag.Int("koi", 4, "number of koi")
	secs := flag.Float64("secs", 20, "run time")
	fps := flag.Float64("fps", 10, "frames per second")
	mode := flag.String("mode", "sprite", "full or sprite")
	flag.Parse()

	cw, ch := 8, 16
	if ws, err := unix.IoctlGetWinsize(1, unix.TIOCGWINSZ); err == nil && ws.Xpixel > 0 && ws.Col > 0 {
		cw, ch = int(ws.Xpixel)/int(ws.Col), int(ws.Ypixel)/int(ws.Row)
	}
	w, h := *cols*cw, *rows*ch
	length := float64(w) / 3.2
	fmt.Printf("mode=%s cell %dx%d px, pond %dx%d px\n", *mode, cw, ch, w, h)
	for i := 0; i < *rows; i++ {
		fmt.Println()
	}
	fmt.Printf("\x1b[%dA\x1b7", *rows)

	var sent int
	write := func(b *bytes.Buffer) { sent += b.Len(); os.Stdout.Write(b.Bytes()); b.Reset() }
	var out bytes.Buffer

	// Sprite frames: koi k, direction d (0 right, 1 left), wag f.
	spriteW, spriteH := int(length*1.05), int(length*0.45)
	frameID := func(k, d, f int) int { return 100 + (k*2+d)*wagFrames + f }
	if *mode == "sprite" {
		water := render.KoiPondImage(w, h, 0, 0, length)
		sendImage(&out, 10, water)
		fmt.Fprintf(&out, "\x1b8\x1b_Ga=p,i=10,p=1,z=1,C=1,q=2\x1b\\")
		for k := 0; k < *koi; k++ {
			for d := 0; d < 2; d++ {
				for f := 0; f < wagFrames; f++ {
					img := image.NewRGBA(image.Rect(0, 0, spriteW, spriteH))
					heading := 0.0
					if d == 1 {
						heading = math.Pi
					}
					render.DrawKoiAlpha(img, render.KoiPatterns[k%len(render.KoiPatterns)], float64(spriteW)/2, float64(spriteH)/2, length, heading, math.Sin(float64(f)/wagFrames*2*math.Pi))
					sendImage(&out, frameID(k, d, f), img)
				}
			}
		}
		write(&out)
		fmt.Printf("upload bytes=%d\n", sent)
	}
	setup := sent
	start := time.Now()
	frames := 0
	shown := make([]int, *koi)
	lanes := max(1, *koi)
	laneH := float64(h) / float64(lanes)
	for time.Since(start).Seconds() < *secs {
		t := time.Since(start).Seconds()
		if *mode == "full" {
			img := render.KoiPondImage(w, h, *koi, t, length)
			sendImage(&out, 7, img)
			out.WriteString("\x1b8\x1b_Ga=p,i=7,p=1,C=1,q=2\x1b\\")
		} else {
			for k := 0; k < *koi; k++ {
				speed := 0.18 + hash(k+11)*0.12
				phase := hash(k+29) * math.Pi * 2
				u := t*speed + phase
				margin := length * 0.55
				cx := margin + (math.Sin(u)*0.5+0.5)*(float64(w)-2*margin)
				cy := laneH*(float64(k)+0.5) + math.Sin(u*0.9+phase)*laneH*0.18
				d := 0
				if math.Cos(u) < 0 {
					d = 1
				}
				f := int(math.Mod(t*6+phase*3, 2*math.Pi)/(2*math.Pi)*wagFrames) % wagFrames
				id := frameID(k, d, f)
				px, py := int(cx)-spriteW/2, int(cy)-spriteH/2
				px, py = max(0, px), max(0, py)
				if shown[k] != 0 && shown[k] != id {
					fmt.Fprintf(&out, "\x1b_Ga=d,d=i,i=%d,p=%d,q=2\x1b\\", shown[k], k+2)
				}
				// Move to the target cell, then shift by the pixel remainder.
				fmt.Fprintf(&out, "\x1b8")
				if py/ch > 0 {
					fmt.Fprintf(&out, "\x1b[%dB", py/ch)
				}
				if px/cw > 0 {
					fmt.Fprintf(&out, "\x1b[%dC", px/cw)
				}
				fmt.Fprintf(&out, "\x1b_Ga=p,i=%d,p=%d,X=%d,Y=%d,z=2,C=1,q=2\x1b\\", id, k+2, px%cw, py%ch)
				shown[k] = id
			}
		}
		write(&out)
		frames++
		time.Sleep(time.Duration(float64(time.Second) / *fps))
	}
	el := time.Since(start).Seconds()
	fmt.Printf("\x1b8\x1b[%dB\nframes=%d fps=%.1f setup_bytes=%d run_bytes/s=%.0f\n", *rows, frames, float64(frames)/el, setup, float64(sent-setup)/el)
}

func hash(i int) float64 {
	x := math.Sin(float64(i)*12.9898+78.233) * 43758.5453
	return x - math.Floor(x)
}
