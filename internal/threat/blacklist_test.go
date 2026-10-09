package threat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func blacklistJSON(now time.Time, score int) string {
	return fmt.Sprintf(`{"meta":{"generatedAt":%q},"data":[{"ipAddress":"8.8.8.8","abuseConfidenceScore":%d,"lastReportedAt":%q}]}`,
		now.UTC().Format(time.RFC3339), score, now.Add(-time.Hour).UTC().Format(time.RFC3339))
}

func TestBlacklistLocalPriorityAndRestore(t *testing.T) {
	var downloads, checks atomic.Int32
	now := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downloads.Add(1)
		if r.Header.Get("Key") != "fixture-key" || r.Header.Get("Accept") != "application/json" ||
			r.URL.Query().Get("limit") != "10000" || r.URL.Query().Has("confidenceMinimum") || r.URL.Query().Has("ipAddress") {
			t.Error("blacklist contract mismatch")
		}
		fmt.Fprint(w, blacklistJSON(now, 95))
	}))
	defer srv.Close()
	check := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checks.Add(1)
		fmt.Fprintf(w, `{"data":{"ipAddress":%q,"isPublic":true,"abuseConfidenceScore":10}}`, r.URL.Query().Get("ipAddress"))
	}))
	defer check.Close()
	dir := t.TempDir()
	b := NewBlacklist("fixture-key", dir, 72*time.Hour)
	b.endpoint = srv.URL
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); b.Refresh(context.Background()) }()
	}
	wg.Wait()
	a := NewAbuseIPDB(AbuseOptions{Key: "fixture-key", Timeout: time.Second, CacheTTL: time.Hour, DailyBudget: 10})
	a.endpoint = check.URL
	s := &Service{Blacklist: b, Abuse: a}
	ip := netip.MustParseAddr("8.8.8.8")
	r := s.Lookup(context.Background(), ip, now)
	if !r.IsThreat || !r.IsAbuser || r.Score != .95 || r.RiskLevel != "critical" || len(r.Blocklists) != 1 ||
		r.Blocklists[0].ListedAt != nil || r.Sources["abuseipdb_check"].State != "skipped" ||
		r.Sources["abuseipdb_blacklist"].LastReportedAt == nil || checks.Load() != 0 || downloads.Load() != 1 {
		t.Fatal(r, checks.Load(), downloads.Load())
	}
	miss := s.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"), now)
	if miss.Score != .1 || miss.IsThreat || checks.Load() != 1 {
		t.Fatal(miss)
	}
	s.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"), now)
	if checks.Load() != 1 {
		t.Fatal("CHECK cache bypassed")
	}
	stale := s.Lookup(context.Background(), ip, now.Add(73*time.Hour))
	if stale.Sources["abuseipdb_blacklist"].State != "stale" || stale.Score != .1 || checks.Load() != 2 {
		t.Fatal("stale list suppressed CHECK", stale)
	}
	restored := NewBlacklist("fixture-key", dir, 72*time.Hour)
	restored.endpoint = srv.URL
	restored.Restore()
	restored.Refresh(context.Background())
	if downloads.Load() != 1 || restored.Lookup(ip, now).Score != 95 {
		t.Fatal("restore or cadence failed")
	}
	info, err := os.Stat(filepath.Join(dir, "abuseipdb_blacklist.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache permissions", err)
	}
}

func TestBlacklistInvalidData(t *testing.T) {
	now := time.Now()
	valid := blacklistJSON(now, 95)
	for _, body := range []string{
		"bad", `{}`, `{"meta":{"generatedAt":"2026-01-01T00:00:00Z"},"data":null}`,
		strings.Replace(valid, "8.8.8.8", "hostname", 1), strings.Replace(valid, "8.8.8.8", "127.0.0.1", 1),
		strings.Replace(valid, "8.8.8.8", "10.0.0.1", 1), blacklistJSON(now, 101), blacklistJSON(now, 24),
		strings.Replace(valid, `"abuseConfidenceScore":95`, `"abuseConfidenceScore":null`, 1),
		blacklistJSON(now.Add(time.Hour), 95), strings.Repeat("x", maxFeedBytes+1),
	} {
		if _, _, err := parseBlacklist([]byte(body), now); err == nil {
			t.Fatal("invalid list accepted")
		}
	}
	empty := fmt.Sprintf(`{"meta":{"generatedAt":%q},"data":[]}`, now.UTC().Format(time.RFC3339))
	if _, entries, err := parseBlacklist([]byte(empty), now); err != nil || len(entries) != 0 {
		t.Fatal("valid empty snapshot rejected", err)
	}
	ipv6 := strings.Replace(valid, "8.8.8.8", "2606:4700:4700::1111", 1)
	if _, entries, err := parseBlacklist([]byte(ipv6), now); err != nil || len(entries) != 1 {
		t.Fatal(err)
	}
}

