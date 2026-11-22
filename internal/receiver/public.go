package receiver

import (
	"cmp"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
)

const reportFreshness = 3 * time.Minute

// Only these fields can leave the receiver through the public endpoint.
type publicNode struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Producer   string               `json:"producer,omitempty"`
	Status     string               `json:"status"`
	State      string               `json:"state,omitempty"`
	Phase      string               `json:"phase,omitempty"`
	Checked    *time.Time           `json:"checked,omitempty"`
	ReadMicros *uint32              `json:"readMicros,omitempty"`
	Queues     map[string]queuePair `json:"queues,omitempty"`
	Error      string               `json:"error,omitempty"`
}

type observation struct {
	Value   *uint64    `json:"value"`
	Quality string     `json:"quality"`
	AsOf    *time.Time `json:"asOf"`
}
type queuePair struct {
	Wrap   observation `json:"wrap"`
	Unwrap observation `json:"unwrap"`
}

type publicSnapshot struct {
	Updated time.Time    `json:"updated"`
	Nodes   []publicNode `json:"nodes"`
}

var networkNames = map[uint32]string{1: "ETH", 56: "BNB"}

func reportHealth(s collector.Status) (status, reason string) {
	switch {
	case s.Latest == nil:
		return "Awaiting report", ""
	case s.Delivery == "stale":
		return "Stale", "No recent heartbeat"
	}
	switch s.Latest.Report.LocalRead {
	case "ok":
		return "Online", ""
	case "unreachable":
		return "Unavailable", "Socket unreachable"
	case "timeout":
		return "Unavailable", "Socket timed out"
	case "unsupported":
		return "Unavailable", "Snapshot unsupported"
	default:
		return "Unavailable", "Invalid snapshot"
	}
}

func project(n node, s collector.Status) (publicNode, error) {
	status, reason := reportHealth(s)
	// Derive the public row key from the random enrollment UUID. Display
	// names can collide after redaction, and stream IDs are private.
	p := publicNode{ID: digest([]byte("public-node:" + n.ID)), Name: redactIPText(n.Name),
		Producer: n.publicProducer(), Status: status, Error: reason}
	last := s.Latest
	if last != nil {
		p.Checked = &last.ReceivedAt
	}
	if status != "Online" {
		return p, nil
	}
	// The collector has already validated the snapshot against the pinned schema.
	var snapshot struct {
		Phase, BridgeState string
		NetworksKnown      bool
		Networks           []struct {
			NetworkClass, ChainID uint32
			Wrap, Unwrap          struct{ UnsignedObserved observation }
		}
	}
	if err := json.Unmarshal(last.Report.Snapshot, &snapshot); err != nil {
		return p, err
	}
	p.State, p.Phase, p.ReadMicros = snapshot.BridgeState, snapshot.Phase, last.ReadMicros
	if snapshot.NetworksKnown {
		p.Queues = make(map[string]queuePair)
		for _, network := range snapshot.Networks {
			name, ok := networkNames[network.ChainID]
			if ok && network.NetworkClass == 2 {
				p.Queues[name] = queuePair{network.Wrap.UnsignedObserved, network.Unwrap.UnsignedObserved}
			}
		}
	}
	return p, nil
}

// Reading the bounded stream histories costs disk I/O. Share the projection across
// viewers for ten seconds. Fleet edits (especially revocation) invalidate it.
type statusCache struct {
	mu       sync.Mutex
	revision uint64
	until    time.Time
	data     *publicSnapshot
}

func (a *web) publicData(now time.Time) (*publicSnapshot, error) {
	f := a.fleet
	c := &f.publicCache
	c.mu.Lock()
	defer c.mu.Unlock()
	f.mu.Lock()
	rev := f.revision
	if rev == c.revision && now.Before(c.until) {
		f.mu.Unlock()
		return c.data, nil
	}
	nodes := slices.Clone(f.nodes)
	f.mu.Unlock()
	rows := make([]publicNode, 0, len(nodes))
	for _, n := range nodes {
		if len(n.Cert) == 0 || n.Revoked != nil {
			continue
		}
		s, err := collector.StreamStatus(f.dir, collector.Enrollment{Node: n.Name, Stream: n.ID}, reportFreshness, now)
		if err != nil {
			return nil, err
		}
		row, err := project(n, s)
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	slices.SortFunc(rows, func(a, b publicNode) int { return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)) })
	c.data = &publicSnapshot{Updated: now, Nodes: rows}
	c.until, c.revision = now.Add(10*time.Second), rev
	return c.data, nil
}

func (a *web) publicStatus(w http.ResponseWriter, r *http.Request) {
	snapshot, err := a.publicData(time.Now().UTC())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(snapshot)
}
