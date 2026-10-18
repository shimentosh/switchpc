//go:build darwin

package input

/*
#cgo LDFLAGS: -framework CoreGraphics -framework ApplicationServices -framework CoreFoundation
#include <CoreGraphics/CoreGraphics.h>
#include <ApplicationServices/ApplicationServices.h>

static void vkvmPromptAccessibility(void) {
	const void *keys[] = { kAXTrustedCheckOptionPrompt };
	const void *values[] = { kCFBooleanTrue };
	CFDictionaryRef options = CFDictionaryCreate(kCFAllocatorDefault, keys, values, 1,
		&kCFCopyStringDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks);
	AXIsProcessTrustedWithOptions(options);
	CFRelease(options);
}
*/
import "C"

import "os/exec"

// System Settings deep links for the privacy panes VKVM needs.
const (
	accessibilityPane   = "x-apple.systempreferences:com.apple.preference.security?Privacy_Accessibility"
	inputMonitoringPane = "x-apple.systempreferences:com.apple.preference.security?Privacy_ListenEvent"
)

// RequestPermission asks macOS for a permission ("accessibility" or
// "input_monitoring"). macOS shows its own prompt the first time and adds
// VKVM to the list; the matching System Settings pane is opened as well so
// the user can switch it on.
func RequestPermission(kind string) {
	switch kind {
	case "accessibility":
		C.vkvmPromptAccessibility()
		_ = exec.Command("open", accessibilityPane).Start()
	case "input_monitoring":
		C.CGRequestListenEventAccess()
		_ = exec.Command("open", inputMonitoringPane).Start()
	}
}
