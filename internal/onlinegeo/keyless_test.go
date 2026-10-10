package onlinegeo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func geojsBody(ip string) string {
	return fmt.Sprintf(`{"ip":%q,"country_code":"DE","country":"Germany","continent_code":"EU","region":"Hesse","city":"Frankfurt","latitude":"50.11","longitude":"8.68","timezone":"Europe/Berlin","accuracy":20}`, ip)
}

func TestThreeKeylessProvidersAndGeoJSCache(t *testing.T) {
	var sequence []string
	s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) { sequence = append(sequence, ipsb); w.WriteHeader(503) }, func(w http.ResponseWriter, r *http.Request) { sequence = append(sequence, ipwho); w.WriteHeader(503) })
	last := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sequence = append(sequence, geojs)
		if r.URL.Path != "/"+sampleIP.String()+".json" || r.URL.RawQuery != "" || r.Method != http.MethodGet || r.Header.Get("User-Agent") != "cch-geoip/0.5.0" {
			t.Error("invalid GeoJS keyless request")
		}
		fmt.Fprint(w, geojsBody(sampleIP.String()))
	}))
	defer last.Close()
	s.endpoints[geojs] = last.URL
	r := s.Lookup(context.Background(), sampleIP, time.Now())
	if strings.Join(sequence, ",") != "ip.sb,ipwho.is,geojs" || r.Location == nil || r.Source != geojs || r.Location.City != "Frankfurt" || r.Location.Continent != "Europe" || r.Location.AccuracyRadiusKM == nil || *r.Location.AccuracyRadiusKM != 20 {
		t.Fatalf("third provider failed: %+v sequence=%v", r, sequence)
	}
	*r.Location.AccuracyRadiusKM = 2
	cached := s.Lookup(context.Background(), sampleIP, time.Now())
	if !cached.CacheHit || *cached.Location.AccuracyRadiusKM != 20 {
		t.Fatal("first result mutation corrupted memory cache radius")
	}
	*cached.Location.AccuracyRadiusKM = 3
	if again := s.Lookup(context.Background(), sampleIP, time.Now()); *again.Location.AccuracyRadiusKM != 20 {
		t.Fatal("cache-hit result mutation corrupted memory cache radius")
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	r = restarted.Lookup(context.Background(), sampleIP, time.Now())
	if !r.CacheHit || *r.Location.AccuracyRadiusKM != 20 {
		t.Fatal("radius cache corrupted or not restored")
	}
}

func TestGeoJSStringCoordinatesAndRadiusValidation(t *testing.T) {
	valid := geojsBody(sampleIP.String())
	for _, body := range []string{
		strings.Replace(valid, `"50.11"`, `"NaN"`, 1),
		strings.Replace(valid, `"50.11"`, `"Inf"`, 1),
		strings.Replace(valid, `"50.11"`, `"91"`, 1),
		strings.Replace(valid, `"8.68"`, `"181"`, 1),
		strings.Replace(valid, `"8.68"`, `""`, 1),
		strings.Replace(valid, `"50.11"`, `50.11`, 1),
		strings.Replace(valid, `"accuracy":20`, `"accuracy":0`, 1),
		strings.Replace(valid, `"accuracy":20`, `"accuracy":20001`, 1),
		strings.Replace(valid, `"accuracy":20`, `"accuracy":20.5`, 1),
		geojsBody("8.8.8.8"),
	} {
		if _, ok := parseLocation(geojs, []byte(body), sampleIP); ok {
			t.Errorf("accepted invalid GeoJS %s", body)
		}
	}
	unknown := strings.Replace(strings.Replace(strings.Replace(valid, `"50.11"`, `""`, 1), `"8.68"`, `""`, 1), `,"accuracy":20`, "", 1)
	l, ok := parseLocation(geojs, []byte(unknown), sampleIP)
	if !ok || l.Latitude != nil || l.Longitude != nil || l.AccuracyRadiusKM != nil {
		t.Fatal("missing coords/radius invented")
	}
	zero := strings.Replace(strings.Replace(valid, `"50.11"`, `"0"`, 1), `"8.68"`, `"0"`, 1)
	l, ok = parseLocation(geojs, []byte(zero), sampleIP)
	if !ok || l.Latitude != nil || l.AccuracyRadiusKM != nil {
		t.Fatal("zero coordinates not unknown")
	}
	for _, provider := range []string{ipsb, ipwho} {
		body := ipsbBody(sampleIP.String())
		if provider == ipwho {
			body = ipwhoBody(sampleIP.String())
		}
		l, ok = parseLocation(provider, []byte(body), sampleIP)
		if !ok || l.AccuracyRadiusKM != nil {
			t.Fatal("provider without accuracy got invented radius")
		}
	}
}

