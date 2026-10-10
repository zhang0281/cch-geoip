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
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var sampleIP = netip.MustParseAddr("9.9.9.9")

func ipapiBody(ip string) string {
	return fmt.Sprintf(`{"ip":%q,"location":{"country_code":"US","country":"United States","continent":"North America","state":"California","city":"Los Angeles","latitude":34.05,"longitude":-118.24,"timezone":"America/Los_Angeles","zip":"90001","accuracy":"high"}}`, ip)
}

func ipwhoBody(ip string) string {
	return fmt.Sprintf(`{"ip":%q,"success":true,"country_code":"CA","country":"Canada","continent_code":"NA","continent":"North America","region":"Ontario","city":"Toronto","latitude":43.65,"longitude":-79.38,"timezone":{"id":"America/Toronto"},"postal":"M5H"}`, ip)
}

func testService(t *testing.T, o Options, apiHandler, whoHandler http.HandlerFunc) *Service {
	t.Helper()
	if o.CacheDir == "" {
		o.CacheDir = t.TempDir()
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	if apiHandler != nil {
		server := httptest.NewServer(apiHandler)
		t.Cleanup(server.Close)
		s.endpoints[ipapi] = server.URL
	}
	if whoHandler != nil {
		server := httptest.NewServer(whoHandler)
		t.Cleanup(server.Close)
		s.endpoints[ipwho] = server.URL
	}
	return s
}

func TestOrderingCachingKeyProtectionAndRestart(t *testing.T) {
	const key = "secret-do-not-leak"
	var first, second atomic.Int32
	s := testService(t, Options{IPAPIKey: key}, func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		if r.Method != http.MethodPost || r.URL.RawQuery != "" || strings.Contains(r.URL.String(), key) {
			t.Error("credential must be POST body only")
		}
		var request struct{ Q, Key string }
		if json.NewDecoder(r.Body).Decode(&request) != nil || request.Q != sampleIP.String() || request.Key != key {
			t.Error("invalid provider request")
		}
		fmt.Fprint(w, ipapiBody(sampleIP.String()))
	}, func(w http.ResponseWriter, r *http.Request) {
		second.Add(1)
		fmt.Fprint(w, ipwhoBody(sampleIP.String()))
	})
	now := time.Now()
	r := s.Lookup(context.Background(), sampleIP, now)
	if r.Location == nil || r.Source != ipapi || r.CacheHit || r.Location.City != "Los Angeles" || r.Location.Accuracy != "high" {
		t.Fatalf("unexpected result: %+v", r)
	}
	if first.Load() != 1 || second.Load() != 0 {
		t.Fatal("wrong provider order")
	}
	r.Location.City = "changed"
	*r.Location.Latitude = 12
	cached := s.Lookup(context.Background(), sampleIP, now.Add(time.Second))
	if !cached.CacheHit || cached.Location.City != "Los Angeles" || *cached.Location.Latitude != 34.05 {
		t.Fatal("cache must be immutable to callers")
	}
	b, err := os.ReadFile(filepath.Join(s.options.CacheDir, stateFile))
	if err != nil || strings.Contains(string(b), key) {
		t.Fatal("state leaked credential")
	}
	info, err := os.Stat(filepath.Join(s.options.CacheDir, stateFile))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("state not private")
	}
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	if !restarted.Lookup(context.Background(), sampleIP, time.Now()).CacheHit {
		t.Fatal("cache lost on restart")
	}
	if len(restarted.state.Quotas[ipapi].Attempts) != 1 {
		t.Fatal("quota lost on restart")
	}
}

func TestMissingKeyAndBackup(t *testing.T) {
	var first, second atomic.Int32
	for _, key := range []string{"", "test"} {
		t.Run(key, func(t *testing.T) {
			s := testService(t, Options{IPAPIKey: key}, func(w http.ResponseWriter, r *http.Request) { first.Add(1); http.Error(w, "no", 503) }, func(w http.ResponseWriter, r *http.Request) {
				second.Add(1)
				fmt.Fprint(w, ipwhoBody(sampleIP.String()))
			})
			r := s.Lookup(context.Background(), sampleIP, time.Now())
			if r.Location == nil || r.Source != ipwho || r.Location.Timezone != "America/Toronto" || r.Location.Postal != "M5H" {
				t.Fatal("backup failed")
			}
			if key == "" && r.Providers[0].State != "disabled" {
				t.Fatal("missing key should skip ipapi")
			}
		})
	}
	if first.Load() != 1 || second.Load() != 2 {
		t.Fatal("missing key sent request")
	}
}

func TestQuotaReservedBeforeNetworkAndSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	var calls atomic.Int32
	s := testService(t, Options{CacheDir: dir, IPWhoDailyBudget: 1}, nil, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		b, err := os.ReadFile(filepath.Join(dir, stateFile))
		var state diskState
		if err != nil || json.Unmarshal(b, &state) != nil || len(state.Quotas[ipwho].Attempts) != 1 {
			t.Error("request sent before durable quota reservation")
		}
		http.Error(w, "failure", 502)
	})
	now := time.Now()
	s.Lookup(context.Background(), sampleIP, now)
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	restarted.endpoints[ipwho] = s.endpoints[ipwho]
	r := restarted.Lookup(context.Background(), netip.MustParseAddr("8.8.8.8"), now.Add(2*time.Minute))
	if calls.Load() != 1 || r.Location != nil || r.Providers[1].State != "quota_exhausted" {
		t.Fatalf("quota reset: %+v", r)
	}
	r = restarted.Lookup(context.Background(), sampleIP, now.Add(24*time.Hour+time.Second))
	if calls.Load() != 2 {
		t.Fatal("rolling quota did not expire")
	}
}

func Test429CooldownPersistsAndExpires(t *testing.T) {
	var calls atomic.Int32
	s := testService(t, Options{}, nil, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
			return
		}
		fmt.Fprint(w, ipwhoBody(sampleIP.String()))
	})
	now := time.Now()
	s.Lookup(context.Background(), sampleIP, now)
	restarted, err := New(s.options)
	if err != nil {
		t.Fatal(err)
	}
	restarted.endpoints[ipwho] = s.endpoints[ipwho]
	r := restarted.Lookup(context.Background(), sampleIP, now.Add(time.Minute))
	if calls.Load() != 1 || r.Providers[1].State != "cooldown" {
		t.Fatal("cooldown not preserved")
	}
	r = restarted.Lookup(context.Background(), sampleIP, now.Add(121*time.Second))
	if calls.Load() != 2 || r.Location == nil {
		t.Fatal("cooldown did not expire")
	}
}

func TestTimeoutLeavesBackupBudget(t *testing.T) {
	s := testService(t, Options{IPAPIKey: "test", Timeout: 200 * time.Millisecond}, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
		}
	}, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, ipwhoBody(sampleIP.String())) })
	start := time.Now()
	r := s.Lookup(context.Background(), sampleIP, time.Now())
	if r.Location == nil || r.Source != ipwho || time.Since(start) > 250*time.Millisecond {
		t.Fatalf("timeout prevented backup: %+v", r)
	}
}

func TestCoalescingIncludesFailedResult(t *testing.T) {
	for _, success := range []bool{true, false} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			s := testService(t, Options{}, nil, func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				if success {
					fmt.Fprint(w, ipwhoBody(sampleIP.String()))
				} else {
					w.WriteHeader(503)
				}
			})
			var wg sync.WaitGroup
			results := make(chan Result, 12)
			for i := 0; i < 12; i++ {
				wg.Add(1)
				go func() { defer wg.Done(); results <- s.Lookup(context.Background(), sampleIP, time.Now()) }()
			}
			<-started
			deadline := time.Now().Add(time.Second)
			for {
				s.mu.Lock()
				n := s.flights[sampleIP].waiters
				s.mu.Unlock()
				if n == 11 {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("waiters did not join")
				}
				time.Sleep(time.Millisecond)
			}
			close(release)
			wg.Wait()
			close(results)
			if calls.Load() != 1 {
				t.Fatal("requests not coalesced")
			}
			for r := range results {
				if (r.Location != nil) != success {
					t.Fatal("coalesced result mismatch")
				}
			}
		})
	}
}

func TestExpiredCacheRefreshes(t *testing.T) {
	var calls atomic.Int32
	s := testService(t, Options{CacheTTL: time.Hour}, nil, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, ipwhoBody(sampleIP.String()))
	})
	now := time.Now()
	s.Lookup(context.Background(), sampleIP, now)
	r := s.Lookup(context.Background(), sampleIP, now.Add(time.Hour))
	if calls.Load() != 2 || r.CacheHit || r.Location == nil {
		t.Fatal("expired cache not refreshed")
	}
}

