package ip2region

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lionsoul2014/ip2region/binding/golang/service"
)

var ErrUnavailable = errors.New("ip2region database unavailable")

type Record struct {
	Available   bool
	Found       bool
	Country     string
	Province    string
	City        string
	ISP         string
	CountryCode string
	Version     string
}

type Store struct {
	mu             sync.RWMutex
	reloadMu       sync.Mutex
	v4Path, v6Path string
	v4Info, v6Info os.FileInfo
	search         *service.Ip2Region
	version        string
}

func NewStore(v4Path, v6Path string) *Store {
	return &Store{v4Path: v4Path, v6Path: v6Path}
}

func (s *Store) Reload() (bool, error) {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	v4, v4err := fileInfo(s.v4Path)
	v6, v6err := fileInfo(s.v6Path)
	if v4err != nil && v6err != nil {
		return false, fmt.Errorf("ip2region databases unavailable: v4=%v; v6=%v", v4err, v6err)
	}
	s.mu.RLock()
	unchanged := same(v4, s.v4Info) && same(v6, s.v6Info) && s.search != nil
	missingLoaded := (s.v4Info != nil && v4err != nil) || (s.v6Info != nil && v6err != nil)
	s.mu.RUnlock()
	if missingLoaded {
		return false, fmt.Errorf("loaded ip2region database disappeared")
	}
	if unchanged {
		return false, nil
	}
	var v4cfg, v6cfg *service.Config
	var err error
	if v4 != nil {
		v4cfg, err = service.NewV4Config(service.VIndexCache, s.v4Path, 4)
		if err != nil {
			return false, fmt.Errorf("load IPv4 xdb: %w", err)
		}
	}
	if v6 != nil {
		v6cfg, err = service.NewV6Config(service.VIndexCache, s.v6Path, 4)
		if err != nil {
			return false, fmt.Errorf("load IPv6 xdb: %w", err)
		}
	}
	search, err := service.NewIp2Region(v4cfg, v6cfg)
	if err != nil {
		return false, fmt.Errorf("create ip2region searcher: %w", err)
	}
	v4After, _ := fileInfo(s.v4Path)
	v6After, _ := fileInfo(s.v6Path)
	if (v4 != nil && !same(v4, v4After)) || (v6 != nil && !same(v6, v6After)) {
		search.Close()
		return false, fmt.Errorf("ip2region database changed during reload")
	}
	s.mu.Lock()
	old := s.search
	s.search, s.v4Info, s.v6Info = search, v4, v6
	s.version = s.versionLocked()
	s.mu.Unlock()
	if old != nil {
		old.Close()
	}
	return true, nil
}

func fileInfo(path string) (os.FileInfo, error) {
	if path == "" {
		return nil, os.ErrNotExist
	}
	info, err := os.Stat(path)
	if err == nil && (!info.Mode().IsRegular() || info.Size() > 64<<20) {
		return nil, fmt.Errorf("invalid xdb file size or type")
	}
	return info, err
}

func same(a, b os.FileInfo) bool {
	if a == nil && b == nil {
		return true
	}
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime() == b.ModTime()
}

func (s *Store) Lookup(ip netip.Addr) (Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.search == nil {
		return Record{}, ErrUnavailable
	}
	raw, err := s.search.Search(ip.String())
	if err != nil {
		return Record{}, fmt.Errorf("ip2region lookup: %w", err)
	}
	r := parse(raw)
	r.Available = true
	r.Version = s.version
	return r, nil
}

func (s *Store) versionLocked() string {
	for _, path := range []string{s.v4Path, s.v6Path} {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			version := filepath.Base(filepath.Dir(resolved))
			if strings.HasPrefix(version, "v") && len(version) < 32 {
				return version
			}
		}
	}
	return "unknown"
}

func parse(raw string) Record {
	parts := strings.Split(raw, "|")
	if len(parts) != 5 {
		return Record{}
	}
	country, province, city, isp, code := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[2]), strings.TrimSpace(parts[3]), strings.TrimSpace(parts[4])
	if country == "" && province == "" && city == "" && isp == "" && code == "" {
		return Record{}
	}
	for _, value := range []string{country, province, city, isp, code} {
		if len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return Record{}
		}
	}
	province, city, isp = clean(province), clean(city), clean(isp)
	return Record{Found: true, Country: country, Province: province, City: city, ISP: isp, CountryCode: strings.ToUpper(code)}
}

func clean(value string) string {
	if value == "0" || value == "-" || value == "Reserved" {
		return ""
	}
	return value
}

func (s *Store) Close() {
	s.reloadMu.Lock()
	defer s.reloadMu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.search != nil {
		s.search.Close()
		s.search = nil
	}
}
