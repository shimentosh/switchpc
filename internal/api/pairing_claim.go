package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"vkvm/internal/config"
	"vkvm/internal/pairing"
)

// SetPairing attaches the short-code pairing manager that the setup window
// opens on the Host. Without it /api/pair/claim always refuses.
func (s *Server) SetPairing(manager *pairing.Manager) {
	s.mu.Lock()
	s.pairing = manager
	s.mu.Unlock()
}

// handlePairClaim lets a new Agent join with the 6-digit code shown on the
// Host. It is the only unauthenticated mutating endpoint: the code itself is
// the credential, is single-use, short-lived and closes after a few failures.
func (s *Server) handlePairClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	manager := s.pairing
	s.mu.Unlock()
	if manager == nil {
		http.Error(w, pairing.ErrNotOpen.Error(), http.StatusForbidden)
		return
	}
	cfg := s.configMgr.Get()
	if cfg.General.Role != "host" {
		http.Error(w, "this computer is not set up as the main computer", http.StatusConflict)
		return
	}

	var req pairing.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		http.Error(w, "invalid pairing request", http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.DeviceID == "" || req.Name == "" || len(req.Name) > 64 {
		http.Error(w, "device_id and a name (max 64 characters) are required", http.StatusBadRequest)
		return
	}
	if req.DeviceID == cfg.General.DeviceID {
		http.Error(w, "a computer cannot pair with itself", http.StatusBadRequest)
		return
	}
	if err := manager.Verify(req.Code); err != nil {
		status := http.StatusForbidden
		if errors.Is(err, pairing.ErrWrongCode) {
			status = http.StatusUnauthorized
		}
		log.Printf("Pairing: rejected claim from %s (%s): %v", req.Name, r.RemoteAddr, err)
		http.Error(w, err.Error(), status)
		return
	}

	endpoint := ""
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		endpoint = host
	}
	if err := s.configMgr.PairDevice(config.PairedDevice{
		DeviceID: req.DeviceID, PublicKey: req.PublicKey, Role: "agent", LastSeenAt: time.Now().UTC(),
	}); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Build the Agent's profile from the inputs it is showing right now.
	var local []pairing.LocalMonitor
	if s.switcher != nil {
		if monitors, err := s.switcher.ListMonitors(); err == nil {
			for _, m := range monitors {
				local = append(local, pairing.LocalMonitor{ID: m.ID, Name: m.Name, Serial: m.Serial})
			}
		}
	}
	inputs, unmatched := pairing.MatchInputs(local, req.Monitors)

	cfg = s.configMgr.Get()
	hostProfile := cfg.General.DisplayName
	if hostProfile == "" && len(cfg.Profiles) > 0 {
		hostProfile = cfg.Profiles[0].Name
	}
	profileName := uniqueProfileName(cfg, req.Name, cfg.General.PeerProfile, hostProfile)
	replaced := false
	for i := range cfg.Profiles {
		if cfg.Profiles[i].Name == cfg.General.PeerProfile && cfg.General.PeerProfile != "" {
			cfg.Profiles[i].Name = profileName
			cfg.Profiles[i].MonitorInputs = inputs
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Profiles = append(cfg.Profiles, config.Profile{
			Name: profileName, Hotkey: nextFreeHotkey(cfg), MonitorInputs: inputs,
		})
	}
	if cfg.General.USBForwardingEnabled {
		cfg.General.AgentProfile = profileName
	}
	cfg.General.PeerProfile = profileName
	s.configMgr.Set(cfg)
	if err := s.configMgr.Save(); err != nil {
		http.Error(w, "failed to save pairing", http.StatusInternalServerError)
		return
	}
	manager.RecordPaired(pairing.Event{Name: profileName, DeviceID: req.DeviceID, Unmatched: unmatched})
	log.Printf("Pairing: paired %s (%s, %s) from %s; %d monitor(s) without an input", profileName, req.OS, req.DeviceID, endpoint, unmatched)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(pairing.Response{
		DeviceID:    cfg.General.DeviceID,
		PublicKey:   cfg.General.IdentityPublicKey,
		Name:        hostProfile,
		Token:       cfg.General.APIToken,
		Port:        cfg.General.APIPort,
		ProfileName: profileName,
		HostProfile: hostProfile,
		Unmatched:   unmatched,
	})
}

// uniqueProfileName returns want, or want with a number appended when another
// profile (other than the one being replaced) already uses it.
func uniqueProfileName(cfg *config.Config, want, replacing, reserved string) string {
	taken := func(name string) bool {
		if strings.EqualFold(name, reserved) {
			return true
		}
		for _, p := range cfg.Profiles {
			if strings.EqualFold(p.Name, name) && p.Name != replacing {
				return true
			}
		}
		return false
	}
	name := want
	for i := 2; taken(name) && i < 100; i++ {
		name = fmt.Sprintf("%s %d", want, i)
	}
	return name
}

// nextFreeHotkey picks the first Ctrl+Alt+<n> no profile uses yet.
func nextFreeHotkey(cfg *config.Config) string {
	used := map[string]bool{}
	for _, p := range cfg.Profiles {
		used[strings.ToUpper(p.Hotkey)] = true
	}
	for n := 1; n <= 9; n++ {
		hk := fmt.Sprintf("Ctrl+Alt+%d", n)
		if !used[strings.ToUpper(hk)] {
			return hk
		}
	}
	return ""
}