func TestStorageFailureNeverSends(t *testing.T) {
	var calls atomic.Int32
	s := testService(t, Options{}, nil, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); t.Error("request after storage failure") })
	// A directory at the rename destination makes even root unable to persist.
	path := filepath.Join(s.options.CacheDir, stateFile)
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	r := s.Lookup(context.Background(), sampleIP, time.Now())
	if calls.Load() != 0 || r.Location != nil || r.Providers[1].State != "storage_unavailable" {
		t.Fatal("storage failed open")
	}
}

func TestNewRejectsInvalidStateAndOptions(t *testing.T) {
	for _, o := range []Options{{}, {CacheDir: t.TempDir(), Timeout: 3 * time.Second}, {CacheDir: t.TempDir(), CacheTTL: 8 * 24 * time.Hour}, {CacheDir: t.TempDir(), IPWhoDailyBudget: 1001}} {
		if _, err := New(o); err == nil {
			t.Fatal("accepted invalid options")
		}
	}
	for _, body := range []string{"{}", "not json", `{"version":1,"cache":{},"quotas":{}}`} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, stateFile), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := New(Options{CacheDir: dir}); err == nil {
			t.Fatal("invalid state resets quota")
		}
	}
	s, err := New(Options{CacheDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	s.state.Quotas[ipwho] = quota{Attempts: []time.Time{time.Now().Add(time.Hour)}}
	if s.persist() != nil {
		t.Fatal("persist test state")
	}
	if _, err := New(s.options); err == nil {
		t.Fatal("accepted future quota")
	}
}

func TestRedirectRejectedAndOversizedResponse(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirected.Add(1)
		fmt.Fprint(w, ipapiBody(sampleIP.String()))
	}))
	defer target.Close()
	s := testService(t, Options{IPAPIKey: "secret"}, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", maxResponseBytes+1)) })
	r := s.Lookup(context.Background(), sampleIP, time.Now())
	if redirected.Load() != 0 || r.Location != nil || r.Providers[1].State != "invalid_response" {
		t.Fatalf("unsafe HTTP handling: %+v", r)
	}
}

func TestResponseValidation(t *testing.T) {
	valid := ipapiBody(sampleIP.String())
	for _, body := range []string{
		ipapiBody("8.8.8.8"),
		strings.Replace(valid, `"US"`, `"XX"`, 1),
		strings.Replace(valid, `34.05`, `91`, 1),
		strings.Replace(valid, `-118.24`, `181`, 1),
		strings.Replace(valid, `-118.24`, `null`, 1),
		strings.Replace(valid, `America/Los_Angeles`, `No/Such_Zone`, 1),
		strings.Replace(valid, `"California"`, `""`, 1),
		`{"ip":"9.9.9.9","is_bogon":true,"location":{}}`,
		strings.Replace(valid, `"ip":`, `"error":"secret-provider-message","ip":`, 1),
		valid + `{}`,
	} {
		if strings.Contains(body, `"state":"","city":"Los Angeles"`) {
			body = strings.Replace(body, `"Los Angeles"`, `""`, 1)
		}
		if _, ok := parseLocation(ipapi, []byte(body), sampleIP); ok {
			t.Errorf("accepted invalid response: %s", body)
		}
	}
	if _, ok := parseLocation(ipwho, []byte(strings.Replace(ipwhoBody(sampleIP.String()), `true`, `false`, 1)), sampleIP); ok {
		t.Fatal("accepted failed lookup")
	}
	missingCoordinates := strings.Replace(strings.Replace(valid, `34.05`, `null`, 1), `-118.24`, `null`, 1)
	l, ok := parseLocation(ipapi, []byte(missingCoordinates), sampleIP)
	if !ok || l.Latitude != nil || l.Longitude != nil {
		t.Fatal("unknown coordinates must remain unknown")
	}
	for _, ip := range []string{"127.0.0.1", "192.0.2.1", "100.64.0.1", "2001:db8::1", "10.1.2.3"} {
		if publicAddress(netip.MustParseAddr(ip)) {
			t.Fatal("reserved address accepted")
		}
	}
}

