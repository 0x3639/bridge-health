package receiver

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/wire"
)

func TestPublicSnapshotProjection(t *testing.T) {
	const network = `{"networkClass":2,"chainId":CHAIN,"wrap":{"unsignedObserved":{"value":0,"quality":"complete","asOf":"2026-01-01T00:00:00Z"}},"unwrap":{"unsignedObserved":{"value":9,"quality":"partial","asOf":"2026-01-01T00:00:00Z"}}}`
	networks := make([]string, 0, 4)
	for _, id := range []string{"56", "1", "73405", "99999"} {
		networks = append(networks, strings.Replace(network, "CHAIN", id, 1))
	}
	snapshot := `{"phase":"running","bridgeState":"LiveState","networksKnown":true,"networks":[` + strings.Join(networks, ",") + `],"build":{"version":"private-build"}}`
	latency := uint32(1350)
	s := collector.Status{Delivery: "fresh", Latest: &collector.Stored{ReadMicros: &latency, ReceivedAt: time.Now().UTC(), Report: wire.Report{LocalRead: "ok", Snapshot: json.RawMessage(snapshot)}}}
	p, err := project(node{Name: "pilot"}, s)
	if err != nil || p.Status != "Online" || p.State != "LiveState" || p.Phase != "running" || p.ReadMicros == nil || *p.ReadMicros != 1350 || len(p.Queues) != 3 {
		t.Fatal(p, err)
	}
	for _, chain := range []string{"BNB", "ETH", "Supernova"} {
		q := p.Queues[chain]
		if q.Wrap.Value == nil || *q.Wrap.Value != 0 || q.Unwrap.Value == nil || *q.Unwrap.Value != 9 || q.Unwrap.Quality != "partial" || q.Unwrap.AsOf == nil {
			t.Fatal(chain, q)
		}
	}
	s.Delivery = "stale"
	p, err = project(node{}, s)
	if err != nil || p.Status != "Stale" || p.State != "" || p.ReadMicros != nil || p.Queues != nil || p.Checked == nil {
		t.Fatal("stale observations presented as current", p, err)
	}
}

func TestDirectoryProducerOverrides(t *testing.T) {
	n := node{Name: "nomlabz.org"}
	if n.publicProducer() != "z1qqjx9zdunxxuvkqwjfkac0jvnrzwu45ey8lhyl" {
		t.Fatal("existing directory name not resolved")
	}
	blank := ""
	n.Producer = &blank
	if n.publicProducer() != "" {
		t.Fatal("cleared producer unexpectedly published")
	}
	n = node{Name: "different-pillar"}
	if n.publicProducer() != "" {
		t.Fatal("unknown pillar assigned a producer")
	}
}
