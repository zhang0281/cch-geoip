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

type blacklistRecord struct {
	IP           string    `json:"ipAddress"`
	Score        *int      `json:"abuseConfidenceScore"`
	LastReported time.Time `json:"lastReportedAt"`
}

type blacklistPayload struct {
	Meta struct {
		Generated time.Time `json:"generatedAt"`
	} `json:"meta"`
	Data *[]blacklistRecord `json:"data"`
}

type blacklistSnapshot struct {
	Attempt, Updated time.Time
	Data             []byte
}

type Blacklist struct {
	mu                 sync.RWMutex
	refreshMu          sync.Mutex
	key, dir, endpoint string
	maxAge             time.Duration
	client             *http.Client
	snapshot           blacklistSnapshot
	published          time.Time
	entries            map[netip.Addr]blacklistRecord
	err                string
}

func NewBlacklist(key, dir string, maxAge time.Duration) *Blacklist {
	return &Blacklist{key: key, dir: dir, maxAge: maxAge,
		endpoint: "https://api.abuseipdb.com/api/v2/blacklist",
		client: &http.Client{Timeout: 30 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func parseBlacklist(data []byte, now time.Time) (blacklistPayload, map[netip.Addr]blacklistRecord, error) {
	var p blacklistPayload
	if len(data) > maxFeedBytes || json.Unmarshal(data, &p) != nil || p.Data == nil ||
		p.Meta.Generated.IsZero() || p.Meta.Generated.After(now.Add(time.Minute)) || len(*p.Data) > 10000 {
		return p, nil, fmt.Errorf("invalid blacklist response")
	}
	entries := make(map[netip.Addr]blacklistRecord, len(*p.Data))
	for _, record := range *p.Data {
		ip, err := netip.ParseAddr(record.IP)
		if err != nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || record.Score == nil ||
			*record.Score < 25 || *record.Score > 100 || record.LastReported.IsZero() ||
			record.LastReported.After(p.Meta.Generated.Add(time.Minute)) {
			return p, nil, fmt.Errorf("invalid blacklist record")
		}
		ip = ip.Unmap()
		if _, exists := entries[ip]; exists {
			return p, nil, fmt.Errorf("duplicate blacklist record")
		}
		entries[ip] = record
	}
	return p, entries, nil
}

func (b *Blacklist) Restore() {
	if b.dir == "" {
		return
	}
	f, err := os.Open(filepath.Join(b.dir, "abuseipdb_blacklist.json"))
	if err != nil {
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 2*maxFeedBytes+1))
	var s blacklistSnapshot
	now := time.Now()
	if err != nil || len(data) > 2*maxFeedBytes || json.Unmarshal(data, &s) != nil ||
		s.Attempt.IsZero() || s.Attempt.After(now.Add(time.Minute)) ||
		s.Updated.After(s.Attempt) {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.snapshot.Attempt = s.Attempt
	if len(s.Data) == 0 || s.Updated.IsZero() {
		return
	}
	p, entries, err := parseBlacklist(s.Data, now)
	if err != nil {
		return
	}
	b.snapshot = s
	b.published, b.entries = p.Meta.Generated, entries
}

// Persist attempts before sending credentials; failures and restarts cannot trigger download loops.
func (b *Blacklist) Refresh(ctx context.Context) {
	if b.key == "" || ctx.Err() != nil {
		return
	}
	b.refreshMu.Lock()
	defer b.refreshMu.Unlock()
	now := time.Now().UTC()
	b.mu.Lock()
	if now.Sub(b.snapshot.Attempt) < 24*time.Hour {
		b.mu.Unlock()
		return
	}
	b.snapshot.Attempt = now
	s := b.snapshot
	b.mu.Unlock()
	if err := b.save(s); err != nil {
		b.setError("cache write failed; download skipped")
		return
	}
	data, err := b.fetch(ctx)
	if err != nil {
		b.setError(err.Error())
		return
	}
	p, entries, err := parseBlacklist(data, time.Now())
	if err != nil {
		b.setError("blacklist validation failed")
		return
	}
	if now.Sub(p.Meta.Generated) > b.maxAge {
		b.setError("provider blacklist is stale")
		return
	}
	s.Data, s.Updated = data, now
	if err := b.save(s); err != nil {
		b.setError("cache write failed; retaining previous snapshot")
		return
	}
	b.mu.Lock()
	b.snapshot, b.published, b.entries, b.err = s, p.Meta.Generated, entries, ""
	b.mu.Unlock()
}

func (b *Blacklist) fetch(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, b.endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("invalid blacklist configuration")
	}
	q := req.URL.Query()
	q.Set("limit", "10000")
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Key", b.key)
	req.Header.Set("Accept", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("blacklist request failed")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blacklist HTTP status %d", res.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxFeedBytes+1))
	if err != nil || len(data) > maxFeedBytes {
		return nil, fmt.Errorf("invalid blacklist response")
	}
	return data, nil
}

func (b *Blacklist) setError(message string) {
	b.mu.Lock()
	b.err = message
	b.mu.Unlock()
}

func (b *Blacklist) save(s blacklistSnapshot) error {
	if b.dir == "" {
		return nil
	}
	if err := os.MkdirAll(b.dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(b.dir, ".blacklist-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(b.dir, "abuseipdb_blacklist.json"))
}

func (b *Blacklist) Lookup(ip netip.Addr, now time.Time) AbuseResult {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r := unavailable(b.err)
	if b.key == "" {
		r.Source.State = "disabled"
		return r
	}
	if b.snapshot.Updated.IsZero() {
		return r
	}
	u, p := b.snapshot.Updated, b.published
	r.Source.UpdatedAt, r.Source.PublishedAt = &u, &p
	if now.Sub(u) > b.maxAge || now.Sub(p) > b.maxAge {
		r.Source.State = "stale"
		return r
	}
	r.Source.State = "ready"
	if record, ok := b.entries[ip.Unmap()]; ok {
		r.Score = *record.Score
		r.Source.Matched = true
		r.Source.AbuseConfidenceScore = record.Score
		last := record.LastReported
		r.Source.LastReportedAt = &last
	}
	return r
}
