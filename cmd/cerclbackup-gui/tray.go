package main

import (
	"fyne.io/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// startTray runs the system-tray icon in the background. It replaces
// cmd/cerclbackup-tray: this single GUI binary now owns both the window
// and the tray icon, so closing the window just hides it instead of
// spawning/monitoring a separate process.
func (a *App) startTray() {
	go systray.Run(a.onTrayReady, func() {})
}

func (a *App) onTrayReady() {
	systray.SetIcon(iconICO())
	systray.SetTitle("CerclBackup")
	systray.SetTooltip("CerclBackup — P2P encrypted backup")

	mShow := systray.AddMenuItem("Show window", "Bring the CerclBackup window to front")
	mBackupNow := systray.AddMenuItem("Backup now", "Run an immediate backup")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop CerclBackup")

	go func() {
		for {
			select {
			case <-mShow.ClickedCh:
				runtime.WindowShow(a.ctx)
			case <-mBackupNow.ClickedCh:
				a.emit("tray:backup-now")
			case <-mQuit.ClickedCh:
				systray.Quit()
				runtime.Quit(a.ctx)
				return
			}
		}
	}()
}
