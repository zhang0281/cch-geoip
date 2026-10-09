package threat

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const dropFixture = "{\"cidr\":\"8.8.8.0/24\",\"sblid\":\"SBL-test\"}\n{\"type\":\"metadata\",\"timestamp\":1791500000,\"copyright\":\"Synthetic test fixture\"}\n"

func TestFeedSnapshots(t *testing.T) {
	var fail atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(503)
			return
		}
		if r.URL.Path == "/tor" {
			fmt.Fprint(w, "1.1.1.1\n")
			return
		}
		fmt.Fprint(w, strings.ReplaceAll(dropFixture, "1791500000", strconv.FormatInt(time.Now().Unix(), 10)))
	}))
	defer server.Close()
	dir := t.TempDir()
	f := NewFeeds(true, dir, 72*time.Hour)
	f.feeds = []feed{{name: "spamhaus_drop_v4", kind: "drop", url: server.URL + "/drop"}, {name: "tor_exit", kind: "tor", url: server.URL + "/tor"}}
	f.Refresh(context.Background())
	r := f.Lookup(netip.MustParseAddr("8.8.8.8"), time.Now())
	if !r.IsThreat || r.Score != 1 || len(r.Blocklists) != 1 {
		t.Fatal(r)
	}
	tor := f.Lookup(netip.MustParseAddr("1.1.1.1"), time.Now())
	if !tor.TorExit || tor.IsThreat || tor.Score != 0 {
		t.Fatal("Tor classified as attack", tor)
	}
	fail.Store(true)
	for i := range f.feeds {
		f.feeds[i].updated = time.Now().Add(-25 * time.Hour)
	}
	f.Refresh(context.Background())
	r = f.Lookup(netip.MustParseAddr("8.8.8.8"), time.Now())
	if !r.IsThreat || r.Sources["spamhaus_drop_v4"].Error == "" {
		t.Fatal("good snapshot lost", r)
	}
	r = f.Lookup(netip.MustParseAddr("8.8.8.8"), time.Now().Add(73*time.Hour))
	if r.IsThreat || r.Sources["spamhaus_drop_v4"].State != "stale" {
		t.Fatal(r)
	}
	restored := NewFeeds(true, dir, 72*time.Hour)
	restored.Restore()
	if !restored.Lookup(netip.MustParseAddr("8.8.8.8"), time.Now()).IsThreat {
		t.Fatal("persistent cache not restored")
	}
}

func TestRejectMalformedFeeds(t *testing.T) {
	for _, data := range []string{"", `<html>failure</html>`, "{\"cidr\":\"0.0.0.0/0\"}\n", dropFixture[:strings.Index(dropFixture, "\n")], strings.Repeat("x", maxFeedBytes+1)} {
		f := feed{name: "spamhaus_drop_v4", kind: "drop"}
		if f.parse([]byte(data)) == nil {
			t.Fatal("invalid DROP accepted")
		}
	}
	for _, data := range []string{"", "# empty\n", "8.8.8.8\ninvalid\n", "127.0.0.1\n", "10.0.0.1\n"} {
		f := feed{kind: "tor"}
		if f.parse([]byte(data)) == nil {
			t.Fatal("invalid Tor accepted", data)
		}
	}
}

func TestFeedDisabledAndOversize(t *testing.T) {
	f := NewFeeds(false, "", time.Hour)
	f.Refresh(context.Background())
	if s := f.Lookup(netip.MustParseAddr("8.8.8.8"), time.Now()).Sources["tor_exit"]; s.State != "disabled" {
		t.Fatal(s)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", maxFeedBytes+1)) }))
	defer srv.Close()
	if _, err := fetchFeed(context.Background(), srv.Client(), srv.URL); err == nil {
		t.Fatal("oversized response accepted")
	}
}
