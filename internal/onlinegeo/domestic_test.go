package onlinegeo

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var domesticIP = netip.MustParseAddr("223.104.143.182")

func domesticBody() string {
	return `{"ip":"223.104.143.182","country_code":"CN","country":"China","continent_code":"AS","region":"Jilin","city":"Changchun","latitude":34.7732,"longitude":113.722,"timezone":"Asia/Shanghai"}`
}

func TestDomesticOnlyIPSBAndPersistentCache(t *testing.T) {
	var calls atomic.Int32
	s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, domesticBody())
	}, func(w http.ResponseWriter, r *http.Request) { t.Error("domestic queried ipwho.is") })
	now := time.Now()
	r := s.LookupDomestic(context.Background(), domesticIP, now)
	if r.Location == nil || r.Source != ipsb || r.Location.City != "Changchun" || len(r.Providers) != 1 {
		t.Fatal("domestic priority failed", r)
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	cached := restarted.LookupDomestic(context.Background(), domesticIP, time.Now())
	if !cached.CacheHit || calls.Load() != 1 || cached.Source != ipsb {
		t.Fatal("domestic cache not restored")
	}
}

func TestDomesticRejectsOtherProviderCacheAndIPSBFailure(t *testing.T) {
	for _, source := range []string{ipwho, geojs} {
		t.Run(source, func(t *testing.T) {
			s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }, func(w http.ResponseWriter, r *http.Request) { t.Error("domestic queried foreign backup") })
			now := time.Now()
			s.state.Cache[domesticIP.String()] = cacheEntry{Source: source, UpdatedAt: now, Location: Location{CountryCode: "CN", Country: "China", City: "Beijing"}}
			r := s.LookupDomestic(context.Background(), domesticIP, now)
			if r.Location != nil || r.CacheHit || len(r.Providers) != 1 || r.Providers[0].Provider != ipsb {
				t.Fatal("disallowed domestic source reused", r)
			}
		})
	}
}

func TestDomesticRejectsWrongCountryAndDoesNotUseForeignFlight(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			fmt.Fprint(w, strings.Replace(domesticBody(), `"CN"`, `"US"`, 1))
			return
		}
		fmt.Fprint(w, domesticBody())
	}, nil)
	now := time.Now()
	foreignDone := make(chan Result, 1)
	go func() { foreignDone <- s.Lookup(context.Background(), domesticIP, now) }()
	<-started
	domestic := s.LookupDomestic(context.Background(), domesticIP, now)
	close(release)
	<-foreignDone
	if domestic.Location == nil || domestic.Location.CountryCode != "CN" || calls.Load() != 2 {
		t.Fatal("domestic shared foreign flight")
	}
	// The foreign flight may have cached its US result after the domestic result.
	// Domestic must reject it and retry IP.SB instead of consuming that cache.
	s.endpoints[ipsb] = "http://127.0.0.1:1"
	r := s.LookupDomestic(context.Background(), domesticIP, now)
	if r.Location != nil || r.CacheHit {
		t.Fatal("domestic consumed wrong-country cache")
	}
}

func TestDomesticWrongCountryAndRateLimitNeverUseBackup(t *testing.T) {
	s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Replace(domesticBody(), `"CN"`, `"US"`, 1))
	}, func(w http.ResponseWriter, r *http.Request) { t.Error("domestic queried backup") })
	now := time.Now()
	r := s.LookupDomestic(context.Background(), domesticIP, now)
	if r.Location != nil || len(r.Providers) != 1 || r.Providers[0].State != "country_mismatch" {
		t.Fatal("domestic accepted wrong country", r)
	}
	s.state.Quotas[ipsb] = quota{Attempts: []time.Time{now, now, now, now}}
	r = s.LookupDomestic(context.Background(), domesticIP, now)
	if r.Location != nil || len(r.Providers) != 1 || r.Providers[0].State != "rate_limited" {
		t.Fatal("domestic rate limit bypassed", r)
	}
}
