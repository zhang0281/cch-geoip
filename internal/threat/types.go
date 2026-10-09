package threat

import (
	"context"
	"net/netip"
	"time"
)

type Source struct {
	State                string     `json:"state"`
	UpdatedAt            *time.Time `json:"updated_at,omitempty"`
	PublishedAt          *time.Time `json:"published_at,omitempty"`
	LastReportedAt       *time.Time `json:"last_reported_at,omitempty"`
	Matched              bool       `json:"matched"`
	AbuseConfidenceScore *int       `json:"abuse_confidence_score,omitempty"`
	Error                string     `json:"error,omitempty"`
}

type Evidence struct {
	Name     string  `json:"name"`
	Category string  `json:"category"`
	ListedAt *string `json:"listed_at"`
}

type Result struct {
	TorExit    bool
	Score      float64
	RiskLevel  string
	IsThreat   bool
	IsAbuser   bool
	Blocklists []Evidence
	Sources    map[string]Source
}

type Service struct {
	Feeds     *Feeds
	Abuse     *AbuseIPDB
	Blacklist *Blacklist
}

func (s *Service) Lookup(ctx context.Context, ip netip.Addr, now time.Time) Result {
	r := Result{RiskLevel: "none", Blocklists: []Evidence{}, Sources: map[string]Source{}}
	if s == nil {
		return r
	}
	if s.Feeds != nil {
		r = s.Feeds.Lookup(ip, now)
	}
	r.Sources["abuseipdb_blacklist"] = Source{State: "disabled"}
	if s.Blacklist != nil {
		local := s.Blacklist.Lookup(ip, now)
		r.Sources["abuseipdb_blacklist"] = local.Source
		if local.Source.State == "ready" && local.Source.Matched {
			r.Sources["abuseipdb"] = local.Source
			r.Sources["abuseipdb_check"] = Source{State: "skipped"}
			r.Blocklists = append(r.Blocklists, Evidence{Name: "AbuseIPDB BLACKLIST", Category: "abuse"})
			applyAbuse(&r, local)
			return r
		}
	}
	if s.Abuse == nil {
		r.Sources["abuseipdb"] = Source{State: "disabled"}
		if s.Blacklist != nil {
			r.Sources["abuseipdb"] = Source{State: "unavailable", Error: "no local match; CHECK disabled"}
		}
		r.Sources["abuseipdb_check"] = Source{State: "disabled"}
		return r
	}
	a := s.Abuse.Lookup(ctx, ip, now)
	r.Sources["abuseipdb"] = a.Source
	r.Sources["abuseipdb_check"] = a.Source
	applyAbuse(&r, a)
	return r
}

func applyAbuse(r *Result, a AbuseResult) {
	if a.Source.State == "ready" {
		score := float64(a.Score) / 100
		if score > r.Score {
			r.Score = score
			r.RiskLevel = riskLevel(score)
		}
		// Reports alone are not proof of a current attacker or a crawler.
		if a.Score >= 75 {
			r.IsAbuser = true
			r.IsThreat = true
		}
	}
}

func riskLevel(score float64) string {
	switch {
	case score >= .9:
		return "critical"
	case score >= .75:
		return "high"
	case score >= .25:
		return "medium"
	case score > 0:
		return "low"
	default:
		return "none"
	}
}
