package receiver

import (
	"crypto/x509"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/wire"
)

// Node names and build versions are the two free-text fields in the v1 response.
// Include dotted IPv4 tails in both compressed and expanded IPv6 literals.
const dottedQuad = `([0-9]+\.){3,}[0-9]+`

var ipText = regexp.MustCompile(`(?i)[0-9a-f:.]*::[0-9a-f:.]*|([0-9a-f]{1,4}:){6}` + dottedQuad + `|([0-9a-f]{1,4}:){7}[0-9a-f]{1,4}|` + dottedQuad)

func redactIPText(value string) string {
	return ipText.ReplaceAllStringFunc(value, func(candidate string) string {
		if strings.Contains(candidate, ":") {
			return "[redacted]"
		}
		// Numeric build labels can resemble addresses. Mask a dotted run only
		// when four consecutive parts are IPv4 octets, including padded ones.
		valid := 0
		for _, part := range strings.Split(candidate, ".") {
			if _, err := strconv.ParseUint(part, 10, 8); err != nil || len(part) > 3 {
				valid = 0
				continue
			}
			valid++
			if valid == 4 {
				return "[redacted]"
			}
		}
		return candidate
	})
}

func (a *web) requireAPIToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		headers := r.Header.Values("Authorization")
		status := http.StatusUnauthorized
		if len(headers) == 1 {
			scheme, raw, ok := strings.Cut(headers[0], " ")
			if ok && strings.EqualFold(scheme, "Bearer") {
				status = a.tokens.authorize(strings.TrimLeft(raw, " "), time.Now())
			}
		}
		switch status {
		case http.StatusOK:
			next(w, r)
		case http.StatusTooManyRequests:
			w.Header().Set("Retry-After", "1")
			apiError(w, status)
		default:
			w.Header().Set("WWW-Authenticate", `Bearer realm="bridge-status"`)
			apiError(w, status)
		}
	}
}

func apiError(w http.ResponseWriter, status int) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": http.StatusText(status)})
}

// Project the closed v1 snapshot schema. Extra properties in a stored snapshot
// must never become service API fields just because the ingestion schema grows.
type apiObservation[T any] struct {
	Value   *T         `json:"value"`
	AsOf    *time.Time `json:"asOf"`
	Quality string     `json:"quality"`
}

type apiWork struct {
	AwaitingFinality         observation               `json:"awaitingFinality"`
	UnsignedObserved         observation               `json:"unsignedObserved"`
	Selected                 apiObservation[selection] `json:"selected"`
	SignedAwaitingSubmission observation               `json:"signedAwaitingSubmission"`
	OldestPendingSince       apiObservation[time.Time] `json:"oldestPendingSince"`
	LastSignedWindow         observation               `json:"lastSignedWindow"`
}

type selection struct {
	Window uint64 `json:"window"`
	Count  uint64 `json:"count"`
}

type apiSnapshot struct {
	SchemaVersion int       `json:"schemaVersion"`
	SampledAt     time.Time `json:"sampledAt"`
	StartedAt     time.Time `json:"startedAt"`
	Build         struct {
		Version   string  `json:"version"`
		GitCommit *string `json:"gitCommit"`
	} `json:"build"`
	Phase       string  `json:"phase"`
	BridgeState *string `json:"bridgeState"`
	Zenon       struct {
		NodeSync               apiObservation[string] `json:"nodeSync"`
		FrontierMomentum       observation            `json:"frontierMomentum"`
		ProcessedReceiveHeight observation            `json:"processedReceiveHeight"`
	} `json:"zenon"`
	NetworksKnown bool `json:"networksKnown"`
	Networks      []struct {
		NetworkClass   uint32                 `json:"networkClass"`
		ChainID        uint32                 `json:"chainId"`
		NodeSync       apiObservation[string] `json:"nodeSync"`
		HeadHeight     observation            `json:"headHeight"`
		ScannedThrough observation            `json:"scannedThrough"`
		LastRangeAt    *time.Time             `json:"lastRangeAt"`
		Wrap           apiWork                `json:"wrap"`
		Unwrap         apiWork                `json:"unwrap"`
	} `json:"networks"`
}

// Keep the transport and storage structs private even if they gain fields later.
type apiHeartbeat struct {
	SchemaVersion int          `json:"schemaVersion"`
	StreamID      string       `json:"streamId"`
	Sequence      uint64       `json:"sequence"`
	SentAt        time.Time    `json:"sentAt"`
	LocalRead     string       `json:"localRead"`
	LastSuccess   *time.Time   `json:"lastLocalSuccessAt"`
	Snapshot      *apiSnapshot `json:"snapshot"`
}

