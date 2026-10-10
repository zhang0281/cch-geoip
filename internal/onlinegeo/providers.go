package onlinegeo

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const maxResponseBytes = 128 << 10

func (s *Service) fetch(ctx context.Context, provider string, ip netip.Addr, now time.Time) (*Location, string, time.Time) {
	method, url := http.MethodGet, s.endpoints[provider]+"/"+ip.String()
	var body io.Reader
	if provider == ipapi {
		method, url = http.MethodPost, s.endpoints[provider]
		payload, _ := json.Marshal(struct {
			Q   string `json:"q"`
			Key string `json:"key"`
		}{ip.String(), s.options.IPAPIKey})
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, "unavailable", now.Add(time.Minute)
	}
	req.Header.Set("Accept", "application/json")
	if provider == ipapi {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.client.Do(req)
	if err != nil {
		return nil, "unavailable", now.Add(time.Minute)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusTooManyRequests {
		return nil, "cooldown", retryAfter(res.Header.Get("Retry-After"), now)
	}
	if res.StatusCode != http.StatusOK {
		cooldown := time.Minute
		if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
			cooldown = 24 * time.Hour
		}
		return nil, "unavailable", now.Add(cooldown)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes {
		return nil, "invalid_response", now.Add(time.Minute)
	}
	location, ok := parseLocation(provider, b, ip)
	if !ok {
		return nil, "invalid_response", now.Add(time.Minute)
	}
	return location, "ready", time.Time{}
}

func retryAfter(value string, now time.Time) time.Time {
	duration := time.Hour
	if seconds, err := strconv.ParseInt(value, 10, 64); err == nil && seconds >= 0 && seconds <= 86400 {
		duration = time.Duration(seconds) * time.Second
	} else if timestamp, err := http.ParseTime(value); err == nil {
		duration = timestamp.Sub(now)
	}
	if duration < time.Minute {
		duration = time.Minute
	}
	if duration > 24*time.Hour {
		duration = 24 * time.Hour
	}
	return now.Add(duration)
}

func parseLocation(provider string, b []byte, ip netip.Addr) (*Location, bool) {
	var address string
	var l Location
	if provider == ipapi {
		var payload struct {
			IP       string          `json:"ip"`
			Error    json.RawMessage `json:"error"`
			IsBogon  bool            `json:"is_bogon"`
			Location struct {
				CountryCode   string   `json:"country_code"`
				Country       string   `json:"country"`
				ContinentCode string   `json:"continent_code"`
				Continent     string   `json:"continent"`
				State         string   `json:"state"`
				StateCode     string   `json:"state_code"`
				City          string   `json:"city"`
				Zip           string   `json:"zip"`
				Timezone      string   `json:"timezone"`
				Accuracy      string   `json:"accuracy"`
				Latitude      *float64 `json:"latitude"`
				Longitude     *float64 `json:"longitude"`
			} `json:"location"`
		}
		if json.Unmarshal(b, &payload) != nil || payload.IsBogon ||
			(len(payload.Error) > 0 && string(payload.Error) != "null" && string(payload.Error) != "false" && string(payload.Error) != "\"\"") {
			return nil, false
		}
		d := payload.Location
		address = payload.IP
		continentCode, continentName := d.ContinentCode, d.Continent
		if name, ok := continentNames[d.Continent]; ok && (continentCode == "" || continentCode == d.Continent) {
			continentCode, continentName = d.Continent, name
		}
		l = Location{CountryCode: d.CountryCode, Country: d.Country, ContinentCode: d.ContinentCode, Continent: d.Continent,
			Region: d.State, RegionCode: d.StateCode, City: d.City, Postal: d.Zip, Timezone: d.Timezone, Accuracy: d.Accuracy, Latitude: d.Latitude, Longitude: d.Longitude}
		l.ContinentCode, l.Continent = continentCode, continentName
	} else {
		var payload struct {
			IP            string   `json:"ip"`
			Success       *bool    `json:"success"`
			CountryCode   string   `json:"country_code"`
			Country       string   `json:"country"`
			ContinentCode string   `json:"continent_code"`
			Continent     string   `json:"continent"`
			Region        string   `json:"region"`
			RegionCode    string   `json:"region_code"`
			City          string   `json:"city"`
			Postal        string   `json:"postal"`
			Latitude      *float64 `json:"latitude"`
			Longitude     *float64 `json:"longitude"`
			Timezone      struct {
				ID string `json:"id"`
			} `json:"timezone"`
		}
		if json.Unmarshal(b, &payload) != nil || payload.Success == nil || !*payload.Success {
			return nil, false
		}
		address = payload.IP
		l = Location{CountryCode: payload.CountryCode, Country: payload.Country, ContinentCode: payload.ContinentCode, Continent: payload.Continent,
			Region: payload.Region, RegionCode: payload.RegionCode, City: payload.City, Postal: payload.Postal, Timezone: payload.Timezone.ID, Latitude: payload.Latitude, Longitude: payload.Longitude}
	}
	returned, err := netip.ParseAddr(address)
	if err != nil || returned.Unmap() != ip || !validateLocation(&l) {
		return nil, false
	}
	return &l, true
}

func validateLocation(l *Location) bool {
	const countries = " AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW "
	if len(l.CountryCode) != 2 || !strings.Contains(countries, " "+l.CountryCode+" ") || l.Country == "" || (l.Region == "" && l.City == "") {
		return false
	}
	for _, value := range []string{l.Country, l.Continent, l.Region, l.RegionCode, l.City, l.Postal, l.Timezone, l.Accuracy} {
		if len(value) > 512 || strings.TrimSpace(value) != value {
			return false
		}
		for _, r := range value {
			if unicode.IsControl(r) {
				return false
			}
		}
	}
	if l.ContinentCode != "" && !strings.Contains(" AF AN AS EU NA OC SA ", " "+l.ContinentCode+" ") {
		return false
	}
	if l.Timezone != "" {
		if l.Timezone != "UTC" && !strings.Contains(l.Timezone, "/") {
			return false
		}
		if _, err := time.LoadLocation(l.Timezone); err != nil {
			return false
		}
	}
	if (l.Latitude == nil) != (l.Longitude == nil) {
		return false
	}
	if l.Latitude != nil {
		lat, lon := *l.Latitude, *l.Longitude
		if math.IsNaN(lat) || math.IsInf(lat, 0) || math.IsNaN(lon) || math.IsInf(lon, 0) || math.Abs(lat) > 90 || math.Abs(lon) > 180 {
			return false
		}
		if lat == 0 && lon == 0 {
			l.Latitude = nil
			l.Longitude = nil
		}
	}
	return true
}

func publicAddress(ip netip.Addr) bool {
	if !ip.IsValid() || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range reservedPrefixes {
		if prefix.Contains(ip) {
			return false
		}
	}
	return true
}

var reservedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"), netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2001:2::/48"), netip.MustParsePrefix("100::/64"),
}

var continentNames = map[string]string{
	"AF": "Africa", "AN": "Antarctica", "AS": "Asia", "EU": "Europe",
	"NA": "North America", "OC": "Oceania", "SA": "South America",
}
