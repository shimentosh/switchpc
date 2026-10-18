package ui

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"vkvm/internal/autostart"
	"vkvm/internal/brand"
	"vkvm/internal/config"
	"vkvm/internal/input"
	"vkvm/internal/network"
	"vkvm/internal/pairing"
)

// DefaultAppPort is the fixed loopback port of the app window server. A
// fixed port lets a second launch find the running instance and lets the
// window reconnect by itself after the app restarts.
const DefaultAppPort = 18079

//go:embed web
var webFiles embed.FS

// appState holds what only the long-running app server needs.
type appState struct {
	version   string
	port      int
	pairing   *pairing.Manager
	onRestart func()
	onQuit    func()
	running   *config.Config // configuration the core was started with

	windowClients atomic.Int32
	focusMu       sync.Mutex
	focusCh       chan struct{}
	spawnMu       sync.Mutex
	lastSpawn     time.Time
}

// NewAppServer creates the server behind the native app window.
func NewAppServer(cfgMgr *config.Manager, version string, pm *pairing.Manager) *Server {
	return &Server{
		configMgr: cfgMgr,
		app: &appState{
			version: version,
			pairing: pm,
			focusCh: make(chan struct{}),
			running: cfgMgr.Get(),
		},
	}
}

// SetLifecycle sets what Restart and Quit do.
func (s *Server) SetLifecycle(onRestart, onQuit func()) {
	s.app.onRestart = onRestart
	s.app.onQuit = onQuit
}

// SetRunningConfig records the configuration the core actually started with,
// so the UI can tell when a change only applies after a restart.
func (s *Server) SetRunningConfig(cfg *config.Config) {
	s.app.running = cfg
}

// Listen binds the app server to 127.0.0.1:port.
func (s *Server) Listen(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	s.listener = ln
	s.app.port = ln.Addr().(*net.TCPAddr).Port
	return nil
}

// URL is the address the window loads.
func (s *Server) URL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/", s.app.port)
}

