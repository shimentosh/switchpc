// Package brand draws the VKVM icon (a monitor with a swap arrow) in code so
// the tray, the window and the installers share one mark without checked-in
// binary assets.
package brand

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

var (
	accentTop    = color.NRGBA{0x4f, 0x6b, 0xff, 0xff}
	accentBottom = color.NRGBA{0x7b, 0x3f, 0xe4, 0xff}
)

// Variant selects how the icon is drawn.
type Variant int

const (
	// App is the full-colour tile used for the app, installer and tray on Windows.
	App Variant = iota
	// Template is a black glyph on transparency for the macOS menu bar, which
	// recolours it for light and dark mode.
	Template
)

// PNG renders the icon at size×size pixels.
func PNG(size int, v Variant) []byte {
	var buf bytes.Buffer
	_ = png.Encode(&buf, Render(size, v))
	return buf.Bytes()
}

// Render draws the icon with 4×4 supersampling for smooth edges.
func Render(size int, v Variant) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const ss = 4
	// Small tray-sized renders enlarge the glyph so it stays legible.
	zoom := 1.0
	if size <= 32 {
		zoom = 1.25
	}
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var bgHits, glyphHits int
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) / float64(size)
					y := (float64(py) + (float64(sy)+0.5)/ss) / float64(size)
					if v == App && roundedRect(x, y, 0.02, 0.02, 0.98, 0.98, 0.22) {
						bgHits++
					}
					if glyph(0.5+(x-0.5)/zoom, 0.5+(y-0.5)/zoom) {
						glyphHits++
					}
				}
			}
			n := float64(ss * ss)
			bgA := float64(bgHits) / n
			glA := float64(glyphHits) / n
			var c color.NRGBA
			if v == Template {
				c = color.NRGBA{0, 0, 0, uint8(math.Round(glA * 255))}
			} else {
				t := float64(py) / float64(size)
				bg := lerp(accentTop, accentBottom, t)
				// white glyph over the gradient tile
				r := float64(bg.R)*(1-glA) + 255*glA
				g := float64(bg.G)*(1-glA) + 255*glA
				b := float64(bg.B)*(1-glA) + 255*glA
				a := math.Max(bgA, glA)
				c = color.NRGBA{uint8(r), uint8(g), uint8(b), uint8(math.Round(a * 255))}
			}
			img.SetNRGBA(px, py, c)
		}
	}
	return img
}

// ICO packs PNG renders of the given sizes into a Windows .ico file.
func ICO(v Variant, sizes ...int) []byte {
	images := make([][]byte, len(sizes))
	for i, s := range sizes {
		images[i] = PNG(s, v)
	}
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		dim := uint8(s)
		if s >= 256 {
			dim = 0
		}
		entry := struct {
			W, H, Colors, Reserved uint8
			Planes, BPP            uint16
			Size, Offset           uint32
		}{dim, dim, 0, 0, 1, 32, uint32(len(images[i])), uint32(offset)}
		_ = binary.Write(&buf, binary.LittleEndian, entry)
		offset += len(images[i])
	}
	for _, img := range images {
		buf.Write(img)
	}
	return buf.Bytes()
}

func glyph(x, y float64) bool {
	// Monitor bezel: outline of a rounded rectangle.
	outer := roundedRect(x, y, 0.17, 0.21, 0.83, 0.65, 0.07)
	inner := roundedRect(x, y, 0.235, 0.275, 0.765, 0.585, 0.03)
	if outer && !inner {
		return true
	}
	// Stand.
	if rect(x, y, 0.465, 0.65, 0.535, 0.74) || roundedRect(x, y, 0.33, 0.73, 0.67, 0.79, 0.03) {
		return true
	}
	// Swap arrows inside the screen: → on top, ← below.
	if rect(x, y, 0.33, 0.365, 0.58, 0.405) || triangle(x, y, 0.57, 0.33, 0.57, 0.44, 0.67, 0.385) {
		return true
	}
	if rect(x, y, 0.42, 0.455, 0.67, 0.495) || triangle(x, y, 0.43, 0.42, 0.43, 0.53, 0.33, 0.475) {
		return true
	}
	return false
}

func rect(x, y, x0, y0, x1, y1 float64) bool { return x >= x0 && x <= x1 && y >= y0 && y <= y1 }

func roundedRect(x, y, x0, y0, x1, y1, r float64) bool {
	if !rect(x, y, x0, y0, x1, y1) {
		return false
	}
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	return (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r
}

func triangle(x, y, ax, ay, bx, by, cx, cy float64) bool {
	d1 := (x-bx)*(ay-by) - (ax-bx)*(y-by)
	d2 := (x-cx)*(by-cy) - (bx-cx)*(y-cy)
	d3 := (x-ax)*(cy-ay) - (cx-ax)*(y-ay)
	neg := d1 < 0 || d2 < 0 || d3 < 0
	pos := d1 > 0 || d2 > 0 || d3 > 0
	return !(neg && pos)
}

func lerp(a, b color.NRGBA, t float64) color.NRGBA {
	f := func(p, q uint8) uint8 { return uint8(float64(p) + (float64(q)-float64(p))*t) }
	return color.NRGBA{f(a.R, b.R), f(a.G, b.G), f(a.B, b.B), 0xff}
}
