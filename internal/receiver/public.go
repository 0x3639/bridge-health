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

var networkNames = map[uint32]string{1: "ETH", 56: "BNB", 73405: "Supernova"}

func project(n node, s collector.Status) (publicNode, error) {
	p := publicNode{Name: n.Name, Producer: n.publicProducer(), Status: "Awaiting report"}
	if s.Latest == nil {
		return p, nil
	}
	last := s.Latest
	p.Checked = &last.ReceivedAt
	if s.Delivery == "stale" {
		p.Status, p.Error = "Stale", "No recent heartbeat"
		return p, nil
	}
	if last.Report.LocalRead != "ok" {
		p.Status = "Unavailable"
		switch last.Report.LocalRead {
		case "unreachable":
			p.Error = "Socket unreachable"
		case "timeout":
			p.Error = "Socket timed out"
		case "unsupported":
			p.Error = "Snapshot unsupported"
		default:
			p.Error = "Invalid snapshot"
		}
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
	p.Status, p.State, p.Phase, p.ReadMicros = "Online", snapshot.BridgeState, snapshot.Phase, last.ReadMicros
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
	data     []byte
}

func (a *web) publicData(now time.Time) ([]byte, error) {
	c := &a.publicCache
	c.mu.Lock()
	defer c.mu.Unlock()
	f := a.fleet
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
	b, err := json.Marshal(struct {
		Updated time.Time    `json:"updated"`
		Nodes   []publicNode `json:"nodes"`
	}{now, rows})
	if err != nil {
		return nil, err
	}
	c.data, c.until, c.revision = b, now.Add(10*time.Second), rev
	return b, nil
}

func (a *web) publicStatus(w http.ResponseWriter, r *http.Request) {
	b, err := a.publicData(time.Now().UTC())
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Write(b)
}
