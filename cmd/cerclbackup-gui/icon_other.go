//go:build !windows

package main

// trayIconBytes returns the icon in the format fyne.io/systray expects on
// this platform. On Linux/macOS the icon is decoded with Go's image.Decode,
// which has no ICO decoder registered, so it must be a format image.Decode
// understands (PNG).
func trayIconBytes() []byte {
	return renderIconPNG()
}
