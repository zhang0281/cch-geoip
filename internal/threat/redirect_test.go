package threat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderDoesNotForwardKeyOnRedirect(t *testing.T) {
	var leaked atomic.Bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Store(true) }))
	defer target.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer origin.Close()
	a := NewAbuseIPDB(AbuseOptions{Key: "synthetic-key", Timeout: time.Second, CacheTTL: time.Hour, DailyBudget: 900})
	a.endpoint = origin.URL
	r := a.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), time.Now())
	if r.Source.State != "unavailable" || leaked.Load() {
		t.Fatal("redirect followed", r)
	}
}