func TestIPAPIContinentCodeFromOfficialShape(t *testing.T) {
	body := strings.Replace(ipapiBody(sampleIP.String()), `"continent":"North America"`, `"continent":"NA"`, 1)
	l, ok := parseLocation(ipapi, []byte(body), sampleIP)
	if !ok || l.ContinentCode != "NA" || l.Continent != "North America" {
		t.Fatalf("continent code not normalized: %+v", l)
	}
	for code, name := range continentNames {
		body = strings.Replace(ipapiBody(sampleIP.String()), `"continent":"North America"`, `"continent":"`+code+`"`, 1)
		l, ok = parseLocation(ipapi, []byte(body), sampleIP)
		if !ok || l.ContinentCode != code || l.Continent != name {
			t.Fatal("continent mapping incomplete")
		}
	}
	b, err := json.Marshal(ProviderStatus{Provider: ipapi, State: "ready"})
	if err != nil || string(b) != `{"provider":"ipapi.is","state":"ready"}` {
		t.Fatal("provider metadata keys not lowercase")
	}
}

func TestRetryAfterBounds(t *testing.T) {
	now := time.Now()
	for _, value := range []string{"0", "-1", "86401", "99999999999999999999", now.Add(48 * time.Hour).Format(http.TimeFormat)} {
		d := retryAfter(value, now).Sub(now)
		if d < time.Minute || d > 24*time.Hour {
			t.Fatal("unbounded retry-after")
		}
	}
}

func TestIPAPIIncompleteRecordTriesBackup(t *testing.T) {
	for _, bad := range []string{
		strings.Replace(strings.Replace(ipapiBody(sampleIP.String()), `"California"`, `""`, 1), `"Los Angeles"`, `""`, 1),
		ipapiBody("8.8.8.8"),
		`{"error":"token secret upstream details"}`,
	} {
		s := testService(t, Options{IPAPIKey: "test"}, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, bad)
		}, func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, ipwhoBody(sampleIP.String()))
		})
		r := s.Lookup(context.Background(), sampleIP, time.Now())
		if r.Location == nil || r.Source != ipwho || r.Location.City != "Toronto" || r.Providers[0].State != "invalid_response" {
			t.Fatal("invalid or incomplete first provider blocked backup")
		}
		b, err := json.Marshal(r)
		if err != nil || strings.Contains(string(b), "secret") || strings.Contains(string(b), "upstream") {
			t.Fatal("unsanitized outcome")
		}
	}
}

func TestCanceledCallerAndBoundedFlights(t *testing.T) {
	s := testService(t, Options{}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if r := s.Lookup(ctx, sampleIP, time.Now()); r.Location != nil || r.Providers[1].State != "canceled" {
		t.Fatal("canceled lookup sent request")
	}
	for i := 0; i < maxFlights; i++ {
		s.flights[netip.AddrFrom4([4]byte{11, 0, 0, byte(i)})] = &flight{done: make(chan struct{})}
	}
	if r := s.Lookup(context.Background(), sampleIP, time.Now()); r.Location != nil || r.Providers[0].State != "busy" {
		t.Fatal("outbound bulkhead not bounded")
	}
	s.flights = map[netip.Addr]*flight{sampleIP: {done: make(chan struct{}), waiters: maxWaiters}}
	if r := s.Lookup(context.Background(), sampleIP, time.Now()); r.Providers[0].State != "busy" {
		t.Fatal("waiters not bounded")
	}
	if r := s.Lookup(context.Background(), netip.MustParseAddr("192.168.0.1"), time.Now()); r.Providers[0].State != "invalid_ip" {
		t.Fatal("private lookup sent request")
	}
}

func TestCacheEvictionBoundedAndOldestFirst(t *testing.T) {
	s := testService(t, Options{}, nil, nil)
	now := time.Now()
	location, ok := parseLocation(ipwho, []byte(ipwhoBody(sampleIP.String())), sampleIP)
	if !ok {
		t.Fatal("fixture invalid")
	}
	for i := 0; i < maxEntries; i++ {
		address := netip.AddrFrom4([4]byte{11, byte(i / 256), byte(i % 256), 1})
		s.state.Cache[address.String()] = cacheEntry{Location: *location, Source: ipwho, UpdatedAt: now.Add(time.Duration(i) * time.Nanosecond)}
	}
	s.evictOldest()
	if len(s.state.Cache) != maxEntries-1 {
		t.Fatal("cache not bounded")
	}
	if _, present := s.state.Cache["11.0.0.1"]; present {
		t.Fatal("oldest cache not evicted")
	}
	if _, err := New(Options{CacheDir: "/proc/cch-onlinegeo-test"}); err == nil {
		t.Fatal("unwritable startup storage accepted")
	}
}
