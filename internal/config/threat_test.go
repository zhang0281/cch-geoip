package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestThreatConfig(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"THREAT_FEEDS_ENABLED", "oops"}, {"ABUSEIPDB_ENABLED", "oops"}, {"THREAT_UPDATE_INTERVAL", "1h"},
		{"THREAT_MAX_AGE", "0s"}, {"ABUSEIPDB_TIMEOUT", "2s"}, {"ABUSEIPDB_TIMEOUT", "bad"},
		{"ABUSEIPDB_CACHE_TTL", "0s"}, {"ABUSEIPDB_DAILY_BUDGET", "0"}, {"ABUSEIPDB_DAILY_BUDGET", "bad"},
		{"ABUSEIPDB_API_KEY", "two words"}, {"ABUSEIPDB_ENABLED", "true"},
		{"ABUSEIPDB_BLACKLIST_ENABLED", "oops"}, {"ABUSEIPDB_CHECK_ENABLED", "oops"},
	} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			clear(t)
			t.Setenv(tc.key, tc.value)
			if _, err := Load(); err == nil {
				t.Fatal("invalid config accepted")
			}
		})
	}
	clear(t)
	c, err := Load()
	if err != nil || !c.AbuseBlacklistEnabled || !c.AbuseCheckEnabled || c.AbuseEnabled {
		t.Fatal("unexpected Abuse defaults", err)
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("synthetic-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ABUSEIPDB_ENABLED", "true")
	t.Setenv("ABUSEIPDB_API_KEY_FILE", path)
	c, err = Load()
	if err != nil || c.AbuseKey != "synthetic-key" {
		t.Fatal(err)
	}
	t.Setenv("ABUSEIPDB_API_KEY", "conflict")
	if _, err = Load(); err == nil {
		t.Fatal("conflicting sources accepted")
	}
}
