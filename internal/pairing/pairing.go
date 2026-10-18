// Package pairing implements the short-code pairing used by the first-run
// setup: the main computer (Host) shows a 6-digit code, the second computer
// (Agent) sends it back together with its identity and the monitor inputs it
// is currently displaying, and the Host answers with the shared API token.
//
// The code is only valid while the Host's setup window has pairing open, for
// a limited time and a limited number of wrong guesses.
package pairing

import (
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultTTL is how long a pairing code stays valid.
	DefaultTTL = 10 * time.Minute
	// MaxFailures closes pairing after this many wrong codes.
	MaxFailures = 5
)

var (
	ErrNotOpen      = errors.New("pairing is not open on the main computer")
	ErrExpired      = errors.New("the pairing code expired; open pairing again on the main computer")
	ErrWrongCode    = errors.New("wrong pairing code")
	ErrTooManyTries = errors.New("too many wrong codes; open pairing again on the main computer")
)

// MonitorInput describes one monitor and the input it currently shows, as
// seen from the computer that sends it.
type MonitorInput struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Serial string `json:"serial,omitempty"`
	Input  int    `json:"input"`
}

// Request is sent by the Agent to the Host's /api/pair/claim endpoint.
type Request struct {
	Code      string         `json:"code"`
	DeviceID  string         `json:"device_id"`
	PublicKey string         `json:"public_key"`
	Name      string         `json:"name"`
	OS        string         `json:"os"`
	Monitors  []MonitorInput `json:"monitors"`
}

// Response is the Host's answer to a successful claim.
type Response struct {
	DeviceID    string `json:"device_id"`
	PublicKey   string `json:"public_key"`
	Name        string `json:"name"`
	Token       string `json:"token"`
	Port        int    `json:"port"`
	ProfileName string `json:"profile_name"` // the Agent's profile on the Host
	HostProfile string `json:"host_profile"`
	Unmatched   int    `json:"unmatched_monitors"`
}

// Event records the most recent successful pairing for the Host UI.
type Event struct {
	Name      string    `json:"name"`
	DeviceID  string    `json:"device_id"`
	Unmatched int       `json:"unmatched_monitors"`
	At        time.Time `json:"at"`
}

// Status is the Host-side view of the pairing window.
type Status struct {
	Open      bool      `json:"open"`
	Code      string    `json:"code,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Last      *Event    `json:"last,omitempty"`
}

// Manager holds the current pairing code. The zero value is closed.
type Manager struct {
	mu       sync.Mutex
	code     string
	expires  time.Time
	failures int
	last     *Event
	now      func() time.Time
}

// NewManager returns a closed pairing manager.
func NewManager() *Manager {
	return &Manager{now: time.Now}
}

// Open generates a fresh code valid for ttl and returns it.
func (m *Manager) Open(ttl time.Duration) (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.code = fmt.Sprintf("%06d", n.Int64())
	m.expires = m.now().Add(ttl)
	m.failures = 0
	return m.code, nil
}

// Close invalidates the current code.
func (m *Manager) Close() {
	m.mu.Lock()
	m.code = ""
	m.mu.Unlock()
}

// Status reports whether pairing is open and the last successful pairing.
func (m *Manager) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{Last: m.last}
	if m.code != "" && m.now().Before(m.expires) {
		st.Open = true
		st.Code = m.code
		st.ExpiresAt = m.expires
	}
	return st
}

// Verify checks a submitted code. A correct code is consumed so it cannot be
// replayed; repeated wrong codes close pairing.
func (m *Manager) Verify(code string) error {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.code == "" {
		return ErrNotOpen
	}
	if !m.now().Before(m.expires) {
		m.code = ""
		return ErrExpired
	}
	if subtle.ConstantTimeCompare([]byte(code), []byte(m.code)) != 1 {
		m.failures++
		if m.failures >= MaxFailures {
			m.code = ""
			return ErrTooManyTries
		}
		return ErrWrongCode
	}
	m.code = ""
	return nil
}

// RecordPaired stores the last successful pairing for the Host UI.
func (m *Manager) RecordPaired(ev Event) {
	if ev.At.IsZero() {
		ev.At = m.now()
	}
	m.mu.Lock()
	m.last = &ev
	m.mu.Unlock()
}

// LocalMonitor is the subset of a locally detected monitor used for matching.
type LocalMonitor struct {
	ID     string
	Name   string
	Serial string
}

// MatchInputs maps the remote computer's monitor inputs onto local monitor
// IDs. The two computers name the same physical monitor differently, so
// monitors are matched by serial number, then by model name, and finally, if
// each side sees exactly one monitor, by elimination. It returns the mapping
// and the number of local monitors left without an input.
func MatchInputs(local []LocalMonitor, remote []MonitorInput) (map[string]int, int) {
	result := make(map[string]int)
	used := make(map[int]bool)
	norm := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

	match := func(key func(LocalMonitor) string, rkey func(MonitorInput) string) {
		for _, lm := range local {
			if _, done := result[lm.ID]; done || key(lm) == "" {
				continue
			}
			candidate := -1
			for i, rm := range remote {
				if used[i] || rm.Input <= 0 || rkey(rm) != key(lm) {
					continue
				}
				if candidate != -1 { // ambiguous
					candidate = -2
					break
				}
				candidate = i
			}
			if candidate >= 0 {
				used[candidate] = true
				result[lm.ID] = remote[candidate].Input
			}
		}
	}
	match(func(m LocalMonitor) string { return norm(m.Serial) }, func(m MonitorInput) string { return norm(m.Serial) })
	match(func(m LocalMonitor) string { return norm(m.Name) }, func(m MonitorInput) string { return norm(m.Name) })

	if len(local) == 1 && len(remote) == 1 && len(result) == 0 && remote[0].Input > 0 {
		result[local[0].ID] = remote[0].Input
	}
	if len(local) == 0 {
		// Nothing to match against (DDC not answering here): every remote
		// monitor still needs its input set by hand.
		return result, len(remote)
	}
	return result, len(local) - len(result)
}
