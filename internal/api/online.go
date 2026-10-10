package api

import (
	"time"

	"github.com/zhang0281/cch-geoip/internal/onlinegeo"
)

func applyOnlineGeo(data object, result onlinegeo.Result, domestic, enabled bool, now time.Time) {
	meta := data["_meta"].(object)
	state := "fallback"
	if !enabled {
		state = "disabled"
	}
	if result.Location != nil {
		state = "ready"
		if result.CacheHit {
			state = "cache"
		}
	}
	meta["online_geo"] = object{"state": state, "source": nullable(result.Source), "cache_hit": result.CacheHit,
		"updated_at": nil, "providers": result.Providers, "domestic": domestic}
	if !result.UpdatedAt.IsZero() {
		meta["online_geo"].(object)["updated_at"] = result.UpdatedAt.UTC().Format(time.RFC3339)
	}
	if result.Location == nil {
		return
	}
	l := result.Location
	location := data["location"].(object)
	country := location["country"].(object)
	// Country extras and continent names must not describe the old MMDB country.
	country["code"], country["name"], country["flag"] = l.CountryCode, l.Country, flag(l.CountryCode)
	country["is_eu_member"] = euCountries[l.CountryCode]
	location["continent"] = object{"code": l.ContinentCode, "name": l.Continent}
	location["region"] = nil
	if l.Region != "" {
		location["region"] = object{"code": l.RegionCode, "name": l.Region, "type": result.Source}
	}
	location["city"], location["postal_code"] = nullable(l.City), nullable(l.Postal)
	coords := object{"latitude": 0.0, "longitude": 0.0, "accuracy_radius_km": nil}
	available := l.Latitude != nil && l.Longitude != nil
	if available {
		coords["latitude"], coords["longitude"] = *l.Latitude, *l.Longitude
		if l.AccuracyRadiusKM != nil {
			coords["accuracy_radius_km"] = *l.AccuracyRadiusKM
		}
	}
	location["coordinates"] = coords
	data["timezone"] = timezone(l.Timezone, now)
	meta["coordinates_available"], meta["coordinate_source"] = available, result.Source
	ispSource := meta["location_source"].(object)["isp"]
	meta["location_source"] = object{"country": result.Source, "region": result.Source, "city": result.Source,
		"postal_code": result.Source, "timezone": result.Source, "coordinates": result.Source, "isp": ispSource}
	meta["location_language"] = "en"
	meta["location_accuracy"] = nullable(l.Accuracy)
	meta["source"] = result.Source + " geolocation + MaxMind GeoLite2 ASN"
}

var euCountries = map[string]bool{
	"AT": true, "BE": true, "BG": true, "HR": true, "CY": true, "CZ": true, "DK": true,
	"EE": true, "FI": true, "FR": true, "DE": true, "GR": true, "HU": true, "IE": true,
	"IT": true, "LV": true, "LT": true, "LU": true, "MT": true, "NL": true, "PL": true,
	"PT": true, "RO": true, "SK": true, "SI": true, "ES": true, "SE": true,
}