func TestBlacklistFailedUpdateAndCadence(t *testing.T) {
	for _, status := range []int{401, 403, 429, 503, 200} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			now := time.Now()
			var requests atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.WriteHeader(status)
				fmt.Fprint(w, "bad JSON")
			}))
			defer srv.Close()
			dir := t.TempDir()
			b := NewBlacklist("fixture-key", dir, 72*time.Hour)
			b.endpoint = srv.URL
			data := []byte(blacklistJSON(now.Add(-25*time.Hour), 80))
			p, entries, err := parseBlacklist(data, now)
			if err != nil {
				t.Fatal(err)
			}
			b.snapshot = blacklistSnapshot{Attempt: now.Add(-25 * time.Hour), Updated: now.Add(-25 * time.Hour), Data: data}
			b.published, b.entries = p.Meta.Generated, entries
			b.Refresh(context.Background())
			r := b.Lookup(netip.MustParseAddr("8.8.8.8"), now)
			if r.Score != 80 || r.Source.State != "ready" || r.Source.Error == "" {
				t.Fatal("lost valid snapshot", r)
			}
			restarted := NewBlacklist("fixture-key", dir, 72*time.Hour)
			restarted.endpoint = srv.URL
			restarted.Restore()
			restarted.Refresh(context.Background())
			if requests.Load() != 1 || restarted.Lookup(netip.MustParseAddr("8.8.8.8"), now).Score != 80 {
				t.Fatal("failure retry cadence not persisted")
			}
		})
	}
}

func TestBlacklistRedirectAndDiskFailure(t *testing.T) {
	var leaked, requests atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirect.Close()
	b := NewBlacklist("fixture-key", t.TempDir(), 72*time.Hour)
	b.endpoint = redirect.URL
	b.Refresh(context.Background())
	if leaked.Load() != 0 || b.Lookup(netip.MustParseAddr("8.8.8.8"), time.Now()).Source.State != "unavailable" {
		t.Fatal("redirect followed")
	}
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	b = NewBlacklist("fixture-key", path, 72*time.Hour)
	b.endpoint = redirect.URL
	b.Refresh(context.Background())
	if requests.Load() != 1 {
		t.Fatal("download attempted without persisted cadence")
	}
}

func TestBlacklistDisabledAndLocalOnly(t *testing.T) {
	now := time.Now()
	ip := netip.MustParseAddr("8.8.8.8")
	b := NewBlacklist("", "", time.Hour)
	b.Refresh(context.Background())
	if b.Lookup(ip, now).Source.State != "disabled" {
		t.Fatal("disabled")
	}
	b = NewBlacklist("fixture-key", "", time.Hour)
	p, entries, err := parseBlacklist([]byte(blacklistJSON(now, 50)), now)
	if err != nil {
		t.Fatal(err)
	}
	b.snapshot.Updated, b.published, b.entries = now, p.Meta.Generated, entries
	r := (&Service{Blacklist: b}).Lookup(context.Background(), ip, now)
	if r.Score != .5 || r.IsThreat || r.Sources["abuseipdb_check"].State != "skipped" {
		t.Fatal(r)
	}
}

func TestBlacklistRestoreInvalidAndFirstFailure(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	path := filepath.Join(dir, "abuseipdb_blacklist.json")
	if err := os.WriteFile(path, []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	b := NewBlacklist("fixture-key", dir, time.Hour)
	b.Restore()
	if !b.snapshot.Attempt.IsZero() {
		t.Fatal("invalid cache restored")
	}
	if err := b.save(blacklistSnapshot{Attempt: now, Data: []byte("bad")}); err != nil {
		t.Fatal(err)
	}
	b.Restore()
	if b.snapshot.Attempt.IsZero() || !b.snapshot.Updated.IsZero() {
		t.Fatal("attempt-only cache not restored")
	}
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(429)
	}))
	defer srv.Close()
	b.endpoint = srv.URL
	b.Refresh(context.Background())
	if calls.Load() != 0 {
		t.Fatal("persisted failed attempt ignored")
	}
	b.snapshot.Attempt = now.Add(-25 * time.Hour)
	b.Refresh(context.Background())
	restarted := NewBlacklist("fixture-key", dir, time.Hour)
	restarted.endpoint = srv.URL
	restarted.Restore()
	restarted.Refresh(context.Background())
	if calls.Load() != 1 {
		t.Fatal("first failure not rate limited across restart")
	}
}

func TestBlacklistStaleProviderAndEmptyReplacement(t *testing.T) {
	now := time.Now()
	body := blacklistJSON(now.Add(-96*time.Hour), 95)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
	defer srv.Close()
	b := NewBlacklist("fixture-key", t.TempDir(), 72*time.Hour)
	b.endpoint = srv.URL
	b.Refresh(context.Background())
	if !b.snapshot.Updated.IsZero() || b.err != "provider blacklist is stale" {
		t.Fatal("stale provider accepted")
	}
	b.snapshot.Attempt = now.Add(-25 * time.Hour)
	body = fmt.Sprintf(`{"meta":{"generatedAt":%q},"data":[]}`, now.UTC().Format(time.RFC3339))
	b.Refresh(context.Background())
	if b.snapshot.Updated.IsZero() || len(b.entries) != 0 || b.err != "" {
		t.Fatal("empty update rejected")
	}
	valid := blacklistJSON(now, 95)
	start := strings.Index(valid, "[{") + 1
	end := strings.LastIndex(valid, "]")
	duplicate := valid[:end] + "," + valid[start:end] + valid[end:]
	if _, _, err := parseBlacklist([]byte(duplicate), now); err == nil {
		t.Fatal("duplicate accepted")
	}
}
