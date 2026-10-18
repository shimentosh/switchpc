//go:build darwin

package window

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>

static void vkvmPrepareApp(void) {
	[NSApplication sharedApplication];
	// VKVM.app is an LSUIElement (menu bar) app; the settings window process
	// needs a regular activation policy to own a focusable window and Dock tile.
	[NSApp setActivationPolicy:NSApplicationActivationPolicyRegular];
}

static void vkvmFocusWindow(void *window) {
	[NSApp activateIgnoringOtherApps:YES];
	if (window != NULL) {
		NSWindow *w = (__bridge NSWindow *)window;
		[w makeKeyAndOrderFront:nil];
	}
}
*/
import "C"

import (
	webview "github.com/webview/webview_go"
)

func runNative(url string) error {
	C.vkvmPrepareApp()
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle(Title)
	w.SetSize(width, height, webview.HintNone)
	w.SetSize(760, 560, webview.HintMin)
	win := w.Window()
	_ = w.Bind("vkvmFocus", func() {
		w.Dispatch(func() { C.vkvmFocusWindow(win) })
	})
	_ = w.Bind("vkvmStartApp", startApp)
	_ = w.Bind("vkvmClose", func() {
		w.Dispatch(w.Terminate)
	})
	w.Navigate(url)
	w.Dispatch(func() { C.vkvmFocusWindow(win) })
	w.Run()
	return nil
}
