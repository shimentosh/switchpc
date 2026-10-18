//go:build !darwin

package input

// RequestPermission is a no-op on platforms without per-app input permissions.
func RequestPermission(string) {}
