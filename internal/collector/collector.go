package collector

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/wire"
	"golang.org/x/time/rate"
)

type Stored struct {
	Node       string      `json:"nodeId"`
	ReceivedAt time.Time   `json:"receivedAt"`
	ReadMicros *uint32     `json:"readMicros,omitempty"`
	Digest     string      `json:"bodySHA256"`
	Report     wire.Report `json:"heartbeat"`
}

// Keep a bounded history in the same atomic update as the replay watermark.
const historyLimit = 30

type streamState struct {
	Stored
	History           []Stored `json:"history,omitempty"`
	HistoricalSuccess *Stored  `json:"historicalSuccess,omitempty"`
}

type Collector struct {
	mu        sync.Mutex
	dir       string
	contracts *wire.Contracts
	registry  *Registry
	buckets   map[string]*rate.Limiter
	global    *rate.Limiter
}

func New(dir string, c *wire.Contracts, reg *Registry) *Collector {
	s := &Collector{dir: dir, contracts: c, buckets: map[string]*rate.Limiter{}, global: rate.NewLimiter(100, 200)}
	s.Reload(reg)
	return s
}
func (s *Collector) Reload(reg *Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := map[string]*rate.Limiter{}
	for _, e := range reg.Entries {
		b := s.buckets[e.Stream]
		if b == nil {
			b = rate.NewLimiter(rate.Every(10*time.Second), 6)
		}
		next[e.Stream] = b
	}
	s.buckets = next
	s.registry = reg
}
func (s *Collector) authorize(fp string) (Enrollment, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.registry.byCert[fp]
	if !ok {
		return Enrollment{}, http.StatusForbidden
	}
	if !s.buckets[e.Stream].Allow() || !s.global.Allow() {
		return e, http.StatusTooManyRequests
	}
	return e, 0
}
func (s *Collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != wire.Path {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		w.WriteHeader(405)
		return
	}
	if r.TLS == nil || len(r.TLS.VerifiedChains) == 0 || len(r.TLS.PeerCertificates) == 0 {
		w.WriteHeader(403)
		return
	}
	cert := r.TLS.PeerCertificates[0]
	now := time.Now().UTC()
	if now.Before(cert.NotBefore) || !now.Before(cert.NotAfter) {
		w.WriteHeader(403)
		return
	}
	fp := fingerprint(cert.Raw)
	e, status := s.authorize(fp)
	if status != 0 {
		if status == 429 {
			w.Header().Set("Retry-After", "10")
		}
		w.WriteHeader(status)
		return
	}
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" || r.Header.Get("Content-Encoding") != "" {
		w.WriteHeader(415)
		return
	}
	if r.ContentLength > wire.MaxBody {
		w.WriteHeader(413)
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, wire.MaxBody))
	if err != nil {
		var large *http.MaxBytesError
		if errors.As(err, &large) {
			w.WriteHeader(413)
		} else {
			w.WriteHeader(400)
		}
		return
	}
	rep, err := s.contracts.Report(data)
	if err != nil {
		http.Error(w, "invalid heartbeat", 400)
		return
	}
	if rep.Stream != e.Stream {
		w.WriteHeader(403)
		return
	}
	var readMicros *uint32
	if n, err := strconv.ParseUint(r.Header.Get(wire.LocalReadHeader), 10, 32); err == nil && n <= 60_000_000 && rep.LocalRead == "ok" {
		measured := uint32(n)
		readMicros = &measured
	}
	status, err = s.accept(fp, e, rep, fingerprint(data), time.Now().UTC(), readMicros)
	if err != nil {
		log.Printf("persist heartbeat for %s: %v", e.Stream, err)
	}
	w.WriteHeader(status)
}
func readLatest(dir, stream string) (*streamState, error) {
	b, err := os.ReadFile(filepath.Join(dir, stream+".json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var prev streamState
	if err = json.Unmarshal(b, &prev); err != nil {
		return nil, err
	}
	if prev.Report.Stream != stream || prev.Report.Sequence == 0 || prev.Node == "" || prev.ReceivedAt.IsZero() || prev.Digest == "" {
		return nil, errors.New("invalid saved stream state")
	}
	return &prev, nil
}
func (s *Collector) accept(fp string, e Enrollment, rep wire.Report, digest string, now time.Time, readMicros *uint32) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.registry.byCert[fp]
	if !ok || current.Node != e.Node || current.Stream != rep.Stream {
		return 403, nil
	}
	prev, err := readLatest(s.dir, rep.Stream)
	if err != nil {
		return 503, err
	}
	if prev != nil {
		if prev.Node != e.Node {
			return 409, nil
		}
		if rep.Sequence == prev.Report.Sequence && digest == prev.Digest {
			// A prior rename may have succeeded while its directory sync failed.
			if err := localstate.SyncDirectory(s.dir); err != nil {
				return 503, err
			}
			return 204, nil
		}
		if rep.Sequence <= prev.Report.Sequence {
			return 409, nil
		}
	}
	if rep.SentAt.Before(now.Add(-wire.MaxAge)) || rep.SentAt.After(now.Add(wire.FutureSkew)) {
		return 400, nil
	}
	record := streamState{Stored: Stored{Node: e.Node, ReceivedAt: now, Digest: digest, Report: rep, ReadMicros: readMicros}}
	if prev != nil {
		prior := prev.History
		if len(prior) >= historyLimit {
			prior = prior[len(prior)-historyLimit+1:]
		}
		record.History = append(prior, prev.Stored)
		if rep.LocalRead != "ok" {
			record.HistoricalSuccess = prev.HistoricalSuccess
			if prev.Report.LocalRead == "ok" {
				record.HistoricalSuccess = &prev.Stored
			}
		}
	}
	if err := localstate.Write(filepath.Join(s.dir, rep.Stream+".json"), record); err != nil {
		return 503, err
	}
	return 204, nil
}

type Status struct {
	Node              string   `json:"nodeId"`
	Stream            string   `json:"streamId"`
	Delivery          string   `json:"delivery"`
	Latest            *Stored  `json:"latest"`
	HistoricalSuccess *Stored  `json:"historicalSuccess,omitempty"`
	DisplayName       string   `json:"displayName,omitempty"`
	ExpectedNetworks  []uint32 `json:"expectedEvmChainIds,omitempty"`
}

func Statuses(dir string, reg *Registry, stale time.Duration, now time.Time) ([]Status, error) {
	out := make([]Status, 0, len(reg.Entries))
	for _, e := range reg.Entries {
		row, err := StreamStatus(dir, e, stale, now)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, nil
}

// StreamStatus reads the latest durable heartbeat, including for a retired stream.
func StreamStatus(dir string, e Enrollment, stale time.Duration, now time.Time) (Status, error) {
	status, _, err := StreamStatusWithHistory(dir, e, stale, now)
	return status, err
}

// StreamStatusWithHistory returns status and prior receipts from the same atomic stream file.
func StreamStatusWithHistory(dir string, e Enrollment, stale time.Duration, now time.Time) (Status, []Stored, error) {
	item, err := readLatest(dir, e.Stream)
	if err != nil {
		return Status{}, nil, err
	}
	row := Status{Node: e.Node, Stream: e.Stream, Delivery: "never", DisplayName: e.DisplayName, ExpectedNetworks: e.ExpectedNetworks}
	if item != nil {
		if item.Node != e.Node {
			return Status{}, nil, errors.New("saved stream belongs to a different node")
		}
		row.Delivery = "fresh"
		row.Latest = &item.Stored
		row.HistoricalSuccess = item.HistoricalSuccess
		if now.Sub(item.ReceivedAt) >= stale {
			row.Delivery = "stale"
		}
		return row, item.History, nil
	}
	return row, nil, nil
}
