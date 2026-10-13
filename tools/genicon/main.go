// Command genicon draws the Cmdex icon (a terminal window with a ">_"
// prompt) and writes it as PNG, or as a Windows .ico holding 256, 48, 32
// and 16 px images:
//
//	go run ./tools/genicon build/appicon.png installer/app.ico
package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"strings"
)

const ss = 4 // supersampling for smooth edges

var size = 512

var (
	bg     = color.RGBA{0x14, 0x1A, 0x21, 0xFF}
	accent = color.RGBA{0xF2, 0xA5, 0x41, 0xFF}
	light  = color.RGBA{0xE6, 0xEA, 0xF0, 0xFF}
)

// inRoundRect reports whether (x, y) is inside a rounded rectangle.
func inRoundRect(x, y, x0, y0, x1, y1, r float64) bool {
	if x < x0 || x > x1 || y < y0 || y > y1 {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
}

// nearSegment reports whether (x, y) is within w/2 of the segment a-b
// (round caps).
func nearSegment(x, y, ax, ay, bx, by, w float64) bool {
	dx, dy := bx-ax, by-ay
	t := ((x-ax)*dx + (y-ay)*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	px, py := ax+t*dx, ay+t*dy
	return (x-px)*(x-px)+(y-py)*(y-py) <= w*w/4
}

func pixel(x, y float64) color.RGBA {
	s := float64(size)
	// window body with an accent border
	if !inRoundRect(x, y, 0.06*s, 0.12*s, 0.94*s, 0.88*s, 0.12*s) {
		return color.RGBA{}
	}
	if !inRoundRect(x, y, 0.105*s, 0.165*s, 0.895*s, 0.835*s, 0.08*s) {
		return accent
	}
	// ">" chevron and "_" cursor
	w := 0.075 * s
	if nearSegment(x, y, 0.27*s, 0.37*s, 0.43*s, 0.50*s, w) || nearSegment(x, y, 0.43*s, 0.50*s, 0.27*s, 0.63*s, w) {
		return accent
	}
	if nearSegment(x, y, 0.52*s, 0.64*s, 0.72*s, 0.64*s, w) {
		return light
	}
	return bg
}

func render(px int) *image.RGBA {
	size = px
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					c := pixel(float64(x)+(float64(sx)+0.5)/ss, float64(y)+(float64(sy)+0.5)/ss)
					af := float64(c.A) / 255
					r += float64(c.R) * af
					g += float64(c.G) * af
					b += float64(c.B) * af
					a += af
				}
			}
			if a == 0 {
				continue
			}
			img.SetRGBA(x, y, color.RGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(255 * a / float64(ss*ss))})
		}
	}
	return img
}

func pngBytes(img image.Image) []byte {
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		log.Fatal(err)
	}
	return b.Bytes()
}

// ico packs PNG images into an .ico file (PNG entries work on Vista and later).
func ico(sizes []int) []byte {
	var imgs [][]byte
	for _, s := range sizes {
		imgs = append(imgs, pngBytes(render(s)))
	}
	var b bytes.Buffer
	binary.Write(&b, binary.LittleEndian, [3]uint16{0, 1, uint16(len(imgs))})
	off := 6 + 16*len(imgs)
	for i, s := range sizes {
		w := uint8(s)
		if s >= 256 {
			w = 0
		}
		b.Write([]byte{w, w, 0, 0})
		binary.Write(&b, binary.LittleEndian, []uint16{1, 32})
		binary.Write(&b, binary.LittleEndian, []uint32{uint32(len(imgs[i])), uint32(off)})
		off += len(imgs[i])
	}
	for _, im := range imgs {
		b.Write(im)
	}
	return b.Bytes()
}

func main() {
	if len(os.Args) < 2 {
		log.Fatal("usage: genicon out.png [out.ico]")
	}
	for _, out := range os.Args[1:] {
		var data []byte
		if strings.HasSuffix(strings.ToLower(out), ".ico") {
			data = ico([]int{256, 48, 32, 16})
		} else {
			data = pngBytes(render(512))
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
}
