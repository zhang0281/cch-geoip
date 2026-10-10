package onlinegeo

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	_ "time/tzdata"
)

// The Docker gate runs this static test binary as the image's non-root user.
func TestRuntimeOnlineStatePermissions(t *testing.T) {
	root := os.Getenv("CCH_TEST_CACHE_DIR")
	if root == "" {
		t.Skip("Docker runtime only")
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, `{"ip":"3.5.140.2","success":true,"country_code":"KR","country":"South Korea","continent_code":"AS","continent":"Asia","region":"Incheon","city":"Incheon","latitude":37.45,"longitude":126.7,"timezone":{"id":"Asia/Seoul"}}`)
	}))
	defer server.Close()
	opts := Options{CacheDir: filepath.Join(root, "online-runtime")}
	s, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	s.endpoints[ipwho] = server.URL
	ip := netip.MustParseAddr("3.5.140.2")
	first := s.Lookup(context.Background(), ip, time.Now())
	if first.Location == nil || first.Location.City != "Incheon" || first.CacheHit {
		t.Fatal("runtime online failed")
	}
	s, err = New(opts)
	if err != nil {
		t.Fatal(err)
	}
	second := s.Lookup(context.Background(), ip, time.Now())
	if !second.CacheHit || calls.Load() != 1 {
		t.Fatal("runtime restore did not reuse cache")
	}
	st, err := os.Stat(filepath.Join(opts.CacheDir, stateFile))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal("runtime state permissions incorrect", err)
	}
}