type apiReceipt struct {
	Node       string       `json:"nodeId"`
	ReceivedAt time.Time    `json:"receivedAt"`
	ReadMicros *uint32      `json:"readMicros,omitempty"`
	Digest     string       `json:"bodySHA256"`
	Heartbeat  apiHeartbeat `json:"heartbeat"`
}

func serviceReceipt(src *collector.Stored) (*apiReceipt, error) {
	if src == nil {
		return nil, nil
	}
	r := src.Report
	heartbeat := apiHeartbeat{SchemaVersion: r.Version, StreamID: r.Stream, Sequence: r.Sequence,
		SentAt: r.SentAt, LocalRead: r.LocalRead, LastSuccess: r.LastSuccess}
	if r.LocalRead == "ok" {
		var snap apiSnapshot
		if err := json.Unmarshal(r.Snapshot, &snap); err != nil {
			return nil, err
		}
		snap.Build.Version = redactIPText(snap.Build.Version)
		heartbeat.Snapshot = &snap
	}
	return &apiReceipt{Node: redactIPText(src.Node), ReceivedAt: src.ReceivedAt, ReadMicros: src.ReadMicros,
		Digest: src.Digest, Heartbeat: heartbeat}, nil
}

type apiNode struct {
	Node               string      `json:"nodeId"`
	Stream             string      `json:"streamId"`
	Delivery           string      `json:"delivery"`
	Latest             *apiReceipt `json:"latest"`
	HistoricalSuccess  *apiReceipt `json:"historicalSuccess,omitempty"`
	Health             string      `json:"status"`
	Error              string      `json:"error,omitempty"`
	Producer           string      `json:"producer,omitempty"`
	Access             string      `json:"access"`
	Created            time.Time   `json:"created"`
	CertificateExpires time.Time   `json:"certificateExpires"`
	RevokedAt          *time.Time  `json:"revokedAt,omitempty"`
}

func (a *web) readAPINode(n node, now time.Time) (apiNode, []collector.Stored, error) {
	status, prior, err := collector.StreamStatusWithHistory(a.fleet.dir, collector.Enrollment{Node: n.Name, Stream: n.ID}, reportFreshness, now)
	if err != nil {
		return apiNode{}, nil, err
	}
	cert, err := x509.ParseCertificate(n.Cert)
	if err != nil {
		return apiNode{}, nil, err
	}
	latest, err := serviceReceipt(status.Latest)
	if err != nil {
		return apiNode{}, nil, err
	}
	lastGood, err := serviceReceipt(status.HistoricalSuccess)
	if err != nil {
		return apiNode{}, nil, err
	}
	health, reason := reportHealth(status)
	access := "active"
	if n.Revoked != nil {
		access, health, reason = "revoked", "Revoked", ""
	} else if !now.Before(cert.NotAfter) {
		access, health, reason = "expired", "Certificate expired", ""
	}
	return apiNode{Node: redactIPText(status.Node), Stream: status.Stream, Delivery: status.Delivery,
		Latest: latest, HistoricalSuccess: lastGood, Health: health, Error: reason, Producer: n.publicProducer(), Access: access,
		Created: n.Created, CertificateExpires: cert.NotAfter, RevokedAt: n.Revoked}, prior, nil
}

func apiReadError(w http.ResponseWriter, err error) {
	log.Printf("read status API: %v", err)
	apiError(w, http.StatusInternalServerError)
}

func (a *web) apiStatus(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	nodes := make([]apiNode, 0)
	for _, n := range a.fleet.listNodes() {
		if len(n.Cert) == 0 {
			continue
		}
		row, _, err := a.readAPINode(n, now)
		if err != nil {
			apiReadError(w, err)
			return
		}
		nodes = append(nodes, row)
	}
	json.NewEncoder(w).Encode(struct {
		Updated time.Time `json:"updated"`
		Nodes   []apiNode `json:"nodes"`
	}{now, nodes})
}

func (a *web) apiStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("stream")
	if !wire.StreamID(id) {
		apiError(w, http.StatusNotFound)
		return
	}
	now := time.Now().UTC()
	for _, n := range a.fleet.listNodes() {
		if n.ID != id || len(n.Cert) == 0 {
			continue
		}
		row, prior, err := a.readAPINode(n, now)
		if err != nil {
			apiReadError(w, err)
			return
		}
		history := make([]*apiReceipt, 0, len(prior))
		for i := range prior {
			receipt, err := serviceReceipt(&prior[i])
			if err != nil {
				apiReadError(w, err)
				return
			}
			history = append(history, receipt)
		}
		json.NewEncoder(w).Encode(struct {
			Updated time.Time     `json:"updated"`
			Node    apiNode       `json:"node"`
			History []*apiReceipt `json:"history"`
		}{now, row, history})
		return
	}
	apiError(w, http.StatusNotFound)
}
