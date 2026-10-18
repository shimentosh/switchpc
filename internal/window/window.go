// Package window shows the VKVM interface in a native app window.
//
// The window runs in its own short-lived process (`vkvm -window <url>`). The
// tray and the native web view both want to own the process main thread and
// event loop (AppKit on macOS), so keeping them in separate processes avoids
// fighting over it. The window only renders the loopback UI served by the core.
package window

import (
	"log"
	"os/exec"
	"runtime"
)

const (
	// Title is the native window title.
	Title  = "VKVM"
	width  = 980
	height = 720
)

// Run opens a native window showing url and blocks until it is closed. When
// no native web view is available it falls back to the default browser.
func Run(url string) {
	runtime.LockOSThread()
	if err := runNative(url); err != nil {
		log.Printf("Native window unavailable (%v); opening the browser instead", err)
		OpenBrowser(url)
	}
}

// OpenBrowser opens url in the system default browser.
func OpenBrowser(url string) {
	var err error
	switch runtime.GOOS {
	case "darwin":
		err = exec.Command("open", url).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		err = exec.Command("xdg-open", url).Start()
	}
	if err != nil {
		log.Printf("Failed to open browser: %v", err)
	}
}
