package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/zhang0281/cch-geoip/internal/geoip"
	"github.com/zhang0281/cch-geoip/internal/threat"
)

type Backend interface {
	Lookup(net.IP, time.Time) (geoip.Record, error)
	Status(time.Time) geoip.Status
}

func NewHandler(store Backend, intel *threat.Service, token string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { write(w, 200, object{"status": "ok"}) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		s := store.Status(time.Now())
		code := 200
		if !s.Ready {
			code = 503
		}
		write(w, code, s)
	})
	mux.HandleFunc("GET /v1/ip2location/{ip}", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(r, token) {
			w.Header().Set("WWW-Authenticate", "Bearer")
			write(w, 401, object{"error": "unauthorized"})
			return
		}
		ip, err := netip.ParseAddr(r.PathValue("ip"))
		if err != nil || ip.Zone() != "" {
			write(w, 400, object{"error": "invalid IP address"})
			return
		}
		ip = ip.Unmap()
		if !publicIP(ip) {
			write(w, 400, object{"error": "non-public IP address"})
			return
		}
		lang, ok := language(r.URL.Query().Get("lang"))
		if !ok {
			write(w, 400, object{"error": "unsupported language"})
			return
		}
		now := time.Now()
		record, err := store.Lookup(net.IP(ip.AsSlice()), now)
		if errors.Is(err, geoip.ErrNotReady) {
			write(w, 503, object{"error": "databases unavailable or stale"})
			return
		}
		if err != nil {
			logger.Error("database lookup failed")
			write(w, 500, object{"error": "database lookup failed"})
			return
		}
		data := response(ip, lang, record, now)
		applyThreat(data, intel.Lookup(r.Context(), ip, now))
		write(w, 200, data)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func authorized(r *http.Request, token string) bool {
	if token == "" {
		return true
	}
	a, b := sha256.Sum256([]byte(r.Header.Get("Authorization"))), sha256.Sum256([]byte("Bearer "+token))
	return subtle.ConstantTimeCompare(a[:], b[:]) == 1
}

func language(s string) (string, bool) {
	if s == "" {
		return "en", true
	}
	s = strings.ToLower(s)
	if s == "zh" || s == "zh-cn" || s == "zh-hans" {
		return "zh-CN", true
	}
	for _, l := range []string{"en", "de", "es", "fr", "ja", "pt-br", "ru"} {
		if s == l {
			if s == "pt-br" {
				return "pt-BR", true
			}
			return l, true
		}
	}
	return "", false
}

var reserved = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("100::/64"),
}

func publicIP(ip netip.Addr) bool {
	if !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, p := range reserved {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

func write(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
