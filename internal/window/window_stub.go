//go:build !windows && !darwin

package window

import "errors"

func runNative(string) error {
	return errors.New("no native web view on this platform")
}
