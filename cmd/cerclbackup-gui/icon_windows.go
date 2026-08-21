package main

// trayIconBytes returns the icon in the format fyne.io/systray expects on
// this platform. On Windows the notify-icon path loads via
// CreateIconFromResourceEx, which requires a real ICO container.
func trayIconBytes() []byte {
	return iconICO()
}