// Serve serves the app UI on the listener opened by Listen. It blocks.
func (s *Server) Serve() error {
	mux := http.NewServeMux()
	static, _ := fs.Sub(webFiles, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/classic", s.handleIndex)
	mux.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(brand.PNG(128, brand.App))
	})
	// Shared with the classic page.
	mux.HandleFunc("/api/monitors", s.handleMonitors)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/switch", s.handleSwitch)
	mux.HandleFunc("/api/test", s.handleTest)
	mux.HandleFunc("/api/discover", s.handleUIDiscover)
	mux.HandleFunc("/api/test-remote", s.handleTestRemote)
	mux.HandleFunc("/api/sync-to", s.handleSyncTo)
	mux.HandleFunc("/api/sleep-display", s.handleSleepDisplay)
	mux.HandleFunc("/api/connection-status", s.handleConnectionStatus)
	mux.HandleFunc("/api/permissions", s.handlePermissions)
	mux.HandleFunc("/api/autostart", s.handleAutostart)
	// App window.
	mux.HandleFunc("/api/app/ping", s.handlePing)
	mux.HandleFunc("/api/app/activate", s.handleActivate)
	mux.HandleFunc("/api/app/events", s.handleEvents)
	mux.HandleFunc("/api/app/restart", s.handleRestart)
	mux.HandleFunc("/api/app/quit", s.handleQuit)
	mux.HandleFunc("/api/state", s.handleState)
	mux.HandleFunc("/api/monitors/detect", s.handleDetect)
	mux.HandleFunc("/api/setup/role", s.handleSetupRole)
	mux.HandleFunc("/api/setup/finish", s.handleSetupFinish)
	mux.HandleFunc("/api/profile", s.handleProfile)
	mux.HandleFunc("/api/hotkeys", s.handleHotkeys)
	mux.HandleFunc("/api/sharing", s.handleSharing)
	mux.HandleFunc("/api/permissions/request", s.handlePermissionRequest)
	mux.HandleFunc("/api/pair/open", s.handlePairOpen)
	mux.HandleFunc("/api/pair/close", s.handlePairClose)
	mux.HandleFunc("/api/pair/status", s.handlePairStatus)
	mux.HandleFunc("/api/pair/discover", s.handlePairDiscover)
	mux.HandleFunc("/api/pair/join", s.handlePairJoin)
	mux.HandleFunc("/api/pair/forget", s.handlePairForget)

	server := &http.Server{
		Handler:           loopbackHostMiddleware(s.app.port, csrfMiddleware(mux)),
		ReadHeaderTimeout: 5 * time.Second,
	}
	err := server.Serve(s.listener)
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// loopbackHostMiddleware rejects requests whose Host header is not the
// loopback address. With a fixed port this blocks DNS-rebinding pages from
// talking to the app server through a hostile domain name.
func loopbackHostMiddleware(port int, next http.Handler) http.Handler {
	allowed := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", port): true,
		fmt.Sprintf("localhost:%d", port): true,
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowed[r.Host] {
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

// ShowWindow brings the app window to the front, opening it if needed.
func (s *Server) ShowWindow() {
	if s.app == nil {
		return
	}
	if s.app.windowClients.Load() > 0 {
		s.app.focusMu.Lock()
		close(s.app.focusCh)
		s.app.focusCh = make(chan struct{})
		s.app.focusMu.Unlock()
		return
	}
	s.app.spawnMu.Lock()
	defer s.app.spawnMu.Unlock()
	// A window that was just launched has not connected yet.
	if time.Since(s.app.lastSpawn) < 5*time.Second {
		return
	}
	s.app.lastSpawn = time.Now()
	exe, err := os.Executable()
	if err != nil {
		log.Printf("Window: cannot locate executable: %v", err)
		return
	}
	cmd := exec.Command(exe, "-window", s.URL())
	if err := cmd.Start(); err != nil {
		log.Printf("Window: failed to start: %v", err)
		return
	}
	go func() { _ = cmd.Wait() }()
}

// ActivateRunning asks an already running VKVM on port to show its window.
// It reports whether a VKVM instance answered.
func ActivateRunning(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	resp, err := client.Get(base + "/api/app/ping")
	if err != nil {
		return false
	}
	var ping struct {
		App string `json:"app"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&ping)
	resp.Body.Close()
	if ping.App != "vkvm" {
		return false
	}
	req, _ := http.NewRequest(http.MethodPost, base+"/api/app/activate", nil)
	req.Header.Set("Origin", base)
	if resp, err := client.Do(req); err == nil {
		resp.Body.Close()
	}
	return true
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v interface{}) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(v); err != nil && err != io.EOF {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return false
	}
	return true
}

func (s *Server) save(w http.ResponseWriter, cfg *config.Config) bool {
	s.configMgr.Set(cfg)
	if err := s.configMgr.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return false
	}
	return true
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]string{"app": "vkvm", "version": s.app.version})
}

func (s *Server) handleActivate(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.ShowWindow()
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleEvents is a long poll held open by the app window. While one is
// open the window counts as present, and ShowWindow focuses it instead of
// launching another one.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	s.app.windowClients.Add(1)
	defer s.app.windowClients.Add(-1)
	s.app.focusMu.Lock()
	ch := s.app.focusCh
	s.app.focusMu.Unlock()
	timer := time.NewTimer(25 * time.Second)
	defer timer.Stop()
	focus := false
	select {
	case <-ch:
		focus = true
	case <-timer.C:
	case <-r.Context().Done():
		return
	}
	writeJSON(w, map[string]bool{"focus": focus})
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	writeJSON(w, map[string]string{"status": "restarting"})
	if s.app.onRestart != nil {
		go func() {
			time.Sleep(300 * time.Millisecond)
			s.app.onRestart()
		}()
	}
}

func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	writeJSON(w, map[string]string{"status": "quitting"})
	if s.app.onQuit != nil {
		go func() {
			time.Sleep(300 * time.Millisecond)
			s.app.onQuit()
		}()
	}
}

// restartPending reports settings that only take effect after a restart.
func (s *Server) restartPending(cfg *config.Config) bool {
	running := s.app.running
	if running == nil {
		return false
	}
	if len(config.ClassifyChange(running, cfg).RestartRequired) > 0 {
		return true
	}
	// The Agent builds its connection to the paired Host at startup.
	return cfg.General.Role == "agent" && pairedHosts(running) != pairedHosts(cfg)
}

func pairedHosts(cfg *config.Config) string {
	var ids []string
	for _, p := range cfg.General.PairedDevices {
		if p.Enabled && p.Role == "host" {
			ids = append(ids, p.DeviceID)
		}
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// DefaultName suggests a friendly name for this computer.
func DefaultName() string {
	name, _ := os.Hostname()
	name = strings.TrimSuffix(strings.TrimSuffix(name, ".local"), ".lan")
	name = strings.ReplaceAll(name, "-", " ")
	if name == "" || len(name) > 32 {
		if runtime.GOOS == "darwin" {
			return "Mac"
		}
		return "PC"
	}
	return name
}

// ThisProfile is the profile that switches the monitors to this computer.
func ThisProfile(cfg *config.Config) string {
	if cfg.General.Role == "agent" {
		return cfg.General.AgentProfile
	}
	if cfg.General.DisplayName != "" {
		return cfg.General.DisplayName
	}
	if len(cfg.Profiles) > 0 {
		return cfg.Profiles[0].Name
	}
	return ""
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	cfg := s.configMgr.Get()
	ips, _ := network.GetLocalIPs()
	type peer struct {
		DeviceID string    `json:"device_id"`
		Role     string    `json:"role"`
		Endpoint string    `json:"endpoint,omitempty"`
		LastSeen time.Time `json:"last_seen,omitempty"`
	}
	peers := []peer{}
	for _, p := range cfg.General.PairedDevices {
		if p.Enabled {
			peers = append(peers, peer{p.DeviceID, p.Role, p.LastKnownEndpoint, p.LastSeenAt})
		}
	}
	profiles := cfg.Profiles
	if profiles == nil {
		profiles = []config.Profile{}
	}
	state := map[string]interface{}{
		"version":         s.app.version,
		"os":              runtime.GOOS,
		"setup_complete":  cfg.General.SetupComplete,
		"role":            cfg.General.Role,
		"name":            cfg.General.DisplayName,
		"default_name":    DefaultName(),
		"api_enabled":     cfg.General.APIEnabled,
		"api_port":        cfg.General.APIPort,
		"local_ips":       ips,
		"profiles":        profiles,
		"current_profile": cfg.General.CurrentProfile,
		"this_profile":    ThisProfile(cfg),
		"peer_profile":    cfg.General.PeerProfile,
		"peers":           peers,
		"sharing_enabled": cfg.General.USBForwardingEnabled,
		"start_on_boot":   autostart.IsEnabled(),
		"permissions":     input.CurrentPermissions(),
		"settings_hotkey": cfg.General.SettingsHotkey,
		"sleep_hotkey":    cfg.General.SleepHotkey,
		"restart_pending": s.restartPending(cfg),
		"ddc_available":   s.sw() != nil,
	}
	if sw := s.sw(); sw != nil && cfg.General.Role == "agent" {
		state["connection"] = map[string]interface{}{
			"connected": sw.IsConnectedToCheck(),
			"state":     sw.AgentTransportState(),
			"error":     sw.AgentTransportError(),
		}
	}
	writeJSON(w, state)
}

type detectedMonitor struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Serial       string `json:"serial,omitempty"`
	DDCSupported bool   `json:"ddc_supported"`
	CurrentInput int    `json:"current_input"`
}

// detectMonitors lists monitors with the input each one shows right now.
func (s *Server) detectMonitors() ([]detectedMonitor, error) {
	sw := s.sw()
	if sw == nil {
		return nil, errors.New("monitor control is unavailable on this computer")
	}
	monitors, err := sw.ListMonitors()
	if err != nil {
		return nil, err
	}
	current, _ := sw.GetCurrentInputs()
	out := make([]detectedMonitor, 0, len(monitors))
	for _, m := range monitors {
		d := detectedMonitor{ID: m.ID, Name: m.Name, Serial: m.Serial, DDCSupported: m.DDCSupported}
		if in, ok := current[m.ID]; ok {
			d.CurrentInput = int(in)
		} else if m.InputSource > 0 {
			d.CurrentInput = int(m.InputSource)
		}
		out = append(out, d)
	}
	return out, nil
}

func (s *Server) handleDetect(w http.ResponseWriter, r *http.Request) {
	monitors, err := s.detectMonitors()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, monitors)
}

// handleSetupRole sets this computer's name and whether it is the main
// computer (Host) or the second one (Agent).
func (s *Server) handleSetupRole(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Role string `json:"role"`
		Name string `json:"name"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 32 {
		http.Error(w, "enter a name of up to 32 characters", http.StatusBadRequest)
		return
	}
	if req.Role != "host" && req.Role != "agent" {
		http.Error(w, "role must be host or agent", http.StatusBadRequest)
		return
	}
	cfg := s.configMgr.Get()
	oldName := ThisProfile(cfg)
	cfg.General.Role = req.Role
	cfg.General.DisplayName = req.Name
	if req.Role == "host" {
		cfg.General.APIEnabled = true
		if cfg.General.APIToken == "" {
			cfg.General.APIToken = config.NewAPIToken()
		}
		cfg.General.CoordinatorAddr = ""
		cfg.Profiles = hostProfiles(cfg, oldName, req.Name)
		cfg.General.CurrentProfile = req.Name
	} else {
		// The Agent's profiles come from the Host after pairing.
		cfg.General.APIEnabled = false
	}
	if !s.save(w, cfg) {
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "restart_required": s.restartPending(cfg)})
}

// hostProfiles makes sure the Host has a profile named after itself (renaming
// its previous own profile or an untouched default) and keeps the peer's.
func hostProfiles(cfg *config.Config, oldName, newName string) []config.Profile {
	var out []config.Profile
	own := -1
	for _, p := range cfg.Profiles {
		isDefault := (p.Name == "PC1" || p.Name == "PC2") && len(p.MonitorInputs) == 0
		switch {
		case p.Name == oldName || p.Name == newName:
			if own == -1 {
				p.Name = newName
				own = len(out)
				out = append(out, p)
			}
		case isDefault && p.Name != cfg.General.PeerProfile:
			// drop untouched first-run placeholders
		default:
			out = append(out, p)
		}
	}
	if own == -1 {
		own = 0
		out = append([]config.Profile{{Name: newName, MonitorInputs: map[string]int{}}}, out...)
	}
	if out[own].MonitorInputs == nil {
		out[own].MonitorInputs = map[string]int{}
	}
	if out[own].Hotkey == "" {
		out[own].Hotkey = "Ctrl+Alt+1"
	}
	return out
}

func (s *Server) handleSetupFinish(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		StartOnBoot *bool `json:"start_on_boot"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	cfg := s.configMgr.Get()
	cfg.General.SetupComplete = true
	if req.StartOnBoot != nil {
		cfg.General.StartOnBoot = *req.StartOnBoot
		var err error
		if *req.StartOnBoot {
			err = autostart.Enable()
		} else if autostart.IsEnabled() {
			err = autostart.Disable()
		}
		if err != nil {
			log.Printf("Setup: start at login: %v", err)
		}
	}
	if !s.save(w, cfg) {
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "restart_required": s.restartPending(cfg)})
}

// handleProfile updates one profile: its hotkey and monitor inputs. Profiles
// live on the main computer; the second computer receives them by sync.
func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Name          string          `json:"name"`
		Hotkey        *string         `json:"hotkey"`
		MonitorInputs map[string]int  `json:"monitor_inputs"`
		SetInputs     map[string]*int `json:"set_inputs"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	cfg := s.configMgr.Get()
	if cfg.General.Role == "agent" {
		http.Error(w, "profiles are edited on the main computer", http.StatusConflict)
		return
	}
	for i := range cfg.Profiles {
		p := &cfg.Profiles[i]
		if p.Name != req.Name {
			continue
		}
		if req.Hotkey != nil {
			p.Hotkey = strings.TrimSpace(*req.Hotkey)
		}
		if req.MonitorInputs != nil {
			p.MonitorInputs = map[string]int{}
			for id, v := range req.MonitorInputs {
				if id != "" && v > 0 {
					p.MonitorInputs[id] = v
				}
			}
		}
		if p.MonitorInputs == nil {
			p.MonitorInputs = map[string]int{}
		}
		for id, v := range req.SetInputs {
			if id == "" {
				continue
			}
			if v == nil || *v <= 0 {
				delete(p.MonitorInputs, id)
			} else {
				p.MonitorInputs[id] = *v
			}
		}
		if !s.save(w, cfg) {
			return
		}
		writeJSON(w, map[string]string{"status": "ok"})
		return
	}
	http.Error(w, "profile not found", http.StatusNotFound)
}

func (s *Server) handleHotkeys(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Settings *string `json:"settings_hotkey"`
		Sleep    *string `json:"sleep_hotkey"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	cfg := s.configMgr.Get()
	if req.Settings != nil {
		cfg.General.SettingsHotkey = strings.TrimSpace(*req.Settings)
	}
	if req.Sleep != nil {
		cfg.General.SleepHotkey = strings.TrimSpace(*req.Sleep)
	}
	if !s.save(w, cfg) {
		return
	}
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleSharing turns keyboard and mouse sharing on the main computer on or
// off. When on, the main computer's keyboard and mouse control the second
// computer while the monitors show it.
func (s *Server) handleSharing(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	cfg := s.configMgr.Get()
	if cfg.General.Role != "host" {
		http.Error(w, "keyboard and mouse sharing is set on the main computer", http.StatusConflict)
		return
	}
	if req.Enabled && cfg.General.PeerProfile == "" {
		http.Error(w, "pair the second computer first", http.StatusConflict)
		return
	}
	cfg.General.USBForwardingEnabled = req.Enabled
	if req.Enabled {
		cfg.General.AgentProfile = cfg.General.PeerProfile
	} else {
		cfg.General.AgentProfile = ""
	}
	if !s.save(w, cfg) {
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "enabled": req.Enabled})
}

func (s *Server) handlePermissionRequest(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if req.Kind != "accessibility" && req.Kind != "input_monitoring" {
		http.Error(w, "unknown permission", http.StatusBadRequest)
		return
	}
	input.RequestPermission(req.Kind)
	writeJSON(w, input.CurrentPermissions())
}

func (s *Server) handlePairOpen(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	cfg := s.configMgr.Get()
	if cfg.General.Role != "host" || !cfg.General.APIEnabled {
		http.Error(w, "set this computer up as the main computer first", http.StatusConflict)
		return
	}
	if s.restartPending(cfg) {
		http.Error(w, "restart VKVM to finish switching to the main computer", http.StatusConflict)
		return
	}
	if _, err := s.app.pairing.Open(pairing.DefaultTTL); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.app.pairing.Status())
}

func (s *Server) handlePairClose(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	s.app.pairing.Close()
	writeJSON(w, s.app.pairing.Status())
}

func (s *Server) handlePairStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.app.pairing.Status())
}

// handlePairDiscover lists main computers found on the local network.
func (s *Server) handlePairDiscover(w http.ResponseWriter, r *http.Request) {
	cfg := s.configMgr.Get()
	port := cfg.General.APIPort
	if port == 0 {
		port = 18080
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	hosts, err := network.DiscoverVKVM(ctx, port)
	if err != nil && len(hosts) == 0 {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	out := []network.DiscoveredHost{}
	for _, h := range hosts {
		if h.DeviceID != "" && h.DeviceID == cfg.General.DeviceID {
			continue
		}
		if h.Role != "" && h.Role != "host" {
			continue
		}
		out = append(out, h)
	}
	writeJSON(w, out)
}

// handlePairJoin sends this computer's pairing request to the main computer
// and, on success, turns this computer into its Agent.
func (s *Server) handlePairJoin(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		Address string `json:"address"`
		Code    string `json:"code"`
		Input   int    `json:"input"` // optional: the input this computer is on
	}
	if !decodeBody(w, r, &req) {
		return
	}
	cfg := s.configMgr.Get()
	addr := strings.TrimSpace(req.Address)
	if _, _, err := net.SplitHostPort(addr); err != nil {
		port := cfg.General.APIPort
		if port == 0 {
			port = 18080
		}
		addr = net.JoinHostPort(addr, strconv.Itoa(port))
	}
	addr, err := validateRemoteAddress(addr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	name := cfg.General.DisplayName
	if name == "" {
		name = DefaultName()
	}
	claim := pairing.Request{
		Code: req.Code, DeviceID: cfg.General.DeviceID, PublicKey: cfg.General.IdentityPublicKey,
		Name: name, OS: runtime.GOOS, DefaultInput: req.Input,
	}
	if monitors, err := s.detectMonitors(); err == nil {
		for _, m := range monitors {
			in := m.CurrentInput
			if req.Input > 0 {
				in = req.Input
			}
			claim.Monitors = append(claim.Monitors, pairing.MonitorInput{ID: m.ID, Name: m.Name, Serial: m.Serial, Input: in})
		}
	}
	body, _ := json.Marshal(claim)
	client := &http.Client{Timeout: 45 * time.Second}
	resp, err := client.Post("http://"+addr+"/api/pair/claim", "application/json", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "could not reach the main computer: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if msg == "" {
			msg = resp.Status
		}
		http.Error(w, msg, http.StatusBadRequest)
		return
	}
	var answer pairing.Response
	if err := json.Unmarshal(data, &answer); err != nil || answer.Token == "" || answer.DeviceID == "" {
		http.Error(w, "the main computer sent an invalid answer", http.StatusBadGateway)
		return
	}

	cfg = s.configMgr.Get()
	cfg.General.Role = "agent"
	cfg.General.APIToken = answer.Token
	cfg.General.CoordinatorAddr = addr
	cfg.General.AgentProfile = answer.ProfileName
	cfg.General.DisplayName = answer.ProfileName
	cfg.General.PeerProfile = answer.HostProfile
	if answer.Port > 0 {
		cfg.General.APIPort = answer.Port
	}
	// Only one main computer at a time.
	var keep []config.PairedDevice
	for _, p := range cfg.General.PairedDevices {
		if p.Role != "host" {
			keep = append(keep, p)
		}
	}
	cfg.General.PairedDevices = keep
	s.configMgr.Set(cfg)
	if err := s.configMgr.PairDevice(config.PairedDevice{
		DeviceID: answer.DeviceID, PublicKey: answer.PublicKey, Role: "host",
		LastKnownEndpoint: addr, LastSeenAt: time.Now().UTC(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	if err := s.configMgr.Save(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	log.Printf("Pairing: joined %s at %s as %q", answer.Name, addr, answer.ProfileName)
	writeJSON(w, map[string]interface{}{
		"status": "paired", "host": answer.Name, "profile": answer.ProfileName,
		"unmatched_monitors": answer.Unmatched, "restart_required": true,
	})
}

// handlePairForget removes a paired computer.
func (s *Server) handlePairForget(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	var req struct {
		DeviceID string `json:"device_id"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	if !s.configMgr.UnpairDevice(req.DeviceID) {
		http.Error(w, "not paired", http.StatusNotFound)
		return
	}
	cfg := s.configMgr.Get()
	if cfg.General.Role == "agent" {
		cfg.General.CoordinatorAddr = ""
		cfg.General.APIToken = ""
	} else {
		cfg.General.USBForwardingEnabled = false
		cfg.General.AgentProfile = ""
	}
	if !s.save(w, cfg) {
		return
	}
	writeJSON(w, map[string]interface{}{"status": "ok", "restart_required": s.restartPending(cfg)})
}
