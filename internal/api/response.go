package api

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	"github.com/zhang0281/cch-geoip/internal/geoip"
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
	var region any
	if len(c.Subdivisions) > 0 {
		s := c.Subdivisions[0]
		region = object{"code": s.IsoCode, "name": localized(s.Names, lang), "type": ""}
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
	// CCH requires boolean/numeric threat fields. These are schema placeholders,
	// not negative intelligence; _meta explicitly declares their unsupported status.
	return object{
		"ip": ip.String(), "version": version, "hostname": nil,
		"location": object{"continent": object{"code": c.Continent.Code, "name": localized(c.Continent.Names, lang)},
			"country": country, "region": region, "city": nullable(localized(c.City.Names, lang)),
			"postal_code": nullable(c.Postal.Code), "coordinates": object{"latitude": c.Location.Latitude,
				"longitude": c.Location.Longitude, "accuracy_radius_km": accuracy}},
		"timezone": timezone(c.Location.TimeZone, now),
		"connection": object{"asn": asn, "handle": nil, "organization": nullable(a.AutonomousSystemOrganization),
			"domain": nil, "route": nil, "rir": "UNKNOWN", "type": "unknown", "subtype": nil,
			"scope": "public", "is_anycast": false},
		"company": object{"name": nil, "domain": nil, "type": "unknown"}, "carrier": nil, "hosting": nil,
		"privacy": object{"is_proxy": false, "is_vpn": false, "is_tor": false, "is_tor_exit": false, "is_relay": false, "is_anonymous": false},
		"threat": object{"is_abuser": false, "is_attacker": false, "is_crawler": false, "is_threat": false,
			"score": 0, "risk_level": "none", "blocklists": []any{}}, "abuse": nil,
		"_meta": object{"source": "MaxMind GeoLite2 / GeoIP2 City + GeoLite2 ASN", "language": lang,
			"unsupported":           []string{"privacy", "threat", "company", "carrier", "hosting", "abuse", "connection.is_anycast", "connection.route", "country.code3", "country.demographics"},
			"warning":               "Privacy/threat flags are compatibility placeholders, not verified negatives; CCH currently ignores this metadata.",
			"coordinates_available": c.Location.AccuracyRadius > 0},
	}
}
