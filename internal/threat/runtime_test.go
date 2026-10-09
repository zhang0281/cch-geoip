package threat

import (
	"crypto/x509"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRuntimeCachePermissions(t *testing.T) {
	dir := os.Getenv("CCH_TEST_CACHE_DIR")
	if dir == "" {
		t.Skip("Docker-only runtime check")
	}
	if os.Getuid() != 65532 {
		t.Fatal("runtime is not the configured unprivileged user")
	}
	f := NewFeeds(true, dir, 72*time.Hour)
	x := f.feeds[0]
	data := strings.ReplaceAll(dropFixture, "1791500000", strconv.FormatInt(time.Now().Unix(), 10))
	if err := x.parse([]byte(data)); err != nil {
		t.Fatal(err)
	}
	x.updated = time.Now()
	if err := f.save(x); err != nil {
		t.Fatal("cache volume is not writable", err)
	}
	f.Restore()
	if f.feeds[0].updated.IsZero() {
		t.Fatal("cache restore failed")
	}
	pool, err := x509.SystemCertPool()
	if err != nil || len(pool.Subjects()) == 0 {
		t.Fatal("system CA certificates unavailable", err)
	}
}
