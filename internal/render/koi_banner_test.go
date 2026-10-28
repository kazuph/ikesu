package render

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// Artwork coordinates are a 480×120 dot grid, enlarged exactly twice.
// Three phase-separated paths keep the actual terminal sprites inside the pond.
const bannerWidth, bannerHeight, bannerScale, bannerFrames = 480, 120, 2, 160

var bannerPaper = color.RGBA{244, 240, 230, 255}
var bannerInk = color.RGBA{23, 43, 42, 255}
var bannerRed = color.RGBA{141, 41, 31, 255}

// A small serif wordmark: stems, slab serifs and a vermilion full stop echo
// the site's Georgia wordmark, on the same fixed dot grid as the koi.
var bannerLogo = []string{
	"00100/00000/01100/00100/00100/00100/00100/00100/01110",
	"11000/01000/01000/01010/01100/01100/01010/01010/11011",
	"00000/00000/01110/11011/10001/11111/10000/11000/01111",
	"00000/00000/01111/11000/11000/01110/00011/00011/11110",
	"00000/00000/11011/01001/01001/01001/01001/01011/00101",
}

// Fixed 5×7 lettering avoids machine-dependent fonts and antialiasing.
var bannerLetters = map[rune]string{
	'R': "11110/10001/10001/11110/10100/10010/10001", 'a': "00000/00000/01110/00001/01111/10001/01111",
	'i': "00100/00000/01100/00100/00100/00100/01110", 's': "00000/00000/01111/10000/01110/00001/11110",
	'e': "00000/00000/01110/10001/11111/10000/01111", 'y': "00000/00000/10001/10001/01111/00001/01110",
	'o': "00000/00000/01110/10001/10001/10001/01110", 'u': "00000/00000/10001/10001/10001/10011/01101",
	'r': "00000/00000/10110/11001/10000/10000/10000", 'g': "00000/00000/01111/10001/01111/00001/01110",
	'n': "00000/00000/10110/11001/10001/10001/10001", 't': "01000/01000/11110/01000/01000/01001/00110",
	'p': "00000/00000/11110/10001/11110/10000/10000", 'd': "00001/00001/01101/10011/10001/10001/01111",
	'.': "0/0/0/0/0/1/1", ' ': "00/00/00/00/00/00/00",
}

func bannerGlyph(img *image.RGBA, glyph string, x, y, scale int, ink color.RGBA) int {
	col, row, width := 0, 0, 0
	for _, dot := range glyph {
		if dot == '/' {
			width = max(width, col)
			col = 0
			row++
			continue
		}
		if dot == '1' {
			draw.Draw(img, image.Rect(x+col*scale, y+row*scale, x+(col+1)*scale, y+(row+1)*scale), image.NewUniform(ink), image.Point{}, draw.Src)
		}
		col++
	}
	return max(width, col) * scale
}

type bannerPose struct {
	at             image.Point
	direction, wag int
}

func bannerFish(tick, fish int) bannerPose {
	phase := 2 * math.Pi * (float64(tick)/bannerFrames + float64(fish)/3)
	direction := 0
	if math.Sin(phase) > 0 {
		direction = 1
	}
	return bannerPose{image.Pt(int(math.Round(354+75*math.Cos(phase)))-20, int(math.Round(60+38*math.Sin(phase)))-8), direction, (tick/3 + fish*2) % 8}
}

func koiBannerFrame(tick int, english bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, bannerWidth, bannerHeight))
	draw.Draw(img, img.Bounds(), image.NewUniform(bannerPaper), image.Point{}, draw.Src)
	// Quiet pond stippling, kept outside the wordmark's reading surface.
	water := color.RGBA{221, 228, 211, 255}
	for y := 5; y < bannerHeight; y += 9 {
		for x := 253 + (y%2)*4; x < bannerWidth-5; x += 13 {
			img.SetRGBA(x, y, water)
		}
	}
	x := 24
	for _, glyph := range bannerLogo {
		x += bannerGlyph(img, glyph, x, 23, 6, bannerInk) + 6
	}
	draw.Draw(img, image.Rect(x+2, 71, x+8, 77), image.NewUniform(bannerRed), image.Point{}, draw.Src)
	if english {
		x = 25
		for _, letter := range "Raise your agents in a pond." {
			x += bannerGlyph(img, bannerLetters[letter], x, 93, 1, bannerInk) + 1
		}
	}
	for fish := 0; fish < 3; fish++ {
		pose := bannerFish(tick, fish)
		sprite := KoiPixelSprite(fish, pose.direction, pose.wag, 40, 16)
		draw.Draw(img, image.Rectangle{Min: pose.at, Max: pose.at.Add(sprite.Bounds().Size())}, sprite, image.Point{}, draw.Over)
	}
	enlarged := image.NewRGBA(image.Rect(0, 0, bannerWidth*bannerScale, bannerHeight*bannerScale))
	for y := 0; y < enlarged.Bounds().Dy(); y++ {
		for x := 0; x < enlarged.Bounds().Dx(); x++ {
			enlarged.SetRGBA(x, y, img.RGBAAt(x/bannerScale, y/bannerScale))
		}
	}
	return enlarged
}

