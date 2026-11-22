package receiver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/wire"
)

type node struct {
	ID            string     `json:"streamId"`
	Name          string     `json:"nodeId"`
	Producer      *string    `json:"producer,omitempty"`
	Created       time.Time  `json:"created"`
	InviteExpires time.Time  `json:"inviteExpires"`
	TokenHash     string     `json:"tokenHash,omitempty"`
	Cert          []byte     `json:"clientCertificate,omitempty"`
	Revoked       *time.Time `json:"revoked,omitempty"`
}

func (n node) state(now time.Time) string {
	switch {
	case n.Revoked != nil && len(n.Cert) == 0:
		return "Cancelled"
	case n.Revoked != nil:
		return "Revoked"
	case len(n.Cert) > 0:
		return "Enrolled"
	case !now.Before(n.InviteExpires):
		return "Expired"
	default:
		return "Pending"
	}
}

type fleet struct {
	mu        sync.Mutex
	dir       string
	nodes     []node
	revision  uint64
	collector *collector.Collector
	ca        *authority
}

func registryFor(nodes []node) (*collector.Registry, error) {
	entries := make([]collector.Enrollment, 0, len(nodes))
	ids := map[string]bool{}
	for _, n := range nodes {
		if !wire.StreamID(n.ID) || ids[n.ID] || n.Name == "" {
			return nil, errors.New("invalid saved receiver node")
		}
		ids[n.ID] = true
		if len(n.Cert) == 0 || n.Revoked != nil {
			continue
		}
		if _, err := x509.ParseCertificate(n.Cert); err != nil {
			return nil, err
		}
		entries = append(entries, collector.Enrollment{Node: n.Name, Stream: n.ID, Certificates: []string{digest(n.Cert)}})
	}
	return collector.NewRegistry(entries)
}

// The caller holds the single-writer state directory lock for the lifetime of the receiver.
func openFleet(dir string) (*fleet, error) {
	a, err := openAuthority(dir)
	if err != nil {
		return nil, err
	}
	f := &fleet{dir: dir, ca: a}
	b, err := os.ReadFile(filepath.Join(dir, "fleet.json"))
	if err == nil {
		err = json.Unmarshal(b, &f.nodes)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	reg, err := registryFor(f.nodes)
	if err != nil {
		return nil, err
	}
	c, err := wire.Compile()
	if err != nil {
		return nil, err
	}
	f.collector = collector.New(dir, c, reg)
	return f, nil
}

func digest(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (f *fleet) save(nodes []node) error {
	reg, err := registryFor(nodes)
	if err != nil {
		return err
	}
	if err = localstate.Write(filepath.Join(f.dir, "fleet.json"), nodes); err != nil {
		return err
	}
	f.collector.Reload(reg)
	f.nodes = nodes
	f.revision++
	return nil
}

var errCSR = errors.New("invalid P-256 certificate request")
var errName = errors.New("enter a unique node name of 1–80 characters")
var errInvite = errors.New("this invitation is invalid, expired, cancelled, or already used")
var errInactive = errors.New("this node or invitation is already inactive")

func (f *fleet) invite(name string, now time.Time) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errName
	}
	for _, n := range f.nodes {
		state := n.state(now)
		if strings.EqualFold(n.Name, name) && (state == "Enrolled" || state == "Pending") {
			return "", errName
		}
	}
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	id := fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
	secret, err := randomToken()
	if err != nil {
		return "", err
	}
	token := id + "." + secret
	n := node{ID: id, Name: name, Created: now, InviteExpires: now.Add(24 * time.Hour), TokenHash: digest([]byte(token))}
	return token, f.save(append(slices.Clone(f.nodes), n))
}

type credentials struct {
	Stream      string `json:"stream"`
	Certificate string `json:"certificate"`
	CA          string `json:"ca"`
	Collector   string `json:"collector"`
}

func (f *fleet) redeem(token string, csr *x509.CertificateRequest, endpoint string, now time.Time) (credentials, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var result credentials
	k, ok := csr.PublicKey.(*ecdsa.PublicKey)
	if !ok || k.Curve != elliptic.P256() || csr.CheckSignature() != nil {
		return result, errCSR
	}
	hash := digest([]byte(token))
	for i, n := range f.nodes {
		if n.TokenHash != hash || n.Revoked != nil || !now.Before(n.InviteExpires) {
			continue
		}
		if len(n.Cert) > 0 {
			// A lost HTTP response can be recovered only by the original node key.
			cert, err := x509.ParseCertificate(n.Cert)
			if err != nil {
				return result, err
			}
			if !k.Equal(cert.PublicKey) {
				return result, errInvite
			}
		} else {
			cert, err := f.ca.issueClient(csr, n.ID)
			if err != nil {
				return result, err
			}
			n.Cert = cert.Raw
			next := slices.Clone(f.nodes)
			next[i] = n
			if err = f.save(next); err != nil {
				return result, err
			}
		}
		return credentials{n.ID, certPEM(n.Cert), certPEM(f.ca.cert.Raw), endpoint}, nil
	}
	return result, errInvite
}

func (f *fleet) revoke(id string, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, n := range f.nodes {
		if n.ID != id {
			continue
		}
		if n.Revoked != nil {
			return errInactive
		}
		n.Revoked = &now
		n.TokenHash = ""
		next := slices.Clone(f.nodes)
		next[i] = n
		return f.save(next)
	}
	return errInactive
}

var producerAddress = regexp.MustCompile(`^z1[023456789acdefghjklmnpqrstuvwxyz]{38}$`)
var errProducer = errors.New("enter a 40-character Zenon producer address starting with z1, or leave it empty")

func (f *fleet) setProducer(id, address string) error {
	if address != "" && !producerAddress.MatchString(address) {
		return errProducer
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, n := range f.nodes {
		if n.ID == id && len(n.Cert) != 0 && n.Revoked == nil {
			next := slices.Clone(f.nodes)
			next[i].Producer = &address
			return f.save(next)
		}
	}
	return errInactive
}
