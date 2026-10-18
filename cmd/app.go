package main

import (
	"flag"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"vkvm/internal/config"
	"vkvm/internal/pairing"
	"vkvm/internal/ui"
)

var (
	windowURL = flag.String("window", "", "Internal: show the app window for this loopback URL")
	restarted = flag.Bool("restarted", false, "Internal: started by VKVM restarting itself")
)

// The app window server and pairing state live for the whole process, across
// supervisor restarts of the core.
var (
	appUI            *ui.Server
	pairingMgr       = pairing.NewManager()
	restartRequested atomic.Bool
	quitMu           sync.Mutex
	quitApp          func()
)

// startAppUI binds the loopback app server. It doubles as the single-instance
// guard: when another VKVM already owns the port, that instance is asked to
// show its window and this process reports false so it can exit.
func startAppUI(cfgMgr *config.Manager) bool {
	server := ui.NewAppServer(cfgMgr, version, pairingMgr)
	port := ui.DefaultAppPort
	// VKVM_APP_PORT lets a second test instance run beside the real one.
	if v, err := strconv.Atoi(os.Getenv("VKVM_APP_PORT")); err == nil && v > 0 {
		port = v
	}
	err := server.Listen(port)
	if err != nil && *restarted {
		// The previous process may still be releasing the port.
		for i := 0; i < 40 && err != nil; i++ {
			time.Sleep(250 * time.Millisecond)
			err = server.Listen(port)
		}
	}
	if err != nil {
		if ui.ActivateRunning(port) {
			log.Printf("VKVM is already running; showing its window")
			return false
		}
		log.Printf("App port %d unavailable (%v); using a random port", port, err)
		if err := server.Listen(0); err != nil {
			log.Printf("App window server unavailable: %v", err)
			return true
		}
	}
	server.SetLifecycle(
		func() {
			// Start the replacement first: on macOS quitting the tray ends the
			// process inside AppKit, so nothing after requestQuit would run.
			// The new process waits until this one has released the ports.
			restartRequested.Store(true)
			if err := startDetached("-restarted"); err != nil {
				log.Printf("Restart failed: %v", err)
				return
			}
			requestQuit()
		},
		requestQuit,
	)
	appUI = server
	go func() {
		if err := server.Serve(); err != nil {
			log.Printf("App window server stopped: %v", err)
		}
	}()
	log.Printf("App window server at %s", server.URL())
	return true
}

func setQuit(fn func()) {
	quitMu.Lock()
	quitApp = fn
	quitMu.Unlock()
}

func requestQuit() {
	quitMu.Lock()
	fn := quitApp
	quitMu.Unlock()
	if fn != nil {
		fn()
		return
	}
	os.Exit(0)
}

// showWindow opens or focuses the app window. VKVM_NO_WINDOW=1 suppresses it
// for automated tests.
func showWindow() {
	if appUI != nil && os.Getenv("VKVM_NO_WINDOW") == "" {
		appUI.ShowWindow()
	}
}

// setupLogFile mirrors the log into vkvm.log in the user cache directory
// (%LOCALAPPDATA%kvm on Windows, ~/Library/Caches/vkvm on macOS), since
// release builds have no console. The file is started over once it passes 2 MB.
func setupLogFile() {
	base, err := os.UserCacheDir()
	if err != nil {
		return
	}
	dir := filepath.Join(base, "vkvm")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	path := filepath.Join(dir, "vkvm.log")
	flags := os.O_CREATE | os.O_WRONLY | os.O_APPEND
	if info, err := os.Stat(path); err == nil && info.Size() > 2<<20 {
		flags = os.O_CREATE | os.O_WRONLY | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	log.Printf("---- VKVM %s starting (pid %d) ----", version, os.Getpid())
}

// finishApp closes the app server once the core has stopped.
func finishApp() {
	if appUI != nil {
		_ = appUI.Stop()
	}
}

// startDetached starts another VKVM process that outlives this one.
func startDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("Started VKVM %v (pid %d)", args, cmd.Process.Pid)
	go func() { _ = cmd.Wait() }()
	return nil
}
