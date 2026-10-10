package onlinegeo

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMaintainPurgesExpiredCacheWithoutNewRequests(t *testing.T) {
	s, err := New(Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	s.state.Cache["3.5.140.2"] = cacheEntry{Source: ipwho, UpdatedAt: now.Add(-25 * time.Hour), Location: Location{CountryCode: "KR", Country: "South Korea", City: "Incheon"}}
	s.state.Quotas[ipwho] = quota{Attempts: []time.Time{now.Add(-25 * time.Hour), now.Add(-time.Minute)}}
	if err := s.persist(); err != nil {
		t.Fatal(err)
	}
	if err := s.Maintain(now); err != nil {
		t.Fatal(err)
	}
	var state diskState
	b, err := os.ReadFile(filepath.Join(s.options.CacheDir, stateFile))
	if err != nil || json.Unmarshal(b, &state) != nil {
		t.Fatal("could not read maintenance state", err)
	}
	if len(state.Cache) != 0 || len(state.Quotas[ipwho].Attempts) != 1 {
		t.Fatal("expired cache or quota not purged")
	}
	if err := s.Maintain(now); err != nil {
		t.Fatal(err)
	}
}
