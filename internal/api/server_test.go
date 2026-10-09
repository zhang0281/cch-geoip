package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zhang0281/cch-geoip/internal/geoip"
	"github.com/zhang0281/cch-geoip/internal/testdb"
)

func TestHTTPWithRealMMDB(t *testing.T) {
	cp, ap, err := testdb.Write(t.TempDir(), "Test City", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	s := geoip.NewStore(cp, ap, 0)
	defer s.Close()
	if _, err = s.Reload(); err != nil {
		t.Fatal(err)
	}
	h := NewHandler(s, nil, "test-token", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, tc := range []struct {
		path, auth string
		status     int
	}{
		{"/healthz", "", 200}, {"/readyz", "", 200}, {"/v1/ip2location/8.8.8.8", "", 401},
		{"/v1/ip2location/8.8.8.8?lang=zh-CN", "Bearer test-token", 200},
		{"/v1/ip2location/2001:4860:4860::8888", "Bearer test-token", 200},
		{"/v1/ip2location/::ffff:8.8.8.8", "Bearer test-token", 200},
		{"/v1/ip2location/1.1.1.1", "Bearer test-token", 200},
		{"/v1/ip2location/hostname.example", "Bearer test-token", 400},
		{"/v1/ip2location/127.0.0.1", "Bearer test-token", 400},
		{"/v1/ip2location/100.64.0.1", "Bearer test-token", 400},
		{"/v1/ip2location/fe80::1%25eth0", "Bearer test-token", 400},
		{"/v1/ip2location/8.8.8.8?lang=xx", "Bearer test-token", 400},
	} {
		t.Run(tc.path+tc.auth, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, nil)
			req.Header.Set("Authorization", tc.auth)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d body %s", w.Code, w.Body.String())
			}
			if tc.status != 200 || len(tc.path) < 15 {
				return
			}
			var d map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
			assertCCH(t, d)
			if tc.path == "/v1/ip2location/8.8.8.8?lang=zh-CN" {
				country := d["location"].(map[string]any)["country"].(map[string]any)
				if country["name"] != "美国" {
					t.Fatal(country)
				}
			}
		})
	}
}

// Mirrors CCH's upstream validator and the fields its dialog dereferences directly.
func assertCCH(t *testing.T, d map[string]any) {
	t.Helper()
	if s, ok := d["ip"].(string); !ok || s == "" {
		t.Fatal("missing IP")
	}
	l := d["location"].(map[string]any)
	c := l["country"].(map[string]any)
	for _, k := range []string{"code", "name"} {
		if _, ok := c[k].(string); !ok {
			t.Fatal(k)
		}
	}
	if _, ok := c["flag"].(map[string]any)["emoji"].(string); !ok {
		t.Fatal("flag")
	}
	if _, ok := d["timezone"].(map[string]any)["id"].(string); !ok {
		t.Fatal("timezone")
	}
	a := d["connection"].(map[string]any)["asn"]
	if a != nil {
		if _, ok := a.(float64); !ok {
			t.Fatal("ASN")
		}
	}
	th := d["threat"].(map[string]any)
	if _, ok := th["score"].(float64); !ok {
		t.Fatal("score")
	}
	if _, ok := th["blocklists"].([]any); !ok {
		t.Fatal("blocklists")
	}
	if _, ok := c["languages"].([]any); !ok {
		t.Fatal("languages")
	}
	if _, ok := d["privacy"].(map[string]any); !ok {
		t.Fatal("privacy")
	}
}

func TestTimezoneAndLanguage(t *testing.T) {
	for _, tc := range []struct {
		date   string
		dst    bool
		offset int
	}{{"2026-01-01T12:00:00Z", false, -28800}, {"2026-07-01T12:00:00Z", true, -25200}} {
		now, _ := time.Parse(time.RFC3339, tc.date)
		z := timezone("America/Los_Angeles", now)
		if z["is_dst"] != tc.dst || z["utc_offset_seconds"] != tc.offset {
			t.Fatal(z)
		}
	}
	if timezone("invalid/zone", time.Now())["id"] != "" {
		t.Fatal("invalid timezone")
	}
	if lang, ok := language("ZH-hans"); !ok || lang != "zh-CN" {
		t.Fatal(lang, ok)
	}
}

func TestNotReady(t *testing.T) {
	s := geoip.NewStore("/missing", "/missing", 0)
	defer s.Close()
	h := NewHandler(s, nil, "", slog.Default())
	for _, p := range []string{"/readyz", "/v1/ip2location/8.8.8.8"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", p, nil))
		if w.Code != 503 {
			t.Fatal(w.Code)
		}
	}
}
