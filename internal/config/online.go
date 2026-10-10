package config

import (
	"fmt"
	"strconv"
	"time"
)

func loadOnline(c *Config) error {
	var err error
	c.OnlineEnabled, err = boolean("ONLINE_GEO_ENABLED", false)
	if err != nil {
		return err
	}
	c.OnlineCacheDir = value("ONLINE_GEO_CACHE_DIR", "/var/lib/cch-geoip/online")
	c.OnlineTimeout, err = duration("ONLINE_GEO_TIMEOUT", 2*time.Second, false)
	if err != nil || c.OnlineTimeout > 2*time.Second {
		return fmt.Errorf("ONLINE_GEO_TIMEOUT must be positive and at most 2s")
	}
	c.OnlineCacheTTL, err = duration("ONLINE_GEO_CACHE_TTL", 24*time.Hour, false)
	if err != nil || c.OnlineCacheTTL < time.Hour || c.OnlineCacheTTL > 7*24*time.Hour {
		return fmt.Errorf("ONLINE_GEO_CACHE_TTL must be between 1h and 168h")
	}
	c.OnlineIPSBBudget, err = onlineBudget("IPSB_DAILY_BUDGET")
	if err != nil {
		return err
	}
	c.OnlineIPWhoBudget, err = onlineBudget("IPWHO_IS_DAILY_BUDGET")
	if err != nil {
		return err
	}
	c.OnlineGeoJSBudget, err = onlineBudget("GEOJS_DAILY_BUDGET")
	return err
}

func onlineBudget(env string) (int, error) {
	n, err := strconv.Atoi(value(env, "900"))
	if err != nil || n < 1 || n > 1000 {
		return 0, fmt.Errorf("%s must be between 1 and 1000", env)
	}
	return n, nil
}
