package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/zhang0281/cch-geoip/internal/api"
	"github.com/zhang0281/cch-geoip/internal/config"
	"github.com/zhang0281/cch-geoip/internal/geoip"
	"github.com/zhang0281/cch-geoip/internal/ip2region"
	"github.com/zhang0281/cch-geoip/internal/onlinegeo"
	"github.com/zhang0281/cch-geoip/internal/threat"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		url := "http://127.0.0.1:8080/healthz"
		if len(os.Args) > 2 {
			url = os.Args[2]
		}
		c := http.Client{Timeout: 2 * time.Second}
		r, err := c.Get(url)
		if err != nil {
			os.Exit(1)
		}
		r.Body.Close()
		if r.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(logger); err != nil {
		logger.Error("service stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	c, err := config.Load()
	if err != nil {
		return err
	}
	store := geoip.NewStore(c.CityDB, c.ASNDB, c.MaxDBAge)
	defer store.Close()
	var region *ip2region.Store
	if c.RegionEnabled {
		region = ip2region.NewStore(c.RegionV4DB, c.RegionV6DB)
		defer region.Close()
	}
	reload := func() {
		changed, err := store.Reload()
		if err != nil {
			logger.Warn("database reload failed; retaining last valid readers", "error", err)
			return
		}
		if changed {
			logger.Info("databases loaded", "status", store.Status(time.Now()))
		}
	}
	reload()
	regionReload := func() {
		if region == nil {
			return
		}
		changed, err := region.Reload()
		if err != nil {
			logger.Warn("ip2region reload failed; retaining last valid searchers", "error", err)
			return
		}
		if changed {
			logger.Info("ip2region databases loaded")
		}
	}
	regionReload()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	feeds := threat.NewFeeds(c.FeedsEnabled, c.FeedDir, c.FeedMaxAge)
	feeds.Restore()
	intel := &threat.Service{Feeds: feeds}
	if c.AbuseEnabled && c.AbuseCheckEnabled {
		intel.Abuse = threat.NewAbuseIPDB(threat.AbuseOptions{Key: c.AbuseKey, Timeout: c.AbuseTimeout, CacheTTL: c.AbuseCacheTTL, DailyBudget: c.AbuseDailyBudget})
	}
	if c.AbuseEnabled && c.AbuseBlacklistEnabled {
		intel.Blacklist = threat.NewBlacklist(c.AbuseKey, c.FeedDir, c.FeedMaxAge)
		intel.Blacklist.Restore()
	}
	feedDone := make(chan struct{})
	go func() {
		defer close(feedDone)
		if intel.Blacklist != nil {
			intel.Blacklist.Refresh(ctx)
		}
		feeds.Refresh(ctx)
		timer := time.NewTicker(c.FeedInterval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if intel.Blacklist != nil {
					intel.Blacklist.Refresh(ctx)
				}
				feeds.Refresh(ctx)
			}
		}
	}()
	defer func() { cancel(); <-feedDone }()
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	ticker := time.NewTicker(c.ReloadInterval)
	defer ticker.Stop()
	var regionBackend api.RegionBackend
	if region != nil {
		regionBackend = region
	}
	var onlineBackend api.OnlineBackend
	var onlineService *onlinegeo.Service
	if c.OnlineEnabled {
		online, err := onlinegeo.New(onlinegeo.Options{CacheDir: c.OnlineCacheDir,
			CacheTTL: c.OnlineCacheTTL, Timeout: c.OnlineTimeout,
			IPSBDailyBudget: c.OnlineIPSBBudget, IPWhoDailyBudget: c.OnlineIPWhoBudget, GeoJSDailyBudget: c.OnlineGeoJSBudget})
		if err != nil {
			logger.Warn("online geo state unavailable; using offline databases")
		} else {
			onlineService = online
			onlineBackend = online
		}
	}
	srv := &http.Server{Addr: c.Listen, Handler: api.NewHandlerWithOnline(store, regionBackend, onlineBackend, intel, c.Token, logger),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192}
	result := make(chan error, 1)
	go func() { result <- srv.ListenAndServe() }()
	logger.Info("listening", "address", c.Listen, "authentication_enabled", c.Token != "")
	cacheTicker := time.NewTicker(time.Minute)
	defer cacheTicker.Stop()
	for {
		select {
		case <-ticker.C:
			reload()
			regionReload()
		case <-cacheTicker.C:
			if onlineService != nil {
				if err := onlineService.Maintain(time.Now()); err != nil {
					logger.Warn("online geo cache cleanup failed; new provider requests disabled")
				}
			}
		case <-hup:
			reload()
			regionReload()
		case err := <-result:
			if err == http.ErrServerClosed {
				return nil
			}
			return fmt.Errorf("HTTP server: %w", err)
		case <-ctx.Done():
			shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			return srv.Shutdown(shutdown)
		}
	}
}
