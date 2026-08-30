//go:build fuzz

package config

import "testing"

func FuzzParseDuration(f *testing.F) {
	for _, seed := range []string{"30d", "1h30m", "garbage", "0d", "999999999999999999999d"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) { _, _ = parseDuration(input) })
}

func FuzzParseYAML(f *testing.F) {
	f.Add([]byte("instances: []"))
	f.Add([]byte("{bad"))
	f.Add([]byte("instances:\n - type: sonarr"))
	f.Fuzz(func(t *testing.T, input []byte) { _, _ = parse(input) })
}
