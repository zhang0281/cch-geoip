package threat

import (
	"os"
	"testing"
)

func TestExternalFeedSnapshots(t *testing.T) {
	tested := false
	for _, tc := range []struct{ env, name, kind string }{
		{"CCH_TEST_DROP_V4", "spamhaus_drop_v4", "drop"},
		{"CCH_TEST_DROP_V6", "spamhaus_drop_v6", "drop"},
		{"CCH_TEST_TOR", "tor_exit", "tor"},
	} {
		path := os.Getenv(tc.env)
		if path == "" {
			continue
		}
		tested = true
		t.Run(tc.name, func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			f := feed{name: tc.name, kind: tc.kind}
			if err = f.parse(b); err != nil {
				t.Fatal(err)
			}
			if tc.kind == "drop" {
				t.Logf("validated %d prefixes", len(f.prefixes))
			} else {
				t.Logf("validated %d exit IPs", len(f.exits))
			}
		})
	}
	if !tested {
		t.Skip("optional already-downloaded upstream data")
	}
}
