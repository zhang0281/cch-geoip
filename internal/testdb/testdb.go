// Package testdb generates synthetic MMDB fixtures; it contains no real MaxMind data.
package testdb

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func Write(dir, cityName string, built time.Time) (string, string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", "", err
	}
	city := mmdbtype.Map{
		"country":   mmdbtype.Map{"iso_code": mmdbtype.String("US"), "names": mmdbtype.Map{"en": mmdbtype.String("United States"), "zh-CN": mmdbtype.String("美国")}},
		"continent": mmdbtype.Map{"code": mmdbtype.String("NA"), "names": mmdbtype.Map{"en": mmdbtype.String("North America")}},
		"city":      mmdbtype.Map{"names": mmdbtype.Map{"en": mmdbtype.String(cityName)}},
		"location":  mmdbtype.Map{"latitude": mmdbtype.Float64(37.4), "longitude": mmdbtype.Float64(-122.1), "accuracy_radius": mmdbtype.Uint16(20), "time_zone": mmdbtype.String("America/Los_Angeles")},
	}
	asn := mmdbtype.Map{"autonomous_system_number": mmdbtype.Uint32(15169), "autonomous_system_organization": mmdbtype.String("Example ASN")}
	cp, ap := filepath.Join(dir, "GeoLite2-City.mmdb"), filepath.Join(dir, "GeoLite2-ASN.mmdb")
	if err := writeOne(cp, "GeoLite2-City", city, built); err != nil {
		return "", "", err
	}
	if err := writeOne(ap, "GeoLite2-ASN", asn, built); err != nil {
		return "", "", err
	}
	return cp, ap, nil
}

func writeOne(path, kind string, record mmdbtype.Map, built time.Time) error {
	tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: kind, BuildEpoch: built.Unix(), IPVersion: 6, Languages: []string{"en", "zh-CN"}})
	if err != nil {
		return err
	}
	for _, cidr := range []string{"8.8.8.0/24", "2001:4860:4860::/48"} {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			return err
		}
		if err = tree.Insert(network, record); err != nil {
			return err
		}
	}
	f, err := os.CreateTemp(filepath.Dir(path), "fixture-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = tree.WriteTo(f); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(f.Name(), path); err != nil {
		return fmt.Errorf("publish fixture: %w", err)
	}
	return nil
}
