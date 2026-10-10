package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func clear(t *testing.T) {
	for _, k := range []string{"ONLINE_GEO_ENABLED", "ONLINE_GEO_CACHE_DIR", "ONLINE_GEO_TIMEOUT", "ONLINE_GEO_CACHE_TTL", "IPSB_DAILY_BUDGET", "GEOJS_DAILY_BUDGET", "IPWHO_IS_DAILY_BUDGET"} {
		t.Setenv(k, "")
	}
	t.Setenv("ABUSEIPDB_BLACKLIST_ENABLED", "")
	t.Setenv("ABUSEIPDB_CHECK_ENABLED", "")
	for _, k := range []string{"GEOIP_LISTEN", "GEOIP_CITY_DB", "GEOIP_ASN_DB", "IP2REGION_DATA_DIR", "IP2REGION_V4_DB", "IP2REGION_V6_DB", "IP2REGION_ENABLED", "IP2REGION_VERSION", "GEOIP_API_TOKEN", "GEOIP_API_TOKEN_FILE", "GEOIP_RELOAD_INTERVAL", "GEOIP_MAX_DB_AGE", "THREAT_FEEDS_ENABLED", "THREAT_CACHE_DIR", "THREAT_UPDATE_INTERVAL", "THREAT_MAX_AGE", "ABUSEIPDB_ENABLED", "ABUSEIPDB_API_KEY", "ABUSEIPDB_API_KEY_FILE", "ABUSEIPDB_TIMEOUT", "ABUSEIPDB_CACHE_TTL", "ABUSEIPDB_DAILY_BUDGET"} {
		t.Setenv(k, "")
	}
}

func TestDefaults(t *testing.T) {
	clear(t)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8080" || c.MaxDBAge != 720*time.Hour || c.ReloadInterval != time.Minute {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestConfigErrors(t *testing.T) {
	for _, tc := range []struct{ key, value string }{{"GEOIP_RELOAD_INTERVAL", "0s"}, {"GEOIP_MAX_DB_AGE", "-1h"}, {"GEOIP_RELOAD_INTERVAL", "oops"}, {"GEOIP_API_TOKEN", "two tokens"}, {"GEOIP_API_TOKEN_FILE", "/missing-token"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			clear(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestTokenFile(t *testing.T) {
	clear(t)
	p := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(p, []byte("test-secret\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEOIP_API_TOKEN_FILE", p)
	c, err := Load()
	if err != nil || c.Token != "test-secret" {
		t.Fatalf("token file: %v", err)
	}
	t.Setenv("GEOIP_API_TOKEN", "other")
	if _, err := Load(); err == nil {
		t.Fatal("conflicting sources accepted")
	}
	t.Setenv("GEOIP_API_TOKEN", "")
	if err := os.WriteFile(p, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatal("empty token accepted")
	}
}
