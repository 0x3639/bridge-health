package wire

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

func TestPinnedFixturesAndBounds(t *testing.T) {
	c, err := Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"startup", "unreachable", "invalid"} {
		b, err := os.ReadFile("schemas/testdata/" + name + ".heartbeat.json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = c.Report(b); (err != nil) != (name == "invalid") {
			t.Errorf("%s: %v", name, err)
		}
	}
	b, err := os.ReadFile("schemas/testdata/startup.heartbeat.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, from, to string }{
		{"nested duplicate", "\"phase\": \"starting\"", "\"phase\": \"running\",\"phase\": \"starting\""},
		{"time order", "\"sampledAt\": \"2026-11-21T12:00:01Z\"", "\"sampledAt\": \"2026-11-21T12:03:01Z\""},
		{"unknown with numeric value", "\"value\": null", "\"value\": 100"},
		{"leaked field", "\"phase\": \"starting\"", "\"phase\": \"starting\",\"peers\":[]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Report(bytes.Replace(b, []byte(tc.from), []byte(tc.to), 1)); err == nil {
				t.Fatal("accepted invalid report")
			}
		})
	}
	if _, err = c.Report(append(b, bytes.Repeat([]byte(" "), MaxBody)...)); err == nil {
		t.Fatal("accepted oversized body")
	}
}
func TestUniqueNetworks(t *testing.T) {
	row := map[string]any{"chainId": json.Number("1")}
	if err := uniqueChains(map[string]any{"networks": []any{row, row}}); err == nil {
		t.Fatal("accepted duplicate IDs")
	}
}
