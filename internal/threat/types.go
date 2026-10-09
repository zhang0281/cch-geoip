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
	Feeds *Feeds
	Abuse *AbuseIPDB
}

func (s *Service) Lookup(ctx context.Context, ip netip.Addr, now time.Time) Result {
	r := Result{RiskLevel: "none", Blocklists: []Evidence{}, Sources: map[string]Source{}}
	if s == nil {
		return r
	}
	if s.Feeds != nil {
		r = s.Feeds.Lookup(ip, now)
	}
	if s.Abuse == nil {
		r.Sources["abuseipdb"] = Source{State: "disabled"}
		return r
	}
	a := s.Abuse.Lookup(ctx, ip, now)
	r.Sources["abuseipdb"] = a.Source
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
	return r
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
