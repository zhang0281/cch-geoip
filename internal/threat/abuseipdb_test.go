package threat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAbuseCacheBudgetAndConcurrent(t *testing.T) {
	var count atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		if r.Header.Get("Key") != "fixture-key" || r.URL.Query().Get("maxAgeInDays") != "30" {
			t.Error("request contract mismatch")
		}
		fmt.Fprintf(w, `{"data":{"ipAddress":%q,"isPublic":true,"abuseConfidenceScore":85}}`, r.URL.Query().Get("ipAddress"))
	}))
	defer srv.Close()
	a := NewAbuseIPDB(AbuseOptions{Key: "fixture-key", Timeout: time.Second, CacheTTL: time.Hour, DailyBudget: 1})
	a.endpoint = srv.URL
	ip := netip.MustParseAddr("8.8.8.8")
	now := time.Now()
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := a.Lookup(context.Background(), ip, now)
			if r.Score != 85 || r.Source.State != "ready" {
				t.Error(r)
			}
		}()
	}
	wg.Wait()
	if count.Load() != 1 {
		t.Fatal("duplicate upstream calls", count.Load())
	}
	if a.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"), now).Source.State != "unavailable" {
		t.Fatal("budget bypass")
	}
	if a.Lookup(context.Background(), ip, now.Add(25*time.Hour)).Score != 85 || count.Load() != 2 {
		t.Fatal("daily reset failed")
	}
}

func TestAbuseFailurePaths(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"quota", 429, "{}"}, {"auth", 401, "{}"}, {"server", 503, "{}"},
		{"invalid_json", 200, "bad"}, {"missing_score", 200, `{"data":{"ipAddress":"8.8.8.8","isPublic":true}}`},
		{"wrong_ip", 200, `{"data":{"ipAddress":"1.1.1.1","isPublic":true,"abuseConfidenceScore":80}}`},
		{"bad_score", 200, `{"data":{"ipAddress":"8.8.8.8","isPublic":true,"abuseConfidenceScore":101}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var count atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.Header().Set("Retry-After", "120")
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer srv.Close()
			a := NewAbuseIPDB(AbuseOptions{Key: "fixture-key", Timeout: time.Second, CacheTTL: time.Hour, DailyBudget: 900})
			a.endpoint = srv.URL
			now := time.Now()
			r := a.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), now)
			if r.Source.State != "unavailable" || r.Score != 0 {
				t.Fatal(r)
			}
			a.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), now)
			if count.Load() != 1 {
				t.Fatal("failure not cached")
			}
			if tc.status == 429 || tc.status == 401 {
				a.Lookup(context.Background(), netip.MustParseAddr("1.1.1.1"), now)
				if count.Load() != 1 {
					t.Fatal("cooldown bypassed")
				}
			}
		})
	}
}

func TestAbuseTimeoutAndDisabled(t *testing.T) {
	a := NewAbuseIPDB(AbuseOptions{})
	if a.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), time.Now()).Source.State != "disabled" {
		t.Fatal("disabled")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer srv.Close()
	a = NewAbuseIPDB(AbuseOptions{Key: "fixture-key", Timeout: 20 * time.Millisecond, DailyBudget: 900})
	a.endpoint = srv.URL
	start := time.Now()
	r := a.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), start)
	if r.Source.State != "unavailable" || time.Since(start) > time.Second {
		t.Fatal("timeout not bounded", r)
	}
}

func TestServiceScoreSemantics(t *testing.T) {
	f := NewFeeds(true, "", time.Hour)
	if err := f.feeds[2].parse([]byte("8.8.8.8\n")); err != nil {
		t.Fatal(err)
	}
	f.feeds[2].updated = time.Now()
	s := &Service{Feeds: f}
	r := s.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), time.Now())
	if !r.TorExit || r.IsThreat || r.RiskLevel != "none" {
		t.Fatal(r)
	}
	for _, score := range []float64{0, .1, .5, .8, 1} {
		if riskLevel(score) == "" {
			t.Fatal(score)
		}
	}
}
