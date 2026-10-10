package config

import (
	"testing"
	"time"
)

func TestOnlineGeoConfiguration(t *testing.T) {
	clear(t)
	c, err := Load()
	if err != nil || c.OnlineEnabled || c.OnlineCacheTTL != 24*time.Hour || c.OnlineTimeout != 2*time.Second || c.OnlineIPSBBudget != 900 {
		t.Fatal("invalid online defaults", err)
	}
	t.Setenv("ONLINE_GEO_ENABLED", "true")
	c, err = Load()
	if err != nil || !c.OnlineEnabled {
		t.Fatal("keyless online providers unavailable", err)
	}

}

func TestOnlineGeoConfigRejectsOverspendingAndUnboundedLatency(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"ONLINE_GEO_ENABLED", "invalid"}, {"ONLINE_GEO_TIMEOUT", "3s"}, {"ONLINE_GEO_TIMEOUT", "0s"},
		{"ONLINE_GEO_CACHE_TTL", "30m"}, {"ONLINE_GEO_CACHE_TTL", "169h"},
		{"IPSB_DAILY_BUDGET", "1001"}, {"IPWHO_IS_DAILY_BUDGET", "1001"},
		{"IPSB_DAILY_BUDGET", "0"}, {"IPWHO_IS_DAILY_BUDGET", "NaN"},
		{"GEOJS_DAILY_BUDGET", "1001"},
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
