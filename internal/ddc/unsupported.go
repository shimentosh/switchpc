//go:build !darwin && !windows

package ddc

func newMacController() (Controller, error) {
	return nil, ErrUnsupportedPlatform
}

func newWindowsController() (Controller, error) {
	return nil, ErrUnsupportedPlatform
}