func TestIPSBRateLimitsPersist(t *testing.T) {
	s := testService(t, Options{}, nil, nil)
	now := time.Now().Add(-2 * time.Second)
	for i := 0; i < 4; i++ {
		if s.reserve(ipsb, now) != "ready" {
			t.Fatal("unexpected rate rejection")
		}
	}
	if s.reserve(ipsb, now) != "rate_limited" {
		t.Fatal("second limit failed")
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.reserve(ipsb, now) != "rate_limited" {
		t.Fatal("restart bypassed second limit")
	}
	if restarted.reserve(ipsb, now.Add(time.Second)) != "ready" {
		t.Fatal("second limit did not expire")
	}
	current := time.Now()
	attempts := make([]time.Time, 90)
	for i := range attempts {
		attempts[i] = current.Add(-30 * time.Second)
	}
	restarted.state.Quotas[ipsb] = quota{Attempts: attempts}
	if err := restarted.persist(); err != nil {
		t.Fatal(err)
	}
	again, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	if again.reserve(ipsb, current) != "rate_limited" {
		t.Fatal("minute limit lost across restart")
	}
	if again.reserve(ipsb, current.Add(30*time.Second)) != "ready" {
		t.Fatal("minute limit did not expire")
	}
}

func TestMigrationPreservesIPWhoStateAndRejectsMalformedLegacy(t *testing.T) {
	now := time.Now()
	location, ok := parseLocation(ipwho, []byte(ipwhoBody(sampleIP.String())), sampleIP)
	if !ok {
		t.Fatal("fixture invalid")
	}
	legacy := diskState{Version: 1, Cache: map[string]cacheEntry{
		sampleIP.String(): {Location: *location, Source: ipwho, UpdatedAt: now.Add(-time.Minute)},
		"8.8.8.8":         {Location: *location, Source: "ipapi.is", UpdatedAt: now.Add(-time.Minute)},
	}, Quotas: map[string]quota{
		"ipapi.is": {Attempts: []time.Time{now.Add(-time.Hour)}},
		ipwho:      {Attempts: []time.Time{now.Add(-25 * time.Hour), now.Add(-time.Minute)}, BlockedUntil: now.Add(time.Hour)},
	}}
	dir := t.TempDir()
	writeState := func(state diskState) {
		b, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, stateFile), b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeState(legacy)
	s, err := New(Options{CacheDir: dir, IPWhoDailyBudget: 1})
	if err != nil {
		t.Fatal(err)
	}
	if s.state.Version != 2 || len(s.state.Cache) != 1 || len(s.state.Quotas) != 3 || len(s.state.Quotas[ipwho].Attempts) != 1 || !s.state.Quotas[ipwho].BlockedUntil.Equal(legacy.Quotas[ipwho].BlockedUntil) {
		t.Fatal("migration lost quota/cache/cooldown")
	}
	if !s.Lookup(context.Background(), sampleIP, time.Now()).CacheHit {
		t.Fatal("old ipwho cache not reused")
	}
	if s.reserve(ipwho, now) != "cooldown" {
		t.Fatal("migration bypassed cooldown")
	}
	if s.reserve(ipwho, now.Add(2*time.Hour)) != "quota_exhausted" {
		t.Fatal("migration bypassed quota")
	}
	if len(s.state.Quotas[ipsb].Attempts) != 0 || len(s.state.Quotas[geojs].Attempts) != 0 {
		t.Fatal("new providers not initialized")
	}
	for _, malform := range []func(*diskState){
		func(d *diskState) { d.Quotas[ipwho] = quota{Attempts: []time.Time{now.Add(time.Hour)}} },
		func(d *diskState) { delete(d.Quotas, ipwho) },
		func(d *diskState) { d.Quotas[geojs] = quota{} },
		func(d *diskState) {
			e := d.Cache[sampleIP.String()]
			e.UpdatedAt = now.Add(time.Hour)
			d.Cache[sampleIP.String()] = e
		},
	} {
		b, _ := json.Marshal(legacy)
		var copy diskState
		if json.Unmarshal(b, &copy) != nil {
			t.Fatal("fixture copy failed")
		}
		malform(&copy)
		writeState(copy)
		if _, err := New(Options{CacheDir: dir}); err == nil {
			t.Fatal("malformed legacy accepted/reset")
		}
	}
}

func TestRateLimitedIPSBTriesBackup(t *testing.T) {
	s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) { t.Error("rate-limited IP.SB was queried") }, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, ipwhoBody(sampleIP.String())) })
	now := time.Now()
	s.state.Quotas[ipsb] = quota{Attempts: []time.Time{now, now, now, now}}
	r := s.Lookup(context.Background(), netip.MustParseAddr("9.9.9.9"), now)
	if r.Location == nil || r.Source != ipwho || r.Providers[0].State != "rate_limited" {
		t.Fatal("rate limit prevented backup")
	}
}

func TestIncompleteAddressDoesNotDisableProviderForOtherIPs(t *testing.T) {
	var calls atomic.Int32
	s := testService(t, Options{}, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/8.8.8.8" {
			fmt.Fprint(w, `{"ip":"8.8.8.8","country_code":"US","country":"United States"}`)
			return
		}
		fmt.Fprint(w, ipsbBody("3.5.140.2"))
	}, nil)
	now := time.Now()
	first := s.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), now)
	if first.Location != nil || first.Providers[0].State != "invalid_response" {
		t.Fatal("incomplete IP should fall back")
	}
	second := s.Lookup(context.Background(), netip.MustParseAddr("3.5.140.2"), now.Add(time.Millisecond))
	if calls.Load() != 2 || second.Location == nil || second.Source != ipsb {
		t.Fatal("incomplete address disabled provider for unrelated IP")
	}
}
