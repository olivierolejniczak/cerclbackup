package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"math"
)

// iconPNG generates a 32x32 PNG icon at runtime — a blue circle with a white
// upward arrow, representing "backup to the cloud".
func iconPNG() []byte {
	const size = 32
	img := image.NewNRGBA(image.Rect(0, 0, size, size))

	bg := color.NRGBA{R: 30, G: 100, B: 200, A: 255}
	fg := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	cx, cy := float64(size/2), float64(size/2)
	radius := float64(size/2) - 1.5

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - cx + 0.5
			dy := float64(y) - cy + 0.5
			if math.Sqrt(dx*dx+dy*dy) <= radius {
				img.SetNRGBA(x, y, bg)
			}
		}
	}

	// Arrow shaft: 3px wide, from y=20 to y=13
	for y := 13; y <= 20; y++ {
		for x := 14; x <= 17; x++ {
			img.SetNRGBA(x, y, fg)
		}
	}
	// Arrow head: rows 9..12, widening toward the tip at y=9
	for row := 0; row < 5; row++ {
		y := 12 - row
		half := row + 1
		for x := 15 - half + 1; x <= 16+half-1; x++ {
			img.SetNRGBA(x, y, fg)
		}
	}

	var buf bytes.Buffer
	png.Encode(&buf, img)
	return buf.Bytes()
}
