package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Listen, CityDB, ASNDB, Token                          string
	ReloadInterval, MaxDBAge                              time.Duration
	FeedsEnabled, AbuseEnabled                            bool
	FeedDir, AbuseKey                                     string
	FeedInterval, FeedMaxAge, AbuseTimeout, AbuseCacheTTL time.Duration
	AbuseDailyBudget                                      int
}

func Load() (Config, error) {
	c := Config{Listen: value("GEOIP_LISTEN", "127.0.0.1:8080"),
		CityDB: value("GEOIP_CITY_DB", "/usr/share/GeoIP/GeoLite2-City.mmdb"),
		ASNDB:  value("GEOIP_ASN_DB", "/usr/share/GeoIP/GeoLite2-ASN.mmdb")}
	var err error
	c.ReloadInterval, err = duration("GEOIP_RELOAD_INTERVAL", time.Minute, false)
	if err != nil {
		return c, err
	}
	c.MaxDBAge, err = duration("GEOIP_MAX_DB_AGE", 30*24*time.Hour, true)
	if err != nil {
		return c, err
	}
	c.Token = strings.TrimSpace(os.Getenv("GEOIP_API_TOKEN"))
	if path := os.Getenv("GEOIP_API_TOKEN_FILE"); path != "" {
		if c.Token != "" {
			return c, fmt.Errorf("set only one token source")
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return c, fmt.Errorf("read API token file: %w", err)
		}
		c.Token = strings.TrimSpace(string(b))
		if c.Token == "" {
			return c, fmt.Errorf("API token file is empty")
		}
	}
	if strings.ContainsAny(c.Token, "\r\n\t ") {
		return c, fmt.Errorf("API token must not contain whitespace")
	}
	if err = loadThreat(&c); err != nil {
		return c, err
	}
	return c, nil
}

func loadThreat(c *Config) error {
	var err error
	c.FeedsEnabled, err = boolean("THREAT_FEEDS_ENABLED", false)
	if err != nil {
		return err
	}
	c.AbuseEnabled, err = boolean("ABUSEIPDB_ENABLED", false)
	if err != nil {
		return err
	}
	c.FeedDir = value("THREAT_CACHE_DIR", "")
	c.FeedInterval, err = duration("THREAT_UPDATE_INTERVAL", 24*time.Hour, false)
	if err != nil {
		return err
	}
	if c.FeedInterval < 24*time.Hour {
		return fmt.Errorf("THREAT_UPDATE_INTERVAL must be at least 24h")
	}
	c.FeedMaxAge, err = duration("THREAT_MAX_AGE", 72*time.Hour, false)
	if err != nil {
		return err
	}
	c.AbuseTimeout, err = duration("ABUSEIPDB_TIMEOUT", 500*time.Millisecond, false)
	if err != nil {
		return err
	}
	if c.AbuseTimeout > time.Second {
		return fmt.Errorf("ABUSEIPDB_TIMEOUT must not exceed 1s")
	}
	c.AbuseCacheTTL, err = duration("ABUSEIPDB_CACHE_TTL", 6*time.Hour, false)
	if err != nil {
		return err
	}
	c.AbuseDailyBudget, err = strconv.Atoi(value("ABUSEIPDB_DAILY_BUDGET", "900"))
	if err != nil || c.AbuseDailyBudget < 1 {
		return fmt.Errorf("invalid ABUSEIPDB_DAILY_BUDGET")
	}
	c.AbuseKey, err = secret("ABUSEIPDB_API_KEY", "ABUSEIPDB_API_KEY_FILE")
	if err != nil {
		return err
	}
	if c.AbuseEnabled && c.AbuseKey == "" {
		return fmt.Errorf("ABUSEIPDB_ENABLED requires an API key")
	}
	return nil
}

func secret(env, fileEnv string) (string, error) {
	s := strings.TrimSpace(os.Getenv(env))
	if path := os.Getenv(fileEnv); path != "" {
		if s != "" {
			return "", fmt.Errorf("set only one %s source", env)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s file: %w", env, err)
		}
		s = strings.TrimSpace(string(b))
		if s == "" {
			return "", fmt.Errorf("%s file is empty", env)
		}
	}
	if strings.ContainsAny(s, "\r\n\t ") {
		return "", fmt.Errorf("%s must not contain whitespace", env)
	}
	return s, nil
}

func boolean(env string, fallback bool) (bool, error) {
	s := os.Getenv(env)
	if s == "" {
		return fallback, nil
	}
	b, err := strconv.ParseBool(s)
	if err != nil {
		return false, fmt.Errorf("invalid %s boolean", env)
	}
	return b, nil
}

func value(key, fallback string) string {
	if s := os.Getenv(key); s != "" {
		return s
	}
	return fallback
}

func duration(key string, fallback time.Duration, allowZero bool) (time.Duration, error) {
	s := os.Getenv(key)
	if s == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 || (!allowZero && d == 0) {
		return 0, fmt.Errorf("invalid %s duration", key)
	}
	return d, nil
}
