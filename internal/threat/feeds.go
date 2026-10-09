package threat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxFeedBytes = 8 << 20

type feed struct {
	name, url, kind string
	data            []byte
	prefixes        []netip.Prefix
	exits           map[netip.Addr]bool
	updated         time.Time
	published       time.Time
	err             string
}

type Feeds struct {
	mu        sync.RWMutex
	refreshMu sync.Mutex
	enabled   bool
	maxAge    time.Duration
	dir       string
	client    *http.Client
	feeds     []feed
}

func NewFeeds(enabled bool, dir string, maxAge time.Duration) *Feeds {
	return &Feeds{enabled: enabled, dir: dir, maxAge: maxAge,
		client: &http.Client{Timeout: 30 * time.Second},
		feeds: []feed{
			{name: "spamhaus_drop_v4", kind: "drop", url: "https://www.spamhaus.org/drop/drop_v4.json"},
			{name: "spamhaus_drop_v6", kind: "drop", url: "https://www.spamhaus.org/drop/drop_v6.json"},
			{name: "tor_exit", kind: "tor", url: "https://check.torproject.org/torbulkexitlist"},
		}}
}

func (f *Feeds) Restore() {
	if !f.enabled || f.dir == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.feeds {
		x := &f.feeds[i]
		b, err := os.ReadFile(filepath.Join(f.dir, x.name+".json"))
		if err != nil || len(b) > 2*maxFeedBytes {
			continue
		}
		var saved struct {
			Updated time.Time
			Data    []byte
		}
		if json.Unmarshal(b, &saved) != nil || saved.Updated.IsZero() || saved.Updated.After(time.Now().Add(time.Minute)) {
			continue
		}
		candidate := *x
		if candidate.parse(saved.Data) != nil {
			continue
		}
		candidate.updated = saved.Updated
		*x = candidate
	}
}

// Network and disk I/O are outside the query lock; a failed update keeps the old snapshot.
func (f *Feeds) Refresh(ctx context.Context) {
	if !f.enabled {
		return
	}
	f.refreshMu.Lock()
	defer f.refreshMu.Unlock()
	for i := range f.feeds {
		if ctx.Err() != nil {
			return
		}
		f.mu.RLock()
		candidate := f.feeds[i]
		f.mu.RUnlock()
		// Persisted snapshots also enforce the provider's daily download cadence across restarts.
		if !candidate.updated.IsZero() && time.Since(candidate.updated) < 24*time.Hour {
			continue
		}
		b, err := fetchFeed(ctx, f.client, candidate.url)
		if err == nil {
			err = candidate.parse(b)
		}
		if err == nil {
			candidate.updated = time.Now().UTC()
			candidate.err = ""
			if f.dir != "" {
				if e := f.save(candidate); e != nil {
					candidate.err = "cache write failed"
				}
			}
			f.mu.Lock()
			f.feeds[i] = candidate
			f.mu.Unlock()
		} else {
			f.mu.Lock()
			f.feeds[i].err = "download or validation failed"
			f.mu.Unlock()
		}
	}
}

func fetchFeed(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	r.Header.Set("User-Agent", "cch-geoip/0.1")
	res, err := client.Do(r)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("feed status %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxFeedBytes {
		return nil, fmt.Errorf("feed too large")
	}
	return b, nil
}

func (f *Feeds) save(x feed) error {
	if err := os.MkdirAll(f.dir, 0700); err != nil {
		return err
	}
	b, err := json.Marshal(struct {
		Updated time.Time
		Data    []byte
	}{x.updated, x.data})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(f.dir, ".feed-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(f.dir, x.name+".json"))
}

func (f *Feeds) Lookup(ip netip.Addr, now time.Time) Result {
	f.mu.RLock()
	defer f.mu.RUnlock()
	r := Result{RiskLevel: "none", Blocklists: []Evidence{}, Sources: map[string]Source{}}
	for _, x := range f.feeds {
		s := Source{State: "disabled"}
		if f.enabled {
			s.State = "unavailable"
			s.Error = x.err
			if !x.updated.IsZero() {
				u := x.updated
				s.UpdatedAt = &u
				if !x.published.IsZero() {
					p := x.published
					s.PublishedAt = &p
				}
				s.State = "ready"
				if now.Sub(u) > f.maxAge || (!x.published.IsZero() && now.Sub(x.published) > f.maxAge) {
					s.State = "stale"
				}
				if s.State == "ready" {
					if x.kind == "tor" {
						s.Matched = x.exits[ip]
					} else {
						for _, p := range x.prefixes {
							if p.Contains(ip) {
								s.Matched = true
								break
							}
						}
					}
				}
			}
		}
		r.Sources[x.name] = s
		if !s.Matched {
			continue
		}
		if x.kind == "tor" {
			r.TorExit = true
			continue
		}
		r.IsThreat = true
		r.Score = 1
		r.RiskLevel = "critical"
		// Feed download time is not the IP's actual listing time.
		r.Blocklists = append(r.Blocklists, Evidence{Name: "Spamhaus DROP (" + x.name + ")", Category: "abuse"})
	}
	return r
}
