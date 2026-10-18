//go:build windows

package window

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/jchv/go-webview2"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"vkvm/internal/brand"
)

var (
	user32                  = windows.NewLazySystemDLL("user32.dll")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
	procCreateIconFromRes   = user32.NewProc("CreateIconFromResourceEx")
	procSendMessage         = user32.NewProc("SendMessageW")
	procDwmSetAttribute     = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
)

// matchTitleBarTheme gives the window a dark title bar when Windows apps use
// dark mode, so the frame matches the page.
func matchTitleBarTheme(hwnd uintptr) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	light, _, err := key.GetIntegerValue("AppsUseLightTheme")
	key.Close()
	if err != nil || light != 0 {
		return
	}
	const dwmwaUseImmersiveDarkMode = 20
	on := int32(1)
	procDwmSetAttribute.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&on)), 4)
}

const (
	swRestore = 9
	wmSetIcon = 0x0080
)

// setIcon gives the window the VKVM icon, drawn from the brand PNG.
func setIcon(hwnd uintptr) {
	for kind, size := range map[uintptr]int{0: 16, 1: 32} { // ICON_SMALL, ICON_BIG
		png := brand.PNG(size, brand.App)
		icon, _, _ := procCreateIconFromRes.Call(
			uintptr(unsafe.Pointer(&png[0])), uintptr(len(png)), 1, 0x00030000,
			uintptr(size), uintptr(size), 0)
		if icon != 0 {
			procSendMessage.Call(hwnd, wmSetIcon, kind, icon)
		}
	}
}

func runNative(url string) error {
	// WebView2 keeps a profile directory; the default sits next to the
	// executable, which is read-only under Program Files.
	dataPath := ""
	if base, err := os.UserCacheDir(); err == nil {
		dataPath = filepath.Join(base, "vkvm", "webview")
	}
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  dataPath,
		WindowOptions: webview2.WindowOptions{
			Title:  Title,
			Width:  width,
			Height: height,
			Center: true,
		},
	})
	if w == nil {
		return errors.New("WebView2 runtime is not installed")
	}
	defer w.Destroy()
	w.SetSize(760, 560, webview2.HintMin)
	hwnd := uintptr(w.Window())
	setIcon(hwnd)
	matchTitleBarTheme(hwnd)
	_ = w.Bind("vkvmFocus", func() {
		w.Dispatch(func() {
			procShowWindow.Call(hwnd, swRestore)
			procSetForegroundWindow.Call(hwnd)
		})
	})
	_ = w.Bind("vkvmClose", func() {
		w.Dispatch(w.Terminate)
	})
	w.Navigate(url)
	w.Run()
	return nil
}
