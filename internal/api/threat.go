package api

import "github.com/zhang0281/cch-geoip/internal/threat"

func applyThreat(data object, r threat.Result) {
	p := data["privacy"].(object)
	p["is_tor_exit"] = r.TorExit
	p["is_tor"] = r.TorExit
	p["is_anonymous"] = r.TorExit
	t := data["threat"].(object)
	t["is_threat"] = r.IsThreat
	t["is_abuser"] = r.IsAbuser
	t["score"] = r.Score
	t["risk_level"] = r.RiskLevel
	t["blocklists"] = r.Blocklists
	m := data["_meta"].(object)
	m["threat_sources"] = r.Sources
	m["score_semantics"] = "Maximum of local DROP policy score (1 on match) and AbuseIPDB abuse confidence / 100; not attack probability."
	m["unsupported"] = []string{"privacy.is_vpn", "privacy.is_proxy", "privacy.is_relay", "threat.is_attacker", "threat.is_crawler", "company", "carrier", "hosting", "abuse", "connection.is_anycast", "connection.route", "country.code3", "country.demographics"}
	m["warning"] = "Unavailable fields use CCH-compatible placeholders. A zero score or non-match is not proof of safety; current CCH ignores _meta."
}
