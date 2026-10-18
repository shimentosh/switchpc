package pairing

import (
	"testing"
	"time"
)

func TestCodeLifecycle(t *testing.T) {
	now := time.Unix(1000, 0)
	m := NewManager()
	m.now = func() time.Time { return now }

	if err := m.Verify("123456"); err != ErrNotOpen {
		t.Fatalf("closed manager: got %v", err)
	}
	code, err := m.Open(time.Minute)
	if err != nil || len(code) != 6 {
		t.Fatalf("open: %q %v", code, err)
	}
	if st := m.Status(); !st.Open || st.Code != code {
		t.Fatalf("status after open: %+v", st)
	}
	if err := m.Verify(" " + code[:3] + " " + code[3:]); err != nil {
		t.Fatalf("correct code with spaces: %v", err)
	}
	if err := m.Verify(code); err != ErrNotOpen {
		t.Fatalf("code must be single use, got %v", err)
	}

	code, _ = m.Open(time.Minute)
	now = now.Add(2 * time.Minute)
	if err := m.Verify(code); err != ErrExpired {
		t.Fatalf("expired code: got %v", err)
	}
}

func TestTooManyFailuresClosesPairing(t *testing.T) {
	m := NewManager()
	code, _ := m.Open(time.Minute)
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for i := 1; i < MaxFailures; i++ {
		if err := m.Verify(wrong); err != ErrWrongCode {
			t.Fatalf("attempt %d: got %v", i, err)
		}
	}
	if err := m.Verify(wrong); err != ErrTooManyTries {
		t.Fatalf("final attempt: got %v", err)
	}
	if err := m.Verify(code); err != ErrNotOpen {
		t.Fatalf("pairing must close after too many failures, got %v", err)
	}
}

func TestMatchInputs(t *testing.T) {
	local := []LocalMonitor{
		{ID: `MONITOR\DEL1\0001`, Name: "DELL U2720Q", Serial: "ABC"},
		{ID: `MONITOR\DEL1\0002`, Name: "DELL U2720Q", Serial: "XYZ"},
		{ID: `MONITOR\LG\0003`, Name: "LG HDR 4K"},
	}
	remote := []MonitorInput{
		{ID: "2", Name: "DELL U2720Q", Serial: "xyz", Input: 15},
		{ID: "1", Name: "DELL U2720Q", Serial: "abc", Input: 17},
		{ID: "3", Name: "LG HDR 4K", Input: 18},
	}
	got, unmatched := MatchInputs(local, remote)
	want := map[string]int{`MONITOR\DEL1\0001`: 17, `MONITOR\DEL1\0002`: 15, `MONITOR\LG\0003`: 18}
	if unmatched != 0 || len(got) != len(want) {
		t.Fatalf("got %v unmatched=%d", got, unmatched)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: got %d want %d", k, got[k], v)
		}
	}
}

func TestMatchInputsSingleMonitorFallbackAndAmbiguity(t *testing.T) {
	got, unmatched := MatchInputs(
		[]LocalMonitor{{ID: "win", Name: "Generic PnP Monitor"}},
		[]MonitorInput{{ID: "mac", Name: "MSI MP223", Input: 17}},
	)
	if unmatched != 0 || got["win"] != 17 {
		t.Fatalf("single monitor fallback: %v %d", got, unmatched)
	}

	// Two identical monitors without serials cannot be told apart.
	got, unmatched = MatchInputs(
		[]LocalMonitor{{ID: "a", Name: "Same"}, {ID: "b", Name: "Same"}},
		[]MonitorInput{{ID: "1", Name: "Same", Input: 15}, {ID: "2", Name: "Same", Input: 17}},
	)
	if unmatched != 2 || len(got) != 0 {
		t.Fatalf("ambiguous names must not guess: %v %d", got, unmatched)
	}
}

func TestReplayRepeatsTheAnswerForTheSameDeviceAndCode(t *testing.T) {
	now := time.Unix(5000, 0)
	m := NewManager()
	m.now = func() time.Time { return now }
	m.Remember("123 456", "mac", Response{Token: "tok"})
	if r, ok := m.Replay("123456", "mac"); !ok || r.Token != "tok" {
		t.Fatalf("replay with same code and device: %v %v", r, ok)
	}
	if _, ok := m.Replay("123456", "other"); ok {
		t.Fatal("another device must not get the answer")
	}
	if _, ok := m.Replay("654321", "mac"); ok {
		t.Fatal("a different code must not replay")
	}
	now = now.Add(ReplayWindow + time.Second)
	if _, ok := m.Replay("123456", "mac"); ok {
		t.Fatal("replay must expire")
	}
}
