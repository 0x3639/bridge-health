// Package wire validates the pinned v1 contracts before telemetry is persisted or sent.
package wire

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"time"
	"unicode/utf8"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const MaxBody = 64 << 10
const MaxSequence = 1<<53 - 1

// LocalReadHeader is optional transport metadata, measured by the reporter in microseconds.
const LocalReadHeader = "Bridge-Health-Read-Micros"
const Path = "/v1/heartbeats"
const MaxAge = 2 * time.Minute
const FutureSkew = 30 * time.Second

//go:embed schemas/*.schema.json
var schemas embed.FS
var uuid = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func StreamID(s string) bool { return uuid.MatchString(s) }

type Report struct {
	Version     int             `json:"schemaVersion"`
	Stream      string          `json:"streamId"`
	Sequence    uint64          `json:"sequence"`
	SentAt      time.Time       `json:"sentAt"`
	LocalRead   string          `json:"localRead"`
	LastSuccess *time.Time      `json:"lastLocalSuccessAt"`
	Snapshot    json.RawMessage `json:"snapshot"`
}
type Contracts struct{ snapshot, heartbeat *jsonschema.Schema }

func Compile() (*Contracts, error) {
	c := jsonschema.NewCompiler()
	c.AssertFormat = true
	out := new(Contracts)
	for _, v := range []struct {
		name   string
		target **jsonschema.Schema
	}{{"snapshot", &out.snapshot}, {"heartbeat", &out.heartbeat}} {
		id := "urn:zenon:orchestrator:health:" + v.name + ":v1"
		b, err := schemas.ReadFile("schemas/" + v.name + ".schema.json")
		if err != nil {
			return nil, err
		}
		if err = c.AddResource(id, bytes.NewReader(b)); err != nil {
			return nil, err
		}
		*v.target, err = c.Compile(id)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// StrictJSON rejects duplicate object members anywhere in a document, trailing
// values, and excessively deep nesting before decoding into a Go struct.
func StrictJSON(b []byte, dst any) error {
	if !utf8.Valid(b) {
		return errors.New("invalid UTF-8")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	if err := consume(d, 0); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("trailing data")
	}
	d = json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(dst)
}
func consume(d *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON nesting exceeds limit")
	}
	t, err := d.Token()
	if err != nil {
		return err
	}
	// v1 only uses nonnegative, interoperable JSON integers. Check the
	// representation before the schema validator processes numeric constraints.
	if num, ok := t.(json.Number); ok {
		n, err := num.Int64()
		if err != nil || n < 0 || n > MaxSequence {
			return errors.New("integer outside v1 range")
		}
	}
	switch t {
	case json.Delim('{'):
		keys := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || keys[name] {
				return errors.New("invalid or duplicate JSON member")
			}
			keys[name] = true
			if err = consume(d, depth+1); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for d.More() {
			if err = consume(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = d.Token()
	return err
}

func (c *Contracts) Snapshot(b []byte) error {
	var doc any
	if err := StrictJSON(b, &doc); err != nil {
		return err
	}
	if err := c.snapshot.Validate(doc); err != nil {
		return err
	}
	return uniqueChains(doc)
}
func uniqueChains(doc any) error {
	networks := doc.(map[string]any)["networks"].([]any)
	if len(networks) > 64 {
		return errors.New("too many networks for heartbeat")
	}
	seen := map[uint32]bool{}
	for _, row := range networks {
		// The schema bounds the integer to uint32.
		raw := row.(map[string]any)["chainId"].(json.Number)
		n, err := raw.Int64()
		if err != nil {
			return errors.New("chain ID must be encoded as a decimal integer")
		}
		id := uint32(n)
		if seen[id] {
			return errors.New("duplicate chain ID")
		}
		seen[id] = true
	}
	return nil
}

func (c *Contracts) Report(b []byte) (Report, error) {
	var r Report
	if len(b) > MaxBody {
		return r, errors.New("heartbeat exceeds size limit")
	}
	var doc any
	if err := StrictJSON(b, &doc); err != nil {
		return r, err
	}
	if err := c.heartbeat.Validate(doc); err != nil {
		return r, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, err
	}
	if !StreamID(r.Stream) {
		return r, errors.New("stream ID must be a lowercase UUID")
	}
	if r.LastSuccess != nil && r.LastSuccess.After(r.SentAt) {
		return r, errors.New("local success time exceeds send time")
	}
	if r.LocalRead == "ok" {
		snap := doc.(map[string]any)["snapshot"]
		if err := uniqueChains(snap); err != nil {
			return r, err
		}
		var ts struct {
			SampledAt time.Time
			StartedAt time.Time
		}
		if err := json.Unmarshal(r.Snapshot, &ts); err != nil {
			return r, err
		}
		if ts.StartedAt.After(ts.SampledAt) || ts.SampledAt.After(*r.LastSuccess) || r.SentAt.Sub(ts.SampledAt) > MaxAge {
			return r, errors.New("inconsistent snapshot time")
		}
		if err := checkObservationTimes(snap, ts.SampledAt); err != nil {
			return r, err
		}
	}
	return r, nil
}

// Walk the already validated snapshot. Old measurements keep their original
// times. An observation made after this same-host snapshot is inconsistent.
func checkObservationTimes(value any, sampledAt time.Time) error {
	switch v := value.(type) {
	case []any:
		for _, row := range v {
			if err := checkObservationTimes(row, sampledAt); err != nil {
				return err
			}
		}
	case map[string]any:
		for key, child := range v {
			switch key {
			case "asOf", "lastRangeAt":
				if child != nil {
					stamp, err := time.Parse(time.RFC3339Nano, child.(string))
					if err != nil || stamp.After(sampledAt) {
						return errors.New("observation time exceeds snapshot time")
					}
				}
			case "oldestPendingSince":
				m := child.(map[string]any)
				if m["value"] != nil {
					since, err := time.Parse(time.RFC3339Nano, m["value"].(string))
					if err != nil {
						return err
					}
					asOf, err := time.Parse(time.RFC3339Nano, m["asOf"].(string))
					if err != nil || since.After(asOf) {
						return errors.New("pending time exceeds observation time")
					}
				}
			}
			if err := checkObservationTimes(child, sampledAt); err != nil {
				return err
			}
		}
	}
	return nil
}
