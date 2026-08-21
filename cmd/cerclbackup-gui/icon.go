package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
)

// iconICO returns a 32x32 ICO file containing a classic BMP-based (DIB)
// image: a BITMAPINFOHEADER followed by 32bpp BGRA color data and a 1bpp AND
// mask. fyne.io/systray on Windows loads the icon via CreateIconFromResourceEx
// on the notify-icon path, which — unlike Explorer's file-icon loader — does
// not reliably parse the "PNG-in-ICO" container (a directory entry pointing
// straight at PNG-encoded bytes); it fails silently, so SetIcon becomes a
// no-op and no tray icon (and no way to reach the app) ever appears. The
// BMP-based format below is understood by every Windows icon loader.
func iconICO() []byte {
	img := renderIcon()
	const size = 32

	colorData := make([]byte, size*size*4)
	for y := 0; y < size; y++ {
		// DIB rows are stored bottom-up.
		srcY := size - 1 - y
		for x := 0; x < size; x++ {
			c := img.NRGBAAt(x, srcY)
			i := (y*size + x) * 4
			colorData[i+0] = c.B
			colorData[i+1] = c.G
			colorData[i+2] = c.R
			colorData[i+3] = c.A
		}
	}

	maskRowBytes := ((size + 31) / 32) * 4
	andMask := make([]byte, maskRowBytes*size) // all zero: fully opaque, alpha channel drives transparency

	header := make([]byte, 40)
	binary.LittleEndian.PutUint32(header[0:], 40)                                   // biSize
	binary.LittleEndian.PutUint32(header[4:], size)                                 // biWidth
	binary.LittleEndian.PutUint32(header[8:], size*2)                               // biHeight (XOR+AND)
	binary.LittleEndian.PutUint16(header[12:], 1)                                   // biPlanes
	binary.LittleEndian.PutUint16(header[14:], 32)                                  // biBitCount
	binary.LittleEndian.PutUint32(header[20:], uint32(len(colorData)+len(andMask))) // biSizeImage

	dib := append(header, colorData...)
	dib = append(dib, andMask...)

	// ICO header: 6 bytes
	// ICONDIR: reserved(2) + type=1(2) + count=1(2)
	var buf bytes.Buffer
	buf.Write([]byte{0x00, 0x00, 0x01, 0x00, 0x01, 0x00})

	// ICONDIRENTRY: 16 bytes
	// width(1) height(1) colorCount(1) reserved(1) planes(2) bitCount(2) bytesInRes(4) imageOffset(4)
	entry := make([]byte, 16)
	entry[0] = size                                            // width
	entry[1] = size                                            // height
	entry[2] = 0                                               // colorCount (0 = >256)
	entry[3] = 0                                               // reserved
	binary.LittleEndian.PutUint16(entry[4:], 1)                // planes
	binary.LittleEndian.PutUint16(entry[6:], 32)               // bit count
	binary.LittleEndian.PutUint32(entry[8:], uint32(len(dib))) // image size
	binary.LittleEndian.PutUint32(entry[12:], 6+16)            // offset = header + one entry
	buf.Write(entry)

	buf.Write(dib)
	return buf.Bytes()
}

func renderIconPNG() []byte {
	var buf bytes.Buffer
	png.Encode(&buf, renderIcon())
	return buf.Bytes()
}

func renderIcon() *image.NRGBA {
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

	return img
}
