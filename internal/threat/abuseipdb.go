package threat

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

type AbuseOptions struct {
	Key               string
	Timeout, CacheTTL time.Duration
	DailyBudget       int
}

type AbuseResult struct {
	Score  int
	Source Source
}

type abuseEntry struct {
	result  AbuseResult
	expires time.Time
}

type AbuseIPDB struct {
	mu            sync.Mutex
	key, endpoint string
	client        *http.Client
	ttl           time.Duration
	budget, used  int
	day           string
	blockedUntil  time.Time
	cache         map[netip.Addr]abuseEntry
	flights       map[netip.Addr]chan struct{}
}

func NewAbuseIPDB(o AbuseOptions) *AbuseIPDB {
	return &AbuseIPDB{key: o.Key, endpoint: "https://api.abuseipdb.com/api/v2/check",
		client: &http.Client{Timeout: o.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, ttl: o.CacheTTL, budget: o.DailyBudget,
		cache: map[netip.Addr]abuseEntry{}, flights: map[netip.Addr]chan struct{}{}}
}

func unavailable(message string) AbuseResult {
	return AbuseResult{Source: Source{State: "unavailable", Error: message}}
}

func (a *AbuseIPDB) Lookup(ctx context.Context, ip netip.Addr, now time.Time) AbuseResult {
	if a.key == "" {
		return AbuseResult{Source: Source{State: "disabled"}}
	}
	a.mu.Lock()
	if e, ok := a.cache[ip]; ok && now.Before(e.expires) {
		a.mu.Unlock()
		return e.result
	}
	if ch, ok := a.flights[ip]; ok {
		a.mu.Unlock()
		select {
		case <-ctx.Done():
			return unavailable("request canceled")
		case <-ch:
			return a.Lookup(ctx, ip, now)
		}
	}
	day := now.UTC().Format("2006-01-02")
	if day != a.day {
		a.day = day
		a.used = 0
	}
	if now.Before(a.blockedUntil) || a.used >= a.budget {
		a.mu.Unlock()
		return unavailable("quota or provider cooldown")
	}
	// Bound memory and simultaneous distinct outbound lookups.
	if len(a.flights) >= 16 {
		a.mu.Unlock()
		return unavailable("provider busy")
	}
	ch := make(chan struct{})
	a.flights[ip] = ch
	a.used++
	a.mu.Unlock()
	r, cooldown := a.fetch(ctx, ip, now)
	ttl := a.ttl
	if r.Source.State != "ready" {
		ttl = time.Minute
	}
	a.mu.Lock()
	if cooldown.After(a.blockedUntil) {
		a.blockedUntil = cooldown
	}
	if len(a.cache) >= 4096 {
		for k, v := range a.cache {
			if !now.Before(v.expires) {
				delete(a.cache, k)
			}
		}
		if len(a.cache) >= 4096 {
			for k := range a.cache {
				delete(a.cache, k)
				break
			}
		}
	}
	a.cache[ip] = abuseEntry{r, now.Add(ttl)}
	delete(a.flights, ip)
	close(ch)
	a.mu.Unlock()
	return r
}

func (a *AbuseIPDB) fetch(ctx context.Context, ip netip.Addr, now time.Time) (AbuseResult, time.Time) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.endpoint, nil)
	if err != nil {
		return unavailable("invalid provider configuration"), time.Time{}
	}
	q := req.URL.Query()
	q.Set("ipAddress", ip.String())
	q.Set("maxAgeInDays", "30")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Key", a.key)
	req.Header.Set("Accept", "application/json")
	res, err := a.client.Do(req)
	if err != nil {
		return unavailable("provider request failed"), time.Time{}
	}
	defer res.Body.Close()
	cooldown := time.Time{}
	if res.StatusCode == 429 || res.StatusCode == 401 || res.StatusCode == 403 {
		cooldown = now.Add(24 * time.Hour)
		if res.StatusCode == 429 {
			if seconds, e := strconv.Atoi(res.Header.Get("Retry-After")); e == nil && seconds > 0 && seconds <= 86400 {
				cooldown = now.Add(time.Duration(seconds) * time.Second)
			}
		}
	}
	if res.StatusCode != 200 {
		return unavailable("provider HTTP status " + strconv.Itoa(res.StatusCode)), cooldown
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 65537))
	if err != nil || len(b) > 65536 {
		return unavailable("invalid provider response"), cooldown
	}
	var payload struct {
		Data struct {
			IP     string `json:"ipAddress"`
			Public bool   `json:"isPublic"`
			Score  *int   `json:"abuseConfidenceScore"`
		} `json:"data"`
	}
	if json.Unmarshal(b, &payload) != nil {
		return unavailable("invalid provider response"), cooldown
	}
	d := payload.Data
	address, e := netip.ParseAddr(d.IP)
	if e != nil || address.Unmap() != ip || !d.Public || d.Score == nil || *d.Score < 0 || *d.Score > 100 {
		return unavailable("invalid provider response"), cooldown
	}
	if res.Header.Get("X-RateLimit-Remaining") == "0" {
		cooldown = now.Add(24 * time.Hour)
		if epoch, e := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); e == nil && epoch > now.Unix() && epoch <= now.Add(24*time.Hour).Unix() {
			cooldown = time.Unix(epoch, 0)
		}
	}
	checked := now.UTC()
	return AbuseResult{Score: *d.Score, Source: Source{State: "ready", UpdatedAt: &checked,
		Matched: *d.Score > 0, AbuseConfidenceScore: d.Score}}, cooldown
}
