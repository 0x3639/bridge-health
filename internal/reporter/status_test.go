package reporter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/wire"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLocalReadFailures(t *testing.T) {
	contracts, err := wire.Compile()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		code int
		body string
		err  error
		want string
	}{
		{"method missing", 404, "", nil, "unsupported"},
		{"timeout", 0, "", context.DeadlineExceeded, "timeout"},
		{"invalid envelope", 200, `{"error":"","result":null}`, nil, "invalidResponse"},
		{"unknown field", 200, `{"error":"","result":{},"extra":1}`, nil, "invalidResponse"},
		{"body too large", 200, strings.Repeat(" ", wire.MaxBody+1), nil, "invalidResponse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Reporter{contracts: contracts, local: &http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
				if tc.err != nil {
					return nil, tc.err
				}
				return &http.Response{StatusCode: tc.code, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})}}
			snap, got := r.read(context.Background())
			if got != tc.want || snap != nil {
				t.Fatalf("%s %s", got, snap)
			}
		})
	}
}
func TestRetryAfterDefersSampling(t *testing.T) {
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Header().Set("Retry-After", "120"); w.WriteHeader(429) }))
	defer sink.Close()
	contracts, err := wire.Compile()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	r, err := New(filepath.Join(dir, "missing.sock"), sink.URL+wire.Path, "e71664d3-a7ba-4029-8e17-95a5d0b85537", dir, &tls.Config{}, contracts)
	if err != nil {
		t.Fatal(err)
	}
	r.remote = sink.Client()
	defer r.Close()
	start := time.Now()
	if _, err = r.Once(context.Background()); err == nil {
		t.Fatal("unexpected successful delivery")
	}
	if next := r.NextSend(start); next.Sub(start) < 120*time.Second {
		t.Fatalf("ignored Retry-After: %v", next)
	}
}

func TestReporterReadDuration(t *testing.T) {
	c, err := wire.Compile()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New("/unused", "https://receiver.invalid"+wire.Path, "e71664d3-a7ba-4029-8e17-95a5d0b85537", t.TempDir(), &tls.Config{}, c)
	if err != nil {
		t.Fatal(err)
	}
	fixture, err := os.ReadFile("../wire/schemas/testdata/startup.heartbeat.json")
	if err != nil {
		t.Fatal(err)
	}
	var report wire.Report
	if err = json.Unmarshal([]byte(strings.ReplaceAll(string(fixture), "2026-11-21T12:00:01Z", time.Now().UTC().Format(time.RFC3339Nano))), &report); err != nil {
		t.Fatal(err)
	}
	outcome := http.StatusOK
	r.local.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: outcome, Body: io.NopCloser(strings.NewReader(`{"error":"","result":` + string(report.Snapshot) + `}`))}, nil
	})
	r.remote.Transport = roundTrip(func(req *http.Request) (*http.Response, error) {
		value := req.Header.Get(wire.LocalReadHeader)
		if outcome == 200 {
			if _, err := strconv.ParseUint(value, 10, 32); err != nil {
				t.Errorf("missing successful read timing: %q", value)
			}
		} else if value != "" {
			t.Error("included timing for failed read")
		}
		return &http.Response{StatusCode: 204, Body: http.NoBody}, nil
	})
	for _, status := range []int{200, 404} {
		outcome = status
		if _, err := r.Once(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
}
