package receiver

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/0x3639/bridge-health/internal/localstate"
	"golang.org/x/time/rate"
)

const apiTokenPrefix = "bhs_"

type tokenInfo struct {
	ID      string
	Name    string
	Created time.Time
	Expires time.Time
}

type savedToken struct {
	tokenInfo
	Digest string
	limit  *rate.Limiter
}

type apiTokens struct {
	mu   sync.Mutex
	path string
	byID map[string]savedToken
}

func tokenLimiter() *rate.Limiter { return rate.NewLimiter(1, 10) }

func openAPITokens(dir string) (*apiTokens, error) {
	s := &apiTokens{path: filepath.Join(dir, "api-tokens.json"), byID: make(map[string]savedToken)}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &s.byID); err != nil {
		return nil, err
	}
	if s.byID == nil {
		return nil, errors.New("invalid API token registry")
	}
	for id, t := range s.byID {
		if len(t.Digest) != 64 || t.Digest[:24] != id || t.ID != id || t.Name == "" || t.Created.IsZero() || !t.Expires.After(t.Created) {
			return nil, errors.New("invalid saved API token")
		}
		t.limit = tokenLimiter()
		s.byID[id] = t
	}
	return s, nil
}

func (s *apiTokens) save(next map[string]savedToken) error {
	if err := localstate.Write(s.path, next); err != nil {
		return err
	}
	s.byID = next
	return nil
}

func (s *apiTokens) issue(name string, lifetime time.Duration, now time.Time) (string, error) {
	random, err := randomToken()
	if err != nil {
		return "", err
	}
	secret := apiTokenPrefix + random
	hash := digest([]byte(secret))
	info := tokenInfo{ID: hash[:24], Name: name, Created: now, Expires: now.Add(lifetime)}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := maps.Clone(s.byID)
	next[info.ID] = savedToken{info, hash, tokenLimiter()}
	if err := s.save(next); err != nil {
		return "", err
	}
	return secret, nil
}

func (s *apiTokens) revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byID[id]; !ok {
		return errInactive
	}
	next := maps.Clone(s.byID)
	delete(next, id)
	return s.save(next)
}

func (s *apiTokens) list() []tokenInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]tokenInfo, 0, len(s.byID))
	for _, t := range s.byID {
		out = append(out, t.tokenInfo)
	}
	slices.SortFunc(out, func(a, b tokenInfo) int { return b.Created.Compare(a.Created) })
	return out
}

func (s *apiTokens) authorize(secret string, now time.Time) int {
	if len(secret) != len(apiTokenPrefix)+43 || !strings.HasPrefix(secret, apiTokenPrefix) {
		return http.StatusUnauthorized
	}
	hash := digest([]byte(secret))
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.byID[hash[:24]]
	if !ok || subtle.ConstantTimeCompare([]byte(t.Digest), []byte(hash)) != 1 || !now.Before(t.Expires) {
		return http.StatusUnauthorized
	}
	if !t.limit.Allow() {
		return http.StatusTooManyRequests
	}
	return http.StatusOK
}

func (a *web) tokenPage(w http.ResponseWriter, status int, secret, msg string) {
	a.render(w, status, page{View: "tokens", Title: "API tokens", User: a.user, Tokens: a.tokens.list(), IssuedToken: secret, Error: msg})
}

func (a *web) issueToken(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PostFormValue("name"))
	if name == "" || len(name) > 80 || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		a.tokenPage(w, 400, "", "Enter a service name of 1–80 characters.")
		return
	}
	lifetimes := map[string]time.Duration{"30": 30 * 24 * time.Hour, "90": 90 * 24 * time.Hour, "365": 365 * 24 * time.Hour}
	lifetime, ok := lifetimes[r.PostFormValue("days")]
	if !ok {
		a.tokenPage(w, 400, "", "Choose a token lifetime of 30, 90 or 365 days.")
		return
	}
	raw, err := a.tokens.issue(name, lifetime, time.Now().UTC())
	if err != nil {
		serverError(w, err)
		return
	}
	a.tokenPage(w, 201, raw, "")
}

func (a *web) revokeToken(w http.ResponseWriter, r *http.Request) {
	err := a.tokens.revoke(r.PathValue("id"))
	switch {
	case errors.Is(err, errInactive):
		http.NotFound(w, r)
	case err != nil:
		serverError(w, err)
	default:
		http.Redirect(w, r, "/admin/tokens", http.StatusSeeOther)
	}
}