func exportKoiBanners(t *testing.T, dest string) {
	t.Helper()
	for _, lang := range []string{"en", "ja"} {
		animation := &gif.GIF{LoopCount: 0}
		for tick := 0; tick < bannerFrames; tick++ {
			img := koiBannerFrame(tick, lang == "en")
			palette := color.Palette{}
			seen := map[color.RGBA]bool{}
			for y := 0; y < img.Bounds().Dy(); y++ {
				for x := 0; x < img.Bounds().Dx(); x++ {
					c := img.RGBAAt(x, y)
					if !seen[c] {
						palette = append(palette, c)
						seen[c] = true
					}
				}
			}
			if len(palette) > 256 {
				t.Fatal("banner palette overflow", len(palette))
			}
			frame := image.NewPaletted(img.Bounds(), palette)
			draw.Draw(frame, frame.Bounds(), img, image.Point{}, draw.Src)
			animation.Image = append(animation.Image, frame)
			animation.Delay = append(animation.Delay, 5)
		}
		var data bytes.Buffer
		if err := gif.EncodeAll(&data, animation); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dest, "ikesu-banner-"+lang+".gif"), data.Bytes(), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKoiBannersMatchRendererAndStayInsidePond(t *testing.T) {
	for _, lang := range []string{"en", "ja"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "docs", "brand", "ikesu-banner-"+lang+".gif"))
		if err != nil {
			t.Fatal(err)
		}
		if len(data) > 2_000_000 {
			t.Fatal("README banner exceeds requested 2MB", lang, len(data))
		}
		animation, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if len(animation.Image) != bannerFrames || animation.LoopCount != 0 {
			t.Fatal("banner must loop through the complete swim", lang)
		}
		ys := [3][2]int{{bannerHeight, 0}, {bannerHeight, 0}, {bannerHeight, 0}}
		for tick, frame := range animation.Image {
			expected := koiBannerFrame(tick, lang == "en")
			if frame.Bounds() != expected.Bounds() || animation.Delay[tick] != 5 {
				t.Fatal("frame geometry or timing differs", lang, tick)
			}
			for fish := 0; fish < 3; fish++ {
				p := bannerFish(tick, fish)
				r := image.Rectangle{Min: p.at, Max: p.at.Add(image.Pt(40, 16))}
				if !r.In(image.Rect(253, 0, bannerWidth, bannerHeight)) {
					t.Fatal("clipped koi or wordmark collision", tick, fish, r)
				}
				ys[fish][0] = min(ys[fish][0], p.at.Y)
				ys[fish][1] = max(ys[fish][1], p.at.Y)
				for other := 0; other < fish; other++ {
					q := bannerFish(tick, other)
					if r.Overlaps(image.Rectangle{Min: q.at, Max: q.at.Add(image.Pt(40, 16))}) {
						t.Fatal("koi overlap", tick, fish, other)
					}
				}
			}
			for y := 0; y < frame.Bounds().Dy(); y++ {
				for x := 0; x < frame.Bounds().Dx(); x++ {
					if color.RGBAModel.Convert(frame.At(x, y)) != expected.RGBAAt(x, y) {
						t.Fatal("GIF differs from actual renderer or dot grid", lang, tick, x, y)
					}
				}
			}
			if dest := os.Getenv("KOI_BANNER_PROOF"); dest != "" && (tick == 0 || tick == bannerFrames/2) {
				if err := os.MkdirAll(dest, 0755); err != nil {
					t.Fatal(err)
				}
				name := "first"
				if tick != 0 {
					name = "middle"
				}
				f, err := os.Create(filepath.Join(dest, "banner-"+lang+"-"+name+".png"))
				if err != nil {
					t.Fatal(err)
				}
				err = png.Encode(f, frame)
				f.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		for fish, span := range ys {
			if span[0] >= bannerHeight/2 || span[1] <= bannerHeight/2 {
				t.Fatal("koi stays in upper or lower half", fish, span)
			}
		}
		t.Logf("%s: %d bytes, %dx%d, %d frames, 8 second loop; all pixels equal renderer; koi top-left y spans %v", lang, len(data), bannerWidth*bannerScale, bannerHeight*bannerScale, bannerFrames, ys)
	}
}
