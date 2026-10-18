// Command icon writes the VKVM icon files used by the installers:
//
//	go run ./packaging/icon <out-dir>
//
// It produces vkvm.ico (Windows), icon_<n>.png for every size, and a macOS
// AppIcon.iconset directory that `iconutil -c icns` turns into AppIcon.icns.
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"vkvm/internal/brand"
)

func main() {
	if len(os.Args) != 2 {
		log.Fatal("usage: go run ./packaging/icon <out-dir>")
	}
	out := os.Args[1]
	iconset := filepath.Join(out, "AppIcon.iconset")
	if err := os.MkdirAll(iconset, 0o755); err != nil {
		log.Fatal(err)
	}
	write := func(path string, data []byte) {
		if err := os.WriteFile(path, data, 0o644); err != nil {
			log.Fatal(err)
		}
	}
	write(filepath.Join(out, "vkvm.ico"), brand.ICO(brand.App, 16, 24, 32, 48, 64, 256))
	for _, size := range []int{16, 32, 64, 128, 256, 512, 1024} {
		data := brand.PNG(size, brand.App)
		write(filepath.Join(out, fmt.Sprintf("icon_%d.png", size)), data)
	}
	for _, size := range []int{16, 32, 128, 256, 512} {
		write(filepath.Join(iconset, fmt.Sprintf("icon_%dx%d.png", size, size)), brand.PNG(size, brand.App))
		write(filepath.Join(iconset, fmt.Sprintf("icon_%dx%d@2x.png", size, size)), brand.PNG(size*2, brand.App))
	}
}
