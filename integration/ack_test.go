package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/reporter"
	"github.com/0x3639/bridge-health/internal/transport"
	"github.com/0x3639/bridge-health/internal/wire"
)

// Drop the HTTP connection after the first durable acceptance, before the
// reporter can see its acknowledgment. The second request must leave receipt
// time and sequence intact.
func TestLostAcknowledgment(t *testing.T) {
	dir := t.TempDir()
	issuers := makeCA(t, dir, "issuers")
	servers := makeCA(t, dir, "servers")
	op := issuers.issue(t, dir, "reporter", false)
	host := servers.issue(t, dir, "collector", true)
	b, err := json.Marshal(map[string]any{"enrollments": []collector.Enrollment{{Node: "pilot", Stream: stream, Certificates: []string{op.fp}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "enrollments.json")
	if err = os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	reg, err := collector.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	contracts, err := wire.Compile()
	if err != nil {
		t.Fatal(err)
	}
	sink := collector.New(dir, contracts, reg)
	status := func() (time.Time, error) {
		rows, err := collector.Statuses(dir, reg, time.Minute, time.Now())
		if err != nil {
			return time.Time{}, err
		}
		return rows[0].Latest.ReceivedAt, nil
	}
	first := make(chan time.Time, 1)
	var attempts atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			saved := httptest.NewRecorder()
			sink.ServeHTTP(saved, r)
			if saved.Code != 204 {
				t.Errorf("initial acceptance: %d", saved.Code)
				w.WriteHeader(400)
				return
			}
			at, err := status()
			if err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			first <- at
			panic(http.ErrAbortHandler)
		}
		sink.ServeHTTP(w, r)
	}))
	server.TLS, err = transport.TLS(host.cert, host.key, issuers.path, true)
	if err != nil {
		t.Fatal(err)
	}
	server.StartTLS()
	defer server.Close()
	cfg, err := transport.TLS(op.cert, op.key, servers.path, false)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := reporter.New(filepath.Join(dir, "missing.sock"), server.URL+wire.Path, stream, dir, cfg, contracts)
	if err != nil {
		t.Fatal(err)
	}
	defer rep.Close()
	if _, err = rep.Once(context.Background()); err != nil {
		t.Fatal(err)
	}
	at, err := status()
	if err != nil {
		t.Fatal(err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected two attempts: %d", attempts.Load())
	}
	if !at.Equal(<-first) {
		t.Fatal("retry refreshed collector timestamp")
	}
}
