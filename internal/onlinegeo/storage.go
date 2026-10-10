package onlinegeo

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"time"
)

const stateFile = "onlinegeo-state.json"
const maxStateBytes = 4 << 20

func (s *Service) load(now time.Time) error {
	if err := os.MkdirAll(s.options.CacheDir, 0700); err != nil {
		return errors.New("online geolocation storage unavailable")
	}
	path := filepath.Join(s.options.CacheDir, stateFile)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxStateBytes {
		return errors.New("invalid online geolocation persistent state")
	}
	f, err := os.Open(path)
	if err != nil {
		return errors.New("online geolocation storage unavailable")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxStateBytes+1))
	if err != nil || len(b) > maxStateBytes {
		return errors.New("invalid online geolocation persistent state")
	}
	var state diskState
	decoder := json.NewDecoder(bytes.NewReader(b))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&state) != nil || decoder.Decode(new(any)) != io.EOF || !validState(state, now) {
		return errors.New("invalid online geolocation persistent state")
	}
	if state.Version == 1 {
		delete(state.Quotas, "ipapi.is")
		state.Quotas[ipsb] = quota{}
		state.Quotas[geojs] = quota{}
		for address, entry := range state.Cache {
			if entry.Source == "ipapi.is" {
				delete(state.Cache, address)
			}
		}
		state.Version = 2
	}
	s.state = state
	s.prune(now)
	return nil
}

func validState(state diskState, now time.Time) bool {
	providers := []string{ipsb, ipwho, geojs}
	if state.Version == 1 {
		providers = []string{"ipapi.is", ipwho}
	}
	if (state.Version != 1 && state.Version != 2) || state.Cache == nil || len(state.Cache) > maxEntries || len(state.Quotas) != len(providers) {
		return false
	}
	for _, provider := range providers {
		q, ok := state.Quotas[provider]
		if !ok || len(q.Attempts) > 1000 || q.BlockedUntil.After(now.Add(24*time.Hour)) {
			return false
		}
		for _, timestamp := range q.Attempts {
			if timestamp.IsZero() || timestamp.After(now) {
				return false
			}
		}
	}
	for address, e := range state.Cache {
		validProvider := false
		for _, provider := range providers {
			if e.Source == provider {
				validProvider = true
			}
		}
		ip, err := netip.ParseAddr(address)
		if err != nil || !publicAddress(ip) || ip.Unmap().String() != address ||
			!validProvider || e.UpdatedAt.IsZero() || e.UpdatedAt.After(now) || !validateLocation(&e.Location) {
			return false
		}
	}
	return true
}

func (s *Service) persist() error {
	b, err := json.Marshal(s.state)
	if err != nil || len(b) > maxStateBytes {
		return errors.New("persistent state too large")
	}
	f, err := os.CreateTemp(s.options.CacheDir, ".onlinegeo-*")
	if err != nil {
		return err
	}
	temporary := f.Name()
	defer os.Remove(temporary)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(temporary, filepath.Join(s.options.CacheDir, stateFile)); err != nil {
		return err
	}
	dir, err := os.Open(s.options.CacheDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
