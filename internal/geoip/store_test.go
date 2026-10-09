package geoip

import (
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/zhang0281/cch-geoip/internal/testdb"
)

func TestLookupReloadAndCorruption(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	cp, ap, err := testdb.Write(dir, "First", now)
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(cp, ap, 30*24*time.Hour)
	defer s.Close()
	if changed, err := s.Reload(); !changed || err != nil {
		t.Fatal(changed, err)
	}
	for _, ip := range []string{"8.8.8.8", "2001:4860:4860::8888"} {
		r, e := s.Lookup(net.ParseIP(ip), now)
		if e != nil || r.City.City.Names["en"] != "First" || r.ASN.AutonomousSystemNumber != 15169 {
			t.Fatalf("lookup: %+v %v", r, e)
		}
	}
	if changed, err := s.Reload(); changed || err != nil {
		t.Fatal(changed, err)
	}
	if _, _, err = testdb.Write(dir, "Second", now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reload(); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "bad")
	if err = os.WriteFile(bad, []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(bad, cp); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reload(); err == nil {
		t.Fatal("corrupt update accepted")
	}
	r, err := s.Lookup(net.ParseIP("8.8.8.8"), now)
	if err != nil || r.City.City.Names["en"] != "Second" || s.Status(now).ReloadHealthy {
		t.Fatal("last known good readers not retained", err)
	}
	if _, err = s.Lookup(net.ParseIP("8.8.8.8"), now.Add(31*24*time.Hour)); err != ErrNotReady {
		t.Fatal("stale database served", err)
	}
	if _, _, err = testdb.Write(dir, "Recovered", now); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Reload(); err != nil {
		t.Fatal(err)
	}
	if !s.Status(now).ReloadHealthy {
		t.Fatal("recovery failed")
	}
}

func TestMissingAndWrongDatabases(t *testing.T) {
	s := NewStore("/missing", "/missing", 0)
	defer s.Close()
	if _, err := s.Reload(); err == nil {
		t.Fatal("missing accepted")
	}
	if _, err := s.Lookup(net.ParseIP("8.8.8.8"), time.Now()); err != ErrNotReady {
		t.Fatal(err)
	}
	cp, ap, err := testdb.Write(t.TempDir(), "City", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s = NewStore(ap, cp, 0)
	defer s.Close()
	if _, err = s.Reload(); err == nil {
		t.Fatal("swapped database types accepted")
	}
}

func TestConcurrentReload(t *testing.T) {
	dir := t.TempDir()
	cp, ap, err := testdb.Write(dir, "Before", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := NewStore(cp, ap, 0)
	defer s.Close()
	if _, err = s.Reload(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				if _, e := s.Lookup(net.ParseIP("8.8.8.8"), time.Now()); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	for range 3 {
		if _, _, err = testdb.Write(dir, "After", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err = s.Reload(); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
}
