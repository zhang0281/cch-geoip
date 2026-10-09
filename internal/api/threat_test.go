package api

import (
	"encoding/json"
	"testing"

	"github.com/zhang0281/cch-geoip/internal/threat"
)

func TestThreatUnknownDatesAreNull(t *testing.T) {
	d := object{"privacy": object{}, "threat": object{}, "_meta": object{}}
	applyThreat(d, threat.Result{TorExit: true, IsThreat: true, Score: 1, RiskLevel: "critical",
		Blocklists: []threat.Evidence{{Name: "Spamhaus DROP", Category: "abuse"}},
		Sources:    map[string]threat.Source{"abuseipdb": {State: "unavailable"}}})
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(b, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["privacy"].(map[string]any)["is_tor_exit"] != true {
		t.Fatal("privacy mapping")
	}
	if decoded["threat"].(map[string]any)["score"] != float64(1) {
		t.Fatal("threat mapping")
	}
	entry := decoded["threat"].(map[string]any)["blocklists"].([]any)[0].(map[string]any)
	date, exists := entry["listed_at"]
	if !exists || date != nil {
		t.Fatal("unknown date must be null; an empty string crashes CCH RelativeTime", entry)
	}
}
