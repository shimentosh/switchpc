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
	// A repeat of a claim that already succeeded (the first reply was lost or
	// too slow) gets the same answer instead of "pairing is not open".
	if resp, ok := manager.Replay(req.Code, req.DeviceID); ok {
		log.Printf("Pairing: repeated claim from %s answered again", req.Name)
		writePairResponse(w, resp)
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
			replaced = true
			break
		}
	}
	if !replaced {
		cfg.Profiles = append(cfg.Profiles, config.Profile{
			Name: profileName, Hotkey: nextFreeHotkey(cfg), MonitorInputs: map[string]int{},
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
	manager.RecordPaired(pairing.Event{Name: profileName, DeviceID: req.DeviceID})
	log.Printf("Pairing: paired %s (%s, %s) from %s", profileName, req.OS, req.DeviceID, endpoint)

	resp := pairing.Response{
		DeviceID:    cfg.General.DeviceID,
		PublicKey:   cfg.General.IdentityPublicKey,
		Name:        hostProfile,
		Token:       cfg.General.APIToken,
		Port:        cfg.General.APIPort,
		ProfileName: profileName,
		HostProfile: hostProfile,
	}
	manager.Remember(req.Code, req.DeviceID, resp)
	writePairResponse(w, resp)

	// Reading monitors over DDC/CI can take many seconds on Windows, so the
	// Agent's inputs are filled in after answering.
	go s.learnPeerInputs(manager, profileName, req)
}

func writePairResponse(w http.ResponseWriter, resp pairing.Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// learnPeerInputs maps the inputs the Agent reported onto this computer's
// monitors and stores them in the Agent's profile. Inputs already set by hand
// are kept.
func (s *Server) learnPeerInputs(manager *pairing.Manager, profileName string, req pairing.Request) {
	var local []pairing.LocalMonitor
	for attempt := 0; attempt < 3 && len(local) == 0; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		if s.switcher == nil {
			break
		}
		monitors, err := s.switcher.ListMonitors()
		if err != nil {
			log.Printf("Pairing: reading monitors failed: %v", err)
			continue
		}
		for _, m := range monitors {
			local = append(local, pairing.LocalMonitor{ID: m.ID, Name: m.Name, Serial: m.Serial})
		}
	}
	inputs, _ := pairing.MatchInputs(local, req.Monitors)
	if req.DefaultInput > 0 {
		for _, m := range local {
			if _, ok := inputs[m.ID]; !ok {
				inputs[m.ID] = req.DefaultInput
			}
		}
	}

	cfg := s.configMgr.Get()
	unmatched := 0
	for i := range cfg.Profiles {
		p := &cfg.Profiles[i]
		if p.Name != profileName {
			continue
		}
		if p.MonitorInputs == nil {
			p.MonitorInputs = map[string]int{}
		}
		for id, v := range inputs {
			if _, set := p.MonitorInputs[id]; !set {
				p.MonitorInputs[id] = v
			}
		}
		for _, m := range local {
			if _, ok := p.MonitorInputs[m.ID]; !ok {
				unmatched++
			}
		}
		if len(local) == 0 && len(p.MonitorInputs) == 0 {
			unmatched = 1
		}
	}
	s.configMgr.Set(cfg)
	if err := s.configMgr.Save(); err != nil {
		log.Printf("Pairing: saving %s's inputs failed: %v", profileName, err)
	}
	manager.UpdateUnmatched(req.DeviceID, unmatched)
	log.Printf("Pairing: %s inputs %v; %d monitor(s) still without an input", profileName, inputs, unmatched)
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
