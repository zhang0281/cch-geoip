// Package onlinegeo provides bounded, free-tier online geolocation with durable
// caching and quota accounting. An absent result leaves offline fallback to callers.
package onlinegeo

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	ipapi      = "ipapi.is"
	ipwho      = "ipwho.is"
	maxEntries = 4096
	maxFlights = 16
	maxWaiters = 64
)

type Options struct {
	IPAPIKey, CacheDir                 string
	CacheTTL, Timeout                  time.Duration
	IPAPIDailyBudget, IPWhoDailyBudget int
}

type Location struct {
	CountryCode, Country, ContinentCode, Continent       string
	Region, RegionCode, City, Postal, Timezone, Accuracy string
	Latitude, Longitude                                  *float64
}

type ProviderStatus struct {
	Provider string `json:"provider"`
	State    string `json:"state"`
}

type Result struct {
	Location  *Location
	Source    string
	CacheHit  bool
	UpdatedAt time.Time
	Providers []ProviderStatus
}

type cacheEntry struct {
	Location  Location  `json:"location"`
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updated_at"`
}

type quota struct {
	Attempts     []time.Time `json:"attempts"`
	BlockedUntil time.Time   `json:"blocked_until"`
}

type diskState struct {
	Version int                   `json:"version"`
	Cache   map[string]cacheEntry `json:"cache"`
	Quotas  map[string]quota      `json:"quotas"`
}

type flight struct {
	done    chan struct{}
	result  Result
	waiters int
}

type Service struct {
	mu           sync.Mutex
	options      Options
	state        diskState
	flights      map[netip.Addr]*flight
	client       *http.Client
	endpoints    map[string]string
	storageFault bool
}

func New(o Options) (*Service, error) {
	if o.CacheTTL == 0 {
		o.CacheTTL = 24 * time.Hour
	}
	if o.Timeout == 0 {
		o.Timeout = 2 * time.Second
	}
	if o.IPAPIDailyBudget == 0 {
		o.IPAPIDailyBudget = 900
	}
	if o.IPWhoDailyBudget == 0 {
		o.IPWhoDailyBudget = 900
	}
	if o.CacheDir == "" || o.CacheTTL < time.Hour || o.CacheTTL > 7*24*time.Hour ||
		o.Timeout <= 0 || o.Timeout > 2*time.Second || o.IPAPIDailyBudget < 1 ||
		o.IPAPIDailyBudget > 1000 || o.IPWhoDailyBudget < 1 || o.IPWhoDailyBudget > 1000 {
		return nil, errors.New("invalid online geolocation configuration")
	}
	s := &Service{options: o, flights: make(map[netip.Addr]*flight),
		state:     diskState{Version: 1, Cache: make(map[string]cacheEntry), Quotas: map[string]quota{ipapi: {}, ipwho: {}}},
		client:    &http.Client{Timeout: o.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		endpoints: map[string]string{ipapi: "https://api.ipapi.is", ipwho: "https://ipwho.is"}}
	if err := s.load(time.Now()); err != nil {
		return nil, err
	}
	if err := s.persist(); err != nil {
		return nil, errors.New("online geolocation storage unavailable")
	}
	return s, nil
}

func (s *Service) Lookup(ctx context.Context, ip netip.Addr, now time.Time) Result {
	ip = ip.Unmap()
	if !publicAddress(ip) {
		return Result{Providers: []ProviderStatus{{State: "invalid_ip"}}}
	}
	s.mu.Lock()
	if e, ok := s.state.Cache[ip.String()]; ok && !now.Before(e.UpdatedAt) && now.Before(e.UpdatedAt.Add(s.options.CacheTTL)) {
		r := Result{Location: &e.Location, Source: e.Source, CacheHit: true, UpdatedAt: e.UpdatedAt,
			Providers: []ProviderStatus{{Provider: e.Source, State: "ready"}}}
		s.mu.Unlock()
		return cloneResult(r)
	}
	if f, ok := s.flights[ip]; ok {
		if f.waiters >= maxWaiters {
			s.mu.Unlock()
			return Result{Providers: []ProviderStatus{{State: "busy"}}}
		}
		f.waiters++
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			s.mu.Lock()
			f.waiters--
			s.mu.Unlock()
			return Result{Providers: []ProviderStatus{{State: "canceled"}}}
		case <-f.done:
			s.mu.Lock()
			f.waiters--
			r := cloneResult(f.result)
			s.mu.Unlock()
			return r
		}
	}
	if len(s.flights) >= maxFlights {
		s.mu.Unlock()
		return Result{Providers: []ProviderStatus{{State: "busy"}}}
	}
	f := &flight{done: make(chan struct{})}
	s.flights[ip] = f
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, s.options.Timeout)
	defer cancel()
	r := s.lookup(ctx, ip, now)
	s.mu.Lock()
	f.result = cloneResult(r)
	delete(s.flights, ip)
	close(f.done)
	s.mu.Unlock()
	return r
}

