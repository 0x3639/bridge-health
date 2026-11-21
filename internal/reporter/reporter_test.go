package reporter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/wire"
)

func TestFailedDeliveryRetryAndRestart(t *testing.T) {
	var bodies []string
	var mu sync.Mutex
	sink := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		mu.Lock()
		defer mu.Unlock()
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.WriteHeader(503)
		} else {
			w.WriteHeader(204)
		}
	}))
	defer sink.Close()
	c, err := wire.Compile()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stream := "e71664d3-a7ba-4029-8e17-95a5d0b85537"
	run := func() {
		r, err := New(filepath.Join(dir, "missing.sock"), sink.URL+wire.Path, stream, dir, &tls.Config{}, c)
		if err != nil {
			t.Fatal(err)
		}
		r.remote = sink.Client()
		defer r.Close()
		if outcome, err := r.Once(context.Background()); err != nil || outcome != "unreachable" {
			t.Fatalf("%s: %v", outcome, err)
		}
	}
	run()
	run()
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 3 || bodies[0] != bodies[1] {
		t.Fatalf("retry bodies changed: %v", bodies)
	}
	var r wire.Report
	if err := json.Unmarshal([]byte(bodies[2]), &r); err != nil || r.Sequence != 2 || r.LastSuccess != nil {
		t.Fatal(r, err)
	}
	now := time.Now()
	if err := localstate.Write(filepath.Join(dir, "sequence.json"), position{Stream: stream, Sequence: wire.MaxSequence, LastSuccess: &now}); err != nil {
		t.Fatal(err)
	}
	reserved, err := New(filepath.Join(dir, "missing.sock"), sink.URL+wire.Path, stream, dir, &tls.Config{}, c)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reserved.Once(context.Background()); err == nil {
		t.Fatal("exhausted stream reused")
	}
}
