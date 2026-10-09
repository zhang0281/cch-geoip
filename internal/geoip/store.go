package geoip

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	geo "github.com/oschwald/geoip2-golang"
)

var ErrNotReady = errors.New("databases not ready")

type Record struct {
	City *geo.City
	ASN  *geo.ASN
}
type Status struct {
	Ready         bool      `json:"ready"`
	CityBuiltAt   time.Time `json:"city_built_at"`
	ASNBuiltAt    time.Time `json:"asn_built_at"`
	ReloadHealthy bool      `json:"reload_healthy"`
}

// Readers stay locked throughout lookup; swapping and closing cannot race mmap access.
type Store struct {
	mu                sync.RWMutex
	cityPath, asnPath string
	city, asn         *geo.Reader
	cityInfo, asnInfo os.FileInfo
	maxAge            time.Duration
	reloadHealthy     bool
}

func NewStore(cityPath, asnPath string, maxAge time.Duration) *Store {
	return &Store{cityPath: cityPath, asnPath: asnPath, maxAge: maxAge}
}

func (s *Store) Reload() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ci, err := os.Stat(s.cityPath)
	if err != nil {
		s.reloadHealthy = false
		return false, fmt.Errorf("stat City database: %w", err)
	}
	ai, err := os.Stat(s.asnPath)
	if err != nil {
		s.reloadHealthy = false
		return false, fmt.Errorf("stat ASN database: %w", err)
	}
	if same(ci, s.cityInfo) && same(ai, s.asnInfo) {
		s.reloadHealthy = true
		return false, nil
	}
	city, err := geo.Open(s.cityPath)
	if err != nil {
		s.reloadHealthy = false
		return false, fmt.Errorf("open City database: %w", err)
	}
	asn, err := geo.Open(s.asnPath)
	if err != nil {
		city.Close()
		s.reloadHealthy = false
		return false, fmt.Errorf("open ASN database: %w", err)
	}
	if (city.Metadata().DatabaseType != "GeoLite2-City" && city.Metadata().DatabaseType != "GeoIP2-City") ||
		asn.Metadata().DatabaseType != "GeoLite2-ASN" {
		city.Close()
		asn.Close()
		s.reloadHealthy = false
		return false, fmt.Errorf("expected City and GeoLite2-ASN databases")
	}
	// Verify that neither file was replaced while opening; retry on the next tick.
	ci2, e1 := os.Stat(s.cityPath)
	ai2, e2 := os.Stat(s.asnPath)
	if e1 != nil || e2 != nil || !same(ci, ci2) || !same(ai, ai2) {
		city.Close()
		asn.Close()
		s.reloadHealthy = false
		return false, fmt.Errorf("database changed during reload")
	}
	oldCity, oldASN := s.city, s.asn
	s.city, s.asn, s.cityInfo, s.asnInfo = city, asn, ci, ai
	s.reloadHealthy = true
	if oldCity != nil {
		oldCity.Close()
	}
	if oldASN != nil {
		oldASN.Close()
	}
	return true, nil
}

func same(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func (s *Store) statusLocked(now time.Time) Status {
	x := Status{ReloadHealthy: s.reloadHealthy}
	if s.city == nil || s.asn == nil {
		return x
	}
	x.CityBuiltAt = time.Unix(int64(s.city.Metadata().BuildEpoch), 0).UTC()
	x.ASNBuiltAt = time.Unix(int64(s.asn.Metadata().BuildEpoch), 0).UTC()
	x.Ready = s.maxAge == 0 || (now.Sub(x.CityBuiltAt) <= s.maxAge && now.Sub(x.ASNBuiltAt) <= s.maxAge)
	return x
}

func (s *Store) Status(now time.Time) Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.statusLocked(now)
}

func (s *Store) Lookup(ip net.IP, now time.Time) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.statusLocked(now).Ready {
		return Record{}, ErrNotReady
	}
	city, err := s.city.City(ip)
	if err != nil {
		return Record{}, fmt.Errorf("City lookup: %w", err)
	}
	asn, err := s.asn.ASN(ip)
	if err != nil {
		return Record{}, fmt.Errorf("ASN lookup: %w", err)
	}
	return Record{City: city, ASN: asn}, nil
}

func (s *Store) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.city != nil {
		s.city.Close()
		s.city = nil
	}
	if s.asn != nil {
		s.asn.Close()
		s.asn = nil
	}
}
