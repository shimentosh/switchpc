package ui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"vkvm/internal/config"
	"vkvm/internal/pairing"
)

func newTestAppServer(t *testing.T) (*Server, *config.Manager) {
	t.Helper()
	mgr, err := config.NewManagerAt(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mgr.Load(); err != nil {
		t.Fatal(err)
	}
	return NewAppServer(mgr, "test", pairing.NewManager()), mgr
}

func postJSON(t *testing.T, h http.HandlerFunc, body string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h(rec, req)
	out := map[string]interface{}{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func TestSetupRoleHostEnablesAPIAndNeedsRestart(t *testing.T) {
	s, mgr := newTestAppServer(t)
	code, out := postJSON(t, s.handleSetupRole, `{"role":"host","name":"Desk PC"}`)
	if code != http.StatusOK {
		t.Fatalf("status %d %v", code, out)
	}
	if out["restart_required"] != true {
		t.Fatalf("enabling the API must require a restart: %v", out)
	}
	cfg := mgr.Get()
	if !cfg.General.APIEnabled || len(cfg.General.APIToken) < 32 {
		t.Fatalf("host must get an API and a generated token: %+v", cfg.General)
	}
	if len(cfg.Profiles) != 1 || cfg.Profiles[0].Name != "Desk PC" || cfg.Profiles[0].Hotkey != "Ctrl+Alt+1" {
		t.Fatalf("default placeholders must become the host profile: %+v", cfg.Profiles)
	}

	// Pairing must wait until the restarted core actually serves the API.
	rec := httptest.NewRecorder()
	s.handlePairOpen(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusConflict {
		t.Fatalf("pairing before restart: got %d", rec.Code)
	}
	s.SetRunningConfig(mgr.Get())
	rec = httptest.NewRecorder()
	s.handlePairOpen(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("pairing after restart: got %d %s", rec.Code, rec.Body)
	}
}

func TestHostProfilesRenameKeepsPeer(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.General.PeerProfile = "Mac"
	cfg.Profiles = []config.Profile{
		{Name: "Old", Hotkey: "Ctrl+Alt+1", MonitorInputs: map[string]int{"m": 15}},
		{Name: "Mac", Hotkey: "Ctrl+Alt+2", MonitorInputs: map[string]int{"m": 17}},
		{Name: "PC2", MonitorInputs: map[string]int{}},
	}
	got := hostProfiles(cfg, "Old", "New")
	if len(got) != 2 || got[0].Name != "New" || got[0].MonitorInputs["m"] != 15 || got[1].Name != "Mac" {
		t.Fatalf("unexpected profiles: %+v", got)
	}
}

func TestSharingNeedsPeerAndSetsAgentProfile(t *testing.T) {
	s, mgr := newTestAppServer(t)
	postJSON(t, s.handleSetupRole, `{"role":"host","name":"PC"}`)
	if code, _ := postJSON(t, s.handleSharing, `{"enabled":true}`); code != http.StatusConflict {
		t.Fatalf("sharing without a peer: got %d", code)
	}
	cfg := mgr.Get()
	cfg.General.PeerProfile = "Mac"
	mgr.Set(cfg)
	if code, _ := postJSON(t, s.handleSharing, `{"enabled":true}`); code != http.StatusOK {
		t.Fatalf("sharing on: got %d", code)
	}
	cfg = mgr.Get()
	if !cfg.General.USBForwardingEnabled || cfg.General.AgentProfile != "Mac" {
		t.Fatalf("sharing on: %+v", cfg.General)
	}
	postJSON(t, s.handleSharing, `{"enabled":false}`)
	cfg = mgr.Get()
	if cfg.General.USBForwardingEnabled || cfg.General.AgentProfile != "" {
		t.Fatalf("sharing off must also stop input capture: %+v", cfg.General)
	}
}

func TestLoopbackHostMiddleware(t *testing.T) {
	h := loopbackHostMiddleware(18079, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for host, want := range map[string]int{"127.0.0.1:18079": 200, "localhost:18079": 200, "evil.example:18079": 403} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Fatalf("%s: got %d want %d", host, rec.Code, want)
		}
	}
}
