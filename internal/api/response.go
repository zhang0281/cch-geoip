package api

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/zhang0281/cch-geoip/internal/geoip"
	"github.com/zhang0281/cch-geoip/internal/ip2region"
)

type object = map[string]any

func localized(names map[string]string, lang string) string {
	if s := names[lang]; s != "" {
		return s
	}
	return names["en"]
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func flag(code string) object {
	emoji, unicode := "", ""
	if len(code) == 2 && code != "ZZ" && code[0] >= 'A' && code[0] <= 'Z' && code[1] >= 'A' && code[1] <= 'Z' {
		a, b := rune(0x1F1E6)+rune(code[0]-'A'), rune(0x1F1E6)+rune(code[1]-'A')
		emoji = string([]rune{a, b})
		unicode = fmt.Sprintf("U+%X U+%X", a, b)
	}
	return object{"emoji": emoji, "unicode": unicode, "svg": nil, "png": nil}
}

func timezone(id string, now time.Time) object {
	x := object{"id": id, "name": id, "abbreviation": "", "utc_offset": "", "utc_offset_seconds": 0, "is_dst": false, "current_time": ""}
	if id == "" {
		return x
	}
	z, err := time.LoadLocation(id)
	if err != nil {
		x["id"] = ""
		x["name"] = ""
		return x
	}
	local := now.In(z)
	name, offset := local.Zone()
	sign := "+"
	n := offset
	if n < 0 {
		sign = "-"
		n = -n
	}
	x["abbreviation"] = name
	x["utc_offset"] = fmt.Sprintf("%s%02d:%02d", sign, n/3600, n%3600/60)
	x["utc_offset_seconds"] = offset
	x["is_dst"] = local.IsDST()
	x["current_time"] = local.Format(time.RFC3339)
	return x
}

func response(ip netip.Addr, lang string, r geoip.Record, now time.Time) object {
	return responseWithRegion(ip, lang, r, ip2region.Record{}, now)
}

func responseWithRegion(ip netip.Addr, lang string, r geoip.Record, region ip2region.Record, now time.Time) object {
	c, a := r.City, r.ASN
	code := strings.ToUpper(c.Country.IsoCode)
	if code == "" {
		code = "ZZ"
	}
	name := localized(c.Country.Names, lang)
	if name == "" {
		name = "Unknown"
	}
	country := object{"code": code, "code3": "", "name": name, "name_native": "", "capital": nil,
		"calling_code": "+0", "tld": ".unknown", "area_km2": 0, "population": 0,
		"borders": []string{}, "is_eu_member": c.Country.IsInEuropeanUnion, "flag": flag(code),
		"languages": []any{}, "currencies": []any{}}
	var regionValue any
	if len(c.Subdivisions) > 0 {
		s := c.Subdivisions[0]
		regionValue = object{"code": s.IsoCode, "name": localized(s.Names, lang), "type": "maxmind"}
	}
	regionMatch := region.Found && region.CountryCode == "CN" && region.Country == "中国" && code == "CN"
	useIP2RegionLocality := regionMatch && (region.Province != "" || region.City != "")
	useIP2RegionISP := regionMatch && region.ISP != ""
	if useIP2RegionLocality {
		regionValue = nil
		if region.Province != "" {
			regionValue = object{"code": "", "name": region.Province, "type": "ip2region"}
		}
	}
	var asn any
	if a.AutonomousSystemNumber != 0 {
		asn = a.AutonomousSystemNumber
	}
	version := "ipv6"
	if ip.Is4() {
		version = "ipv4"
	}
	var accuracy any
	if c.Location.AccuracyRadius != 0 {
		accuracy = c.Location.AccuracyRadius
	}
	coordinates := object{"latitude": c.Location.Latitude, "longitude": c.Location.Longitude, "accuracy_radius_km": accuracy}
	coordinatesAvailable := c.Location.AccuracyRadius > 0
	if useIP2RegionLocality {
		// ip2region supplies a more useful CN locality but no city centroid.
		// Do not display a MaxMind country-level coordinate beside it as if it
		// were the ip2region city's coordinate.
		coordinates = object{"latitude": 0.0, "longitude": 0.0, "accuracy_radius_km": nil}
		coordinatesAvailable = false
	}
	// CCH requires boolean/numeric threat fields. These are schema placeholders,
	// not negative intelligence; _meta explicitly declares their unsupported status.
	city := nullable(localized(c.City.Names, lang))
	if useIP2RegionLocality {
		city = nullable(region.City)
	}
	org := nullable(a.AutonomousSystemOrganization)
	var carrier any
	if useIP2RegionISP {
		if name := mobileCarrier(region.ISP); name != "" {
			carrier = object{"name": name, "mcc": nil, "mnc": nil}
		}
	}
	postalCode := nullable(c.Postal.Code)
	if useIP2RegionLocality {
		postalCode = nil
	}
	unsupported := []string{"privacy.is_vpn", "privacy.is_proxy", "privacy.is_relay", "threat.is_attacker", "threat.is_crawler", "company", "carrier", "hosting", "abuse", "connection.is_anycast", "connection.route", "country.code3", "country.demographics"}
	if carrier != nil {
		unsupported = removeString(unsupported, "carrier")
	}
	return object{
		"ip": ip.String(), "version": version, "hostname": nil,
		"location": object{"continent": object{"code": c.Continent.Code, "name": localized(c.Continent.Names, lang)},
			"country": country, "region": regionValue, "city": city,
			"postal_code": postalCode, "coordinates": coordinates},
		"timezone": timezone(c.Location.TimeZone, now),
		"connection": object{"asn": asn, "handle": nil, "organization": org, "isp": nullable(usedISP(useIP2RegionISP, region.ISP)),
			"domain": nil, "route": nil, "rir": "UNKNOWN", "type": "unknown", "subtype": nil,
			"scope": "public", "is_anycast": false},
		"company": object{"name": nil, "domain": nil, "type": "unknown"}, "carrier": carrier, "hosting": nil,
		"privacy": object{"is_proxy": false, "is_vpn": false, "is_tor": false, "is_tor_exit": false, "is_relay": false, "is_anonymous": false},
		"threat": object{"is_abuser": false, "is_attacker": false, "is_crawler": false, "is_threat": false,
			"score": 0, "risk_level": "none", "blocklists": []any{}}, "abuse": nil,
		"_meta": object{"source": "MaxMind GeoLite2 / GeoIP2 City + GeoLite2 ASN", "language": lang,
			"location_source":       map[string]any{"country": "maxmind", "region": sourceName(useIP2RegionLocality), "city": sourceName(useIP2RegionLocality), "isp": sourceName(useIP2RegionISP), "coordinates": coordinateSource(useIP2RegionLocality)},
			"ip2region":             object{"state": regionState(region.Available), "version": region.Version, "matched": regionMatch, "country": nullable(region.Country), "province": nullable(region.Province), "city": nullable(region.City), "isp": nullable(region.ISP), "country_code": nullable(region.CountryCode)},
			"unsupported":           unsupported,
			"warning":               "Privacy/threat flags are compatibility placeholders, not verified negatives; CCH currently ignores this metadata.",
			"coordinates_available": coordinatesAvailable, "coordinate_source": coordinateSource(useIP2RegionLocality)},
	}
}

func removeString(values []string, target string) []string {
	for i, value := range values {
		if value == target {
			return append(values[:i], values[i+1:]...)
		}
	}
	return values
}

func sourceName(used bool) string {
	if used {
		return "ip2region"
	}
	return "maxmind"
}

func coordinateSource(used bool) string {
	if used {
		return "unavailable-ip2region-has-no-city-centroid"
	}
	return "maxmind"
}

func regionState(available bool) string {
	if available {
		return "ready"
	}
	return "unavailable"
}

func mobileCarrier(isp string) string {
	switch strings.TrimSpace(isp) {
	case "移动", "中国移动":
		return "中国移动"
	case "联通", "中国联通":
		return "中国联通"
	case "电信", "中国电信":
		return "中国电信"
	case "广电", "中国广电":
		return "中国广电"
	default:
		return ""
	}
}

func usedISP(use bool, isp string) string {
	if !use {
		return ""
	}
	return isp
}
