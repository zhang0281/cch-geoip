package ip2region

import (
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestParse(t *testing.T) {
	if got := parse(""); got.Found {
		t.Fatal("empty record matched")
	}
	got := parse("中国|吉林省|长春市|移动|cn")
	if !got.Found || got.Country != "中国" || got.Province != "吉林省" || got.City != "长春市" || got.ISP != "移动" || got.CountryCode != "CN" {
		t.Fatalf("unexpected record: %+v", got)
	}
	if got = parse("bad|record"); got.Found {
		t.Fatal("short record matched")
	}
	if got = parse("中国|吉林省|长春市|移动\x00|CN"); got.Found {
		t.Fatal("control-character record matched")
	}
}

func TestRealV4AndV6Data(t *testing.T) {
	root := filepath.Join("..", "..", "data", "ip2region", "v3.18.0")
	required := false
	if configured := os.Getenv("IP2REGION_TEST_DATA_DIR"); configured != "" {
		root = configured
		required = true
	}
	if _, err := os.Stat(filepath.Join(root, "ip2region_v4.xdb")); err != nil {
		if required {
			t.Fatal("configured xdb test directory missing", err)
		}
		t.Skip("ip2region xdb fixture unavailable; run make ip2region-smoke for upstream integration")
	}
	s := NewStore(filepath.Join(root, "ip2region_v4.xdb"), filepath.Join(root, "ip2region_v6.xdb"))
	if changed, err := s.Reload(); err != nil || !changed {
		t.Fatalf("load xdb: changed=%v err=%v", changed, err)
	}
	t.Cleanup(s.Close)
	for _, tc := range []struct {
		ip, city, province, isp string
	}{
		{"223.104.143.182", "长春市", "吉林省", "移动"},
		{"2001:4860:4860::8888", "Miami", "Florida", "Google LLC"},
	} {
		r, err := s.Lookup(netip.MustParseAddr(tc.ip))
		if err != nil {
			t.Fatal(tc.ip, err)
		}
		if !r.Found || r.City != tc.city || r.Province != tc.province || r.ISP != tc.isp {
			t.Fatalf("%s: unexpected record %+v", tc.ip, r)
		}
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r, err := s.Lookup(netip.MustParseAddr("223.104.143.182")); err != nil || !r.Found || r.City != "长春市" {
				t.Errorf("concurrent lookup: %+v %v", r, err)
			}
		}()
	}
	wg.Wait()
	// Publish a truncated replacement in a separate directory and verify the
	// loaded readers survive both corrupt updates and a disappearing xdb.
	dir := filepath.Join(t.TempDir(), "v3.18.0")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	v4, err := os.ReadFile(filepath.Join(root, "ip2region_v4.xdb"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "ip2region_v4.xdb")
	if err = os.WriteFile(path, v4, 0644); err != nil {
		t.Fatal(err)
	}
	partial := NewStore(path, "")
	defer partial.Close()
	if _, err = partial.Reload(); err != nil {
		t.Fatal(err)
	}
	if changed, err := partial.Reload(); err != nil || changed {
		t.Fatal("unchanged v4-only store reloaded", changed, err)
	}
	bad := filepath.Join(dir, "bad.xdb")
	if err = os.WriteFile(bad, []byte("corrupt"), 0644); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(bad, path); err != nil {
		t.Fatal(err)
	}
	if _, err = partial.Reload(); err == nil {
		t.Fatal("corrupt update accepted")
	}
	if r, err := partial.Lookup(netip.MustParseAddr("223.104.143.182")); err != nil || r.City != "长春市" {
		t.Fatal("valid readers lost after bad update", err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err = partial.Reload(); err == nil {
		t.Fatal("missing update accepted")
	}
	if r, err := partial.Lookup(netip.MustParseAddr("223.104.143.182")); err != nil || r.City != "长春市" {
		t.Fatal("valid readers lost after missing file", err)
	}
}

func TestUnavailableStore(t *testing.T) {
	s := NewStore("/missing/v4.xdb", "/missing/v6.xdb")
	if _, err := s.Reload(); err == nil {
		t.Fatal("missing database accepted")
	}
	if _, err := s.Lookup(netip.MustParseAddr("8.8.8.8")); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("unexpected lookup error: %v", err)
	}
}
