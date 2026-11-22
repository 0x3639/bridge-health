package collector

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/wire"
)

func TestIngestionAndLimits(t *testing.T) {
	now := time.Now().UTC()
	cert := &x509.Certificate{Raw: []byte("test leaf"), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	fp := fingerprint(cert.Raw)
	e := Enrollment{Node: "node", Stream: "e71664d3-a7ba-4029-8e17-95a5d0b85537", Certificates: []string{fp}}
	reg := &Registry{Entries: []Enrollment{e}, byCert: map[string]Enrollment{fp: e}}
	c, err := wire.Compile()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	s := New(dir, c, reg)
	report := wire.Report{Version: 1, LocalRead: "unreachable", Stream: e.Stream, Sequence: 1, SentAt: now}
	send := func(b []byte, ct string) int {
		req := httptest.NewRequest("POST", wire.Path, strings.NewReader(string(b)))
		req.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{cert}}, PeerCertificates: []*x509.Certificate{cert}}
		req.Header.Set("Content-Type", ct)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, req)
		return w.Code
	}
	report.SentAt = now.Add(time.Minute)
	b, _ := json.Marshal(report)
	if got := send(b, "application/json"); got != 400 {
		t.Fatal("future", got)
	}
	report.SentAt = now.Add(-3 * time.Minute)
	b, _ = json.Marshal(report)
	if got := send(b, "application/json"); got != 400 {
		t.Fatal("stale", got)
	}
	report.SentAt = now
	b, _ = json.Marshal(report)
	if got := send(b, "text/plain"); got != 415 {
		t.Fatal("content type", got)
	}
	if got := send(make([]byte, wire.MaxBody+1), "application/json"); got != 413 {
		t.Fatal("size", got)
	}
	if got := send(b, "application/json"); got != 204 {
		t.Fatal("fresh", got)
	}
	report.LocalRead = "timeout"
	b, _ = json.Marshal(report)
	if got := send(b, "application/json"); got != 409 {
		t.Fatal("sequence reused with other content", got)
	}
	if got := send(b, "application/json"); got != 429 {
		t.Fatal("per-stream rate", got)
	}
}
func TestConcurrentSequenceAndEnrollment(t *testing.T) {
	dir := t.TempDir()
	id := "e71664d3-a7ba-4029-8e17-95a5d0b85537"
	fp := strings.Repeat("0", 64)
	e := Enrollment{Node: "a", Stream: id, Certificates: []string{fp}}
	path := filepath.Join(dir, "registry.json")
	put := func(entries []Enrollment) {
		b, _ := json.Marshal(Registry{Entries: entries})
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
	}
	put([]Enrollment{e, e})
	if _, err := Load(path); err == nil {
		t.Fatal("duplicate stream/cert accepted")
	}
	put([]Enrollment{e})
	reg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s := New(dir, nil, reg)
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for i := 1; i <= 20; i++ {
		wg.Add(1)
		go func(seq uint64) {
			defer wg.Done()
			code, err := s.accept(fp, e, wire.Report{Stream: id, Sequence: seq, SentAt: now}, "digest", now, nil)
			if err != nil || code != 204 && code != 409 {
				t.Errorf("accept: %d %v", code, err)
			}
		}(uint64(i))
	}
	wg.Wait()
	prev, err := readLatest(dir, id)
	if err != nil || prev == nil || prev.Report.Sequence != 20 {
		t.Fatal(prev, err)
	}
	s.Reload(&Registry{})
	if code, _ := s.accept(fp, e, wire.Report{Stream: id, Sequence: 21, SentAt: now}, "digest", now, nil); code != 403 {
		t.Fatal("revocation during request", code)
	}
}
