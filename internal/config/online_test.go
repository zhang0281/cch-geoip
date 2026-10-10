package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOnlineGeoConfiguration(t *testing.T) {
	clear(t)
	c, err := Load()
	if err != nil || c.OnlineEnabled || c.OnlineCacheTTL != 24*time.Hour || c.OnlineTimeout != 2*time.Second || c.OnlineIPAPIBudget != 900 {
		t.Fatal("invalid online defaults", err)
	}
	t.Setenv("ONLINE_GEO_ENABLED", "true")
	c, err = Load()
	if err != nil || !c.OnlineEnabled || c.OnlineKey != "" {
		t.Fatal("keyless ipwho.is fallback unavailable", err)
	}
	p := filepath.Join(t.TempDir(), "ipapi-key")
	if err := os.WriteFile(p, []byte("fixture-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("IPAPI_IS_API_KEY_FILE", p)
	c, err = Load()
	if err != nil || c.OnlineKey != "fixture-key" {
		t.Fatal("key file not accepted", err)
	}
	t.Setenv("IPAPI_IS_API_KEY", "fixture-value")
	if _, err = Load(); err == nil {
		t.Fatal("conflicting key sources accepted")
	}
}

func TestOnlineGeoConfigRejectsOverspendingAndUnboundedLatency(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"ONLINE_GEO_ENABLED", "invalid"}, {"ONLINE_GEO_TIMEOUT", "3s"}, {"ONLINE_GEO_TIMEOUT", "0s"},
		{"ONLINE_GEO_CACHE_TTL", "30m"}, {"ONLINE_GEO_CACHE_TTL", "169h"},
		{"IPAPI_IS_DAILY_BUDGET", "1001"}, {"IPWHO_IS_DAILY_BUDGET", "1001"},
		{"IPAPI_IS_DAILY_BUDGET", "0"}, {"IPWHO_IS_DAILY_BUDGET", "NaN"},
		{"IPAPI_IS_API_KEY", "bad key"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			clear(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid online configuration accepted")
			}
		})
	}
}
