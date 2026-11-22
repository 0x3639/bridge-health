package collector

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/0x3639/bridge-health/internal/wire"
)

type Enrollment struct {
	DisplayName      string   `json:"displayName,omitempty"`
	ExpectedNetworks []uint32 `json:"expectedEvmChainIds,omitempty"`
	Node             string   `json:"nodeId"`
	Stream           string   `json:"streamId"`
	Certificates     []string `json:"certificateSHA256"`
}
type Registry struct {
	Entries []Enrollment `json:"enrollments"`
	byCert  map[string]Enrollment
}

func Load(path string) (*Registry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var reg Registry
	if err = wire.StrictJSON(data, &reg); err != nil {
		return nil, err
	}
	return NewRegistry(reg.Entries)
}

func NewRegistry(entries []Enrollment) (*Registry, error) {
	reg := &Registry{Entries: entries}
	if reg.Entries == nil {
		return nil, errors.New("enrollments array required")
	}
	reg.byCert = map[string]Enrollment{}
	streams := map[string]bool{}
	for _, e := range reg.Entries {
		if strings.TrimSpace(e.Node) == "" || len(e.Node) > 128 || !wire.StreamID(e.Stream) || streams[e.Stream] || len(e.Certificates) == 0 {
			return nil, errors.New("invalid node, duplicate stream, or missing certificate")
		}
		streams[e.Stream] = true
		for _, fp := range e.Certificates {
			if len(fp) != 64 || strings.ToLower(fp) != fp {
				return nil, errors.New("expected lowercase SHA-256 certificate fingerprint")
			}
			if _, err := hex.DecodeString(fp); err != nil {
				return nil, err
			}
			if _, used := reg.byCert[fp]; used {
				return nil, errors.New("certificate assigned more than once")
			}
			reg.byCert[fp] = e
		}
	}
	return reg, nil
}
func fingerprint(b []byte) string { return fmt.Sprintf("%x", sha256.Sum256(b)) }
