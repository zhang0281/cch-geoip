package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	geo "github.com/oschwald/geoip2-golang"
	"github.com/zhang0281/cch-geoip/internal/geoip"
	"github.com/zhang0281/cch-geoip/internal/ip2region"
	"github.com/zhang0281/cch-geoip/internal/onlinegeo"
)

type geoStub struct {
	record geoip.Record
	err    error
}

func (s geoStub) Lookup(net.IP, time.Time) (geoip.Record, error) { return s.record, s.err }
func (s geoStub) Status(time.Time) geoip.Status                  { return geoip.Status{Ready: s.err == nil} }

type regionStub struct{ record ip2region.Record }

func (s regionStub) Lookup(netip.Addr) (ip2region.Record, error) { return s.record, nil }

type onlineStub struct {
	calls         int
	domesticCalls int
	result        onlinegeo.Result
}

func (s *onlineStub) LookupDomestic(context.Context, netip.Addr, time.Time) onlinegeo.Result {
	s.domesticCalls++
	return s.result
}

func (s *onlineStub) Lookup(context.Context, netip.Addr, time.Time) onlinegeo.Result {
	s.calls++
	return s.result
}

func fixtureRecord(country string) geoip.Record {
	c := &geo.City{}
	c.Country.IsoCode = country
	c.Country.Names = map[string]string{"en": "Offline Country"}
	c.City.Names = map[string]string{"en": "Offline City"}
	c.Postal.Code = "Offline Postal"
	c.Location.TimeZone = "America/Los_Angeles"
	c.Location.Latitude, c.Location.Longitude, c.Location.AccuracyRadius = 34, -118, 1000
	return geoip.Record{City: c, ASN: &geo.ASN{AutonomousSystemNumber: 15169, AutonomousSystemOrganization: "Offline ASN"}}
}

func lookupJSON(t *testing.T, store Backend, region RegionBackend, online OnlineBackend, path string) (int, map[string]any) {
	t.Helper()
	h := NewHandlerWithOnline(store, region, online, nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	var d map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatal(err)
	}
	if w.Code == 200 {
		assertCCH(t, d)
	}
	return w.Code, d
}

func TestDomesticIPSBFailureFallsBackWithoutForeignLookup(t *testing.T) {
	for _, found := range []bool{true, false} {
		online := &onlineStub{}
		region := regionStub{ip2region.Record{Available: true, Found: found, Country: "中国", CountryCode: "CN", Province: "吉林省", City: "长春市"}}
		status, d := lookupJSON(t, geoStub{record: fixtureRecord("CN")}, region, online, "/v1/ip2location/223.104.143.182")
		if status != 200 || online.calls != 0 || online.domesticCalls != 1 {
			t.Fatal("domestic leaked online", status, online.calls)
		}
		want := "Offline City"
		if found {
			want = "长春市"
		}
		if d["location"].(map[string]any)["city"] != want {
			t.Fatal("domestic priority incorrect")
		}
		if d["_meta"].(map[string]any)["online_geo"].(map[string]any)["state"] != "fallback" {
			t.Fatal("domestic metadata incorrect")
		}
	}
}

func TestDomesticIPSBOverridesIP2Region(t *testing.T) {
	region := regionStub{ip2region.Record{Available: true, Found: true, Country: "中国", CountryCode: "CN", Province: "吉林省", City: "长春市"}}
	online := &onlineStub{result: onlinegeo.Result{Source: "ip.sb", Location: &onlinegeo.Location{CountryCode: "CN", Country: "China", Region: "Jilin", City: "Changchun", Timezone: "Asia/Shanghai"}}}
	status, d := lookupJSON(t, geoStub{record: fixtureRecord("CN")}, region, online, "/v1/ip2location/223.104.143.182")
	if status != 200 || online.calls != 0 || online.domesticCalls != 1 || d["location"].(map[string]any)["city"] != "Changchun" {
		t.Fatal("domestic did not prefer IP.SB")
	}
	if d["_meta"].(map[string]any)["online_geo"].(map[string]any)["source"] != "ip.sb" {
		t.Fatal("domestic source metadata incorrect")
	}
}

func TestIP2RegionPrimarySurvivesMissingOrConflictingMMDB(t *testing.T) {
	region := regionStub{ip2region.Record{Available: true, Found: true, Country: "中国", CountryCode: "CN", Province: "吉林省", City: "长春市"}}
	for _, store := range []geoStub{{record: fixtureRecord("US")}, {err: geoip.ErrNotReady}} {
		online := &onlineStub{}
		status, d := lookupJSON(t, store, region, online, "/v1/ip2location/223.104.143.182?lang=zh-CN")
		if status != 200 || online.calls != 0 || online.domesticCalls != 1 {
			t.Fatal("domestic primary incorrectly depends on MMDB")
		}
		l := d["location"].(map[string]any)
		if l["country"].(map[string]any)["code"] != "CN" || l["city"] != "长春市" || d["timezone"].(map[string]any)["id"] != "Asia/Shanghai" {
			t.Fatal("domestic result mixed with conflicting MMDB")
		}
	}
}

