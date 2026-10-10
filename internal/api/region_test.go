package api

import (
	"net/netip"
	"testing"
	"time"

	geo "github.com/oschwald/geoip2-golang"
	"github.com/zhang0281/cch-geoip/internal/geoip"
	"github.com/zhang0281/cch-geoip/internal/ip2region"
)

func TestResponseUsesIp2RegionLocalityWithoutMixingCoordinates(t *testing.T) {
	city := &geo.City{}
	city.Country.IsoCode = "CN"
	city.Country.Names = map[string]string{"zh-CN": "中国", "en": "China"}
	city.Continent.Code = "AS"
	city.Continent.Names = map[string]string{"zh-CN": "亚洲", "en": "Asia"}
	city.Location.TimeZone = "Asia/Shanghai"
	city.Location.Latitude, city.Location.Longitude, city.Location.AccuracyRadius = 35, 113, 1000
	city.City.Names = map[string]string{"zh-CN": "Test City", "en": "Test City"}
	record := geoip.Record{City: city, ASN: &geo.ASN{AutonomousSystemNumber: 134810, AutonomousSystemOrganization: "China Mobile"}}
	region := ip2region.Record{Available: true, Found: true, Country: "中国", Province: "吉林省", City: "长春市", ISP: "移动", CountryCode: "CN", Version: "v3.18.0"}
	d := responseWithRegion(netip.MustParseAddr("223.104.143.182"), "zh-CN", record, region, time.Now())
	location := d["location"].(object)
	if location["city"] != "长春市" || location["region"].(object)["name"] != "吉林省" {
		t.Fatalf("ip2region locality missing: %#v", location)
	}
	coordinates := location["coordinates"].(object)
	if coordinates["latitude"] != 0.0 || coordinates["longitude"] != 0.0 || coordinates["accuracy_radius_km"] != nil {
		t.Fatalf("coordinates mixed with ip2region city: %#v", coordinates)
	}
	if d["carrier"].(object)["name"] != "中国移动" {
		t.Fatalf("carrier missing: %#v", d["carrier"])
	}
	if d["connection"].(object)["organization"] != "China Mobile" {
		t.Fatal("ASN organization changed")
	}
	meta := d["_meta"].(object)
	if meta["ip2region"].(object)["version"] != "v3.18.0" || meta["coordinates_available"] != false {
		t.Fatalf("source metadata incorrect: %#v", meta)
	}
	foreign := region
	foreign.Country, foreign.CountryCode, foreign.City = "United States", "US", "Wrong City"
	d = responseWithRegion(netip.MustParseAddr("8.8.8.8"), "en", record, foreign, time.Now())
	if d["location"].(object)["city"] != "Test City" {
		t.Fatal("foreign record overrode MaxMind")
	}
}
