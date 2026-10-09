package threat

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

func (f *feed) parse(data []byte) error {
	if len(data) == 0 || len(data) > maxFeedBytes {
		return fmt.Errorf("invalid feed size")
	}
	if f.kind == "tor" {
		return f.parseTor(data)
	}
	prefixes := []netip.Prefix{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 4096), maxFeedBytes)
	metadata := false
	published := time.Time{}
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var row struct {
			CIDR      string `json:"cidr"`
			Type      string `json:"type"`
			Copyright string `json:"copyright"`
			Timestamp int64  `json:"timestamp"`
		}
		if err := json.Unmarshal(line, &row); err != nil {
			return err
		}
		if row.CIDR == "" {
			if row.Copyright == "" || row.Timestamp <= 0 {
				return fmt.Errorf("missing DROP metadata")
			}
			metadata = true
			published = time.Unix(row.Timestamp, 0).UTC()
			if published.After(time.Now().Add(5 * time.Minute)) {
				return fmt.Errorf("future DROP timestamp")
			}
			continue
		}
		p, err := netip.ParsePrefix(row.CIDR)
		if err != nil || p.Bits() == 0 || (f.name == "spamhaus_drop_v4" && !p.Addr().Is4()) || (f.name == "spamhaus_drop_v6" && !p.Addr().Is6()) {
			return fmt.Errorf("invalid DROP prefix")
		}
		prefixes = append(prefixes, p.Masked())
	}
	if scanner.Err() != nil {
		return scanner.Err()
	}
	if len(prefixes) == 0 || !metadata {
		return fmt.Errorf("empty or incomplete DROP feed")
	}
	f.prefixes = prefixes
	f.published = published
	f.data = append([]byte(nil), data...)
	return nil
}

func (f *feed) parseTor(data []byte) error {
	exits := map[netip.Addr]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		ip, err := netip.ParseAddr(line)
		if err != nil || ip.Zone() != "" || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			return fmt.Errorf("invalid Tor IP")
		}
		exits[ip.Unmap()] = true
	}
	if len(exits) == 0 {
		return fmt.Errorf("empty Tor feed")
	}
	f.exits = exits
	f.data = append([]byte(nil), data...)
	return nil
}