func TestForeignUsesCoherentOnlineLocation(t *testing.T) {
	lat, lon := 37.45, 126.7
	online := &onlineStub{result: onlinegeo.Result{Source: "ip.sb", CacheHit: true, UpdatedAt: time.Now(), Location: &onlinegeo.Location{
		CountryCode: "KR", Country: "South Korea", ContinentCode: "AS", Continent: "Asia", Region: "Incheon", City: "Incheon", Postal: "12345", Timezone: "Asia/Seoul", Latitude: &lat, Longitude: &lon, Accuracy: "HIGH",
	}}}
	status, d := lookupJSON(t, geoStub{record: fixtureRecord("US")}, nil, online, "/v1/ip2location/3.5.140.2?lang=zh-CN")
	if status != 200 || online.calls != 1 {
		t.Fatal("foreign lookup failed")
	}
	l := d["location"].(map[string]any)
	if l["city"] != "Incheon" || l["postal_code"] != "12345" || l["country"].(map[string]any)["code"] != "KR" || l["continent"].(map[string]any)["code"] != "AS" {
		t.Fatal("MMDB location mixed with online")
	}
	c := l["coordinates"].(map[string]any)
	if c["latitude"] != lat || c["longitude"] != lon || c["accuracy_radius_km"] != nil {
		t.Fatal("invalid online coordinates or invented radius")
	}
	if d["timezone"].(map[string]any)["id"] != "Asia/Seoul" || d["connection"].(map[string]any)["asn"] != float64(15169) {
		t.Fatal("timezone or offline ASN lost")
	}
	m := d["_meta"].(map[string]any)
	if m["location_accuracy"] != "HIGH" || m["online_geo"].(map[string]any)["state"] != "cache" {
		t.Fatal("source metadata lost")
	}
	if d["threat"].(map[string]any)["score"] != float64(0) {
		t.Fatal("geo provider changed threat")
	}
}

func TestOnlineWithoutCoordinatesClearsMMDBCoordinates(t *testing.T) {
	online := &onlineStub{result: onlinegeo.Result{Source: "ipwho.is", Location: &onlinegeo.Location{CountryCode: "FR", Country: "France", City: "Paris"}}}
	_, d := lookupJSON(t, geoStub{record: fixtureRecord("US")}, nil, online, "/v1/ip2location/3.5.140.2")
	if d["location"].(map[string]any)["coordinates"].(map[string]any)["latitude"] != float64(0) || d["_meta"].(map[string]any)["coordinates_available"] != false || d["timezone"].(map[string]any)["id"] != "" {
		t.Fatal("offline location leaked into incomplete online result")
	}
}

func TestGeoJSAccuracyRadiusUsesProviderKilometers(t *testing.T) {
	lat, lon, radius := 37.4585, 126.7015, 20.0
	online := &onlineStub{result: onlinegeo.Result{Source: "geojs", Location: &onlinegeo.Location{
		CountryCode: "KR", Country: "South Korea", City: "Incheon", Latitude: &lat, Longitude: &lon, AccuracyRadiusKM: &radius,
	}}}
	_, d := lookupJSON(t, geoStub{record: fixtureRecord("US")}, nil, online, "/v1/ip2location/3.5.140.2")
	coords := d["location"].(map[string]any)["coordinates"].(map[string]any)
	if coords["accuracy_radius_km"] != radius {
		t.Fatal("GeoJS accuracy was lost or replaced by MMDB")
	}
}

func TestOnlineFailureFallsBackAndMMDBFailureDoesNotPreventOnline(t *testing.T) {
	for _, broken := range []bool{false, true} {
		s := geoStub{record: fixtureRecord("US")}
		if broken {
			s.err = geoip.ErrNotReady
		}
		online := &onlineStub{}
		status, d := lookupJSON(t, s, nil, online, "/v1/ip2location/3.5.140.2")
		if broken && status != 503 {
			t.Fatal("missing all sources must fail")
		}
		if !broken && (status != 200 || d["location"].(map[string]any)["city"] != "Offline City") {
			t.Fatal("offline fallback lost")
		}
	}
	online := &onlineStub{result: onlinegeo.Result{Source: "ipwho.is", Location: &onlinegeo.Location{CountryCode: "FR", Country: "France", City: "Paris"}}}
	status, d := lookupJSON(t, geoStub{err: geoip.ErrNotReady}, nil, online, "/v1/ip2location/3.5.140.2")
	if status != 200 || d["location"].(map[string]any)["city"] != "Paris" || d["connection"].(map[string]any)["asn"] != nil {
		t.Fatal("online incorrectly requires available MMDB")
	}
	status, d = lookupJSON(t, geoStub{err: io.ErrUnexpectedEOF}, nil, online, "/v1/ip2location/3.5.140.2")
	if status != 200 || d["location"].(map[string]any)["city"] != "Paris" {
		t.Fatal("online cannot rescue offline decode error")
	}
}

func TestInvalidAndUnauthorizedRequestsNeverCallOnline(t *testing.T) {
	online := &onlineStub{}
	h := NewHandlerWithOnline(geoStub{record: fixtureRecord("US")}, nil, online, nil, "fixture-token", slog.Default())
	for _, path := range []string{"/v1/ip2location/3.5.140.2", "/v1/ip2location/127.0.0.1", "/v1/ip2location/hostname", "/v1/ip2location/3.5.140.2?lang=invalid"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", path, nil)
		if path != "/v1/ip2location/3.5.140.2" {
			r.Header.Set("Authorization", "Bearer fixture-token")
		}
		h.ServeHTTP(w, r)
		if w.Code == 200 || online.calls != 0 {
			t.Fatal("invalid request reached upstream")
		}
	}
}
