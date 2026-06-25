package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// iconICO returns a 32x32 ICO file wrapping a PNG image.
// fyne.io/systray on Windows requires ICO format, not raw PNG.
// We use the "PNG-in-ICO" format (Windows Vista+): a valid ICO directory
// whose single entry points directly to compressed PNG data.
func iconICO() []byte {
	pngData := renderIconPNG()

	// ICO header: 6 bytes
	// ICONDIR: reserved(2) + type=1(2) + count=1(2)
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00})

	// ICONDIRENTRY: 16 bytes
	// width(1) height(1) colorCount(1) reserved(1) planes(2) bitCount(2) bytesInRes(4) imageOffset(4)
	entry := make([]byte, 16)
	entry[0] = 32  // width
	entry[1] = 32  // height
	entry[2] = 0   // colorCount (0 = >256)
	entry[3] = 0   // reserved
	binary.LittleEndian.PutUint16(entry[4:], 1)                      // planes
	binary.LittleEndian.PutUint16(entry[6:], 32)                     // bit count
	binary.LittleEndian.PutUint32(entry[8:], uint32(len(pngData)))   // image size
	binary.LittleEndian.PutUint32(entry[12:], 6+16)                  // offset = header + one entry
	buf.Write(entry)

	buf.Write(pngData)
	return buf.Bytes()
}

func renderIconPNG() []byte {
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

	// Arrow shaft
	for y := 13; y <= 20; y++ {
		for x := 14; x <= 17; x++ {
			img.SetNRGBA(x, y, fg)
		}
	}
	// Arrow head
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