func (s *Service) lookup(ctx context.Context, ip netip.Addr, now time.Time) Result {
	r := Result{}
	for _, provider := range []string{ipapi, ipwho} {
		if provider == ipapi && s.options.IPAPIKey == "" {
			r.Providers = append(r.Providers, ProviderStatus{provider, "disabled"})
			continue
		}
		if ctx.Err() != nil {
			r.Providers = append(r.Providers, ProviderStatus{provider, "canceled"})
			break
		}
		state := s.reserve(provider, now)
		if state != "ready" {
			r.Providers = append(r.Providers, ProviderStatus{provider, state})
			continue
		}
		providerCtx, cancel := context.WithTimeout(ctx, s.options.Timeout/2)
		location, state, cooldown := s.fetch(providerCtx, provider, ip, now)
		cancel()
		r.Providers = append(r.Providers, ProviderStatus{provider, state})
		s.mu.Lock()
		q := s.state.Quotas[provider]
		if cooldown.After(q.BlockedUntil) {
			q.BlockedUntil = cooldown
			s.state.Quotas[provider] = q
		}
		if location != nil {
			s.prune(now)
			if len(s.state.Cache) >= maxEntries {
				s.evictOldest()
			}
			s.state.Cache[ip.String()] = cacheEntry{*location, provider, now.UTC()}
			r.Location = location
			r.Source = provider
			r.UpdatedAt = now.UTC()
		}
		if err := s.persist(); err != nil {
			s.storageFault = true
		}
		s.mu.Unlock()
		if location != nil {
			return cloneResult(r)
		}
	}
	return r
}

// Reserve durably before any network I/O: crashes and failed requests still cost quota.
func (s *Service) reserve(provider string, now time.Time) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storageFault {
		return "storage_unavailable"
	}
	s.prune(now)
	q := s.state.Quotas[provider]
	if now.Before(q.BlockedUntil) {
		return "cooldown"
	}
	budget := s.options.IPAPIDailyBudget
	if provider == ipwho {
		budget = s.options.IPWhoDailyBudget
	}
	if len(q.Attempts) >= budget {
		return "quota_exhausted"
	}
	q.Attempts = append(q.Attempts, now.UTC())
	s.state.Quotas[provider] = q
	if err := s.persist(); err != nil {
		s.storageFault = true
		return "storage_unavailable"
	}
	return "ready"
}

func (s *Service) prune(now time.Time) {
	for provider, q := range s.state.Quotas {
		kept := q.Attempts[:0]
		for _, timestamp := range q.Attempts {
			if timestamp.After(now.Add(-24 * time.Hour)) {
				kept = append(kept, timestamp)
			}
		}
		q.Attempts = kept
		s.state.Quotas[provider] = q
	}
	for ip, e := range s.state.Cache {
		if !now.Before(e.UpdatedAt.Add(s.options.CacheTTL)) {
			delete(s.state.Cache, ip)
		}
	}
}

func (s *Service) evictOldest() {
	var key string
	var oldest time.Time
	for ip, e := range s.state.Cache {
		if key == "" || e.UpdatedAt.Before(oldest) {
			key = ip
			oldest = e.UpdatedAt
		}
	}
	delete(s.state.Cache, key)
}

// Maintain removes expired cached data even when no new IPs are queried.
func (s *Service) Maintain(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	before := len(s.state.Cache)
	for _, q := range s.state.Quotas {
		before += len(q.Attempts)
	}
	s.prune(now)
	after := len(s.state.Cache)
	for _, q := range s.state.Quotas {
		after += len(q.Attempts)
	}
	if before == after {
		return nil
	}
	if err := s.persist(); err != nil {
		s.storageFault = true
		return errors.New("online geolocation storage unavailable")
	}
	return nil
}

func cloneResult(r Result) Result {
	r.Providers = append([]ProviderStatus(nil), r.Providers...)
	if r.Location != nil {
		copy := *r.Location
		if copy.Latitude != nil {
			v := *copy.Latitude
			copy.Latitude = &v
		}
		if copy.Longitude != nil {
			v := *copy.Longitude
			copy.Longitude = &v
		}
		r.Location = &copy
	}
	return r
}
