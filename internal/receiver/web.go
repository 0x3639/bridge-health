package receiver

import (
	"crypto/sha256"
	"crypto/subtle"
	"crypto/x509"
	"embed"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"golang.org/x/time/rate"
)

//go:embed assets/pages.html assets/ui assets/install.py
var assets embed.FS
var pages = template.Must(template.ParseFS(assets, "assets/pages.html"))

const sessionCookie = "__Host-bridge-health"

type web struct {
	fleet                                           *fleet
	origin, statusOrigin, endpoint, downloads, user string
	publicCache                                     statusCache
	password                                        [32]byte
	loginRate, enrollRate                           *rate.Limiter
	mu                                              sync.Mutex
	sessions                                        map[string]time.Time
	installer                                       []byte
}

func newWeb(f *fleet, origin, statusOrigin, endpoint, downloads, user, password string) (*web, error) {
	// Bind the script to this deployment and the exact artifacts built into its image.
	script, err := assets.ReadFile("assets/install.py")
	if err != nil {
		return nil, err
	}
	checks := map[string]string{}
	for _, arch := range []string{"amd64", "arm64"} {
		file, err := os.Open(filepath.Join(downloads, "bridge-health-reporter-linux-"+arch))
		if err != nil {
			return nil, err
		}
		h := sha256.New()
		_, err = io.Copy(h, file)
		file.Close()
		if err != nil {
			return nil, err
		}
		checks[arch] = fmt.Sprintf("%x", h.Sum(nil))
	}
	values, _ := json.Marshal(checks)
	src := strings.Replace(string(script), "\"@@BASE_URL@@\"", fmt.Sprintf("%q", origin), 1)
	src = strings.Replace(src, "\"@@CHECKSUMS@@\"", string(values), 1)
	return &web{fleet: f, origin: origin, statusOrigin: statusOrigin, endpoint: endpoint, downloads: downloads, user: user, password: sha256.Sum256([]byte(password)),
		loginRate: rate.NewLimiter(rate.Every(2*time.Second), 12), enrollRate: rate.NewLimiter(4, 20),
		sessions: map[string]time.Time{}, installer: []byte(src)}, nil
}

func (a *web) handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	m.HandleFunc("GET /login", func(w http.ResponseWriter, r *http.Request) { a.render(w, 200, page{View: "login", Title: "Sign in"}) })
	m.HandleFunc("POST /login", a.login)
	m.HandleFunc("POST /admin", a.login)
	m.HandleFunc("POST /logout", a.admin(a.logout))
	m.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, 200, page{View: "public", Title: "Pillar status"})
	})
	m.HandleFunc("GET /api/status", a.publicStatus)
	m.HandleFunc("GET /admin", a.admin(a.dashboard))
	m.HandleFunc("POST /nodes/{id}/producer", a.admin(a.setProducer))
	m.HandleFunc("GET /invitations", a.admin(func(w http.ResponseWriter, r *http.Request) { a.invitations(w, 200, "", "") }))
	m.HandleFunc("GET /nodes/{id}", a.admin(a.dashboard))
	m.HandleFunc("POST /invitations", a.admin(a.invite))
	m.HandleFunc("POST /nodes/{id}/revoke", a.admin(a.revoke))
	m.HandleFunc("GET /install", func(w http.ResponseWriter, r *http.Request) {
		a.render(w, 200, page{View: "install", Title: "Install reporter", Origin: a.origin})
	})
	m.HandleFunc("GET /install.py", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Disposition", `attachment; filename="bridge-health-install.py"`)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write(a.installer)
	})
	m.HandleFunc("GET /downloads/{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name != "bridge-health-reporter-linux-amd64" && name != "bridge-health-reporter-linux-arm64" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		http.ServeFile(w, r, filepath.Join(a.downloads, name))
	})
	m.HandleFunc("POST /v1/enroll", a.enroll)
	m.HandleFunc("GET /assets/ui/{file}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("file")
		if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
			http.NotFound(w, r)
			return
		}
		http.ServeFileFS(w, r, assets, "assets/ui/"+name)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; script-src 'self'; connect-src 'self'; img-src 'self'; font-src 'self'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.Method == "POST" {
			r.Body = http.MaxBytesReader(w, r.Body, 12<<10)
			// Browser management forms must originate on the configured HTTPS site.
			if r.URL.Path != "/v1/enroll" && !a.sameOrigin(r) {
				http.Error(w, "Invalid request origin", 403)
				return
			}
		}
		m.ServeHTTP(w, r)
	})
}

func (a *web) sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	return origin == "https://"+r.Host && (origin == a.origin || origin == a.statusOrigin)
}

func cookie(value string, age int) *http.Cookie {
	return &http.Cookie{Name: sessionCookie, Value: value, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode, MaxAge: age}
}
func (a *web) login(w http.ResponseWriter, r *http.Request) {
	if !a.loginRate.Allow() {
		w.Header().Set("Retry-After", "2")
		a.render(w, 429, page{View: "login", Title: "Sign in", Error: "Too many sign-in attempts. Try again shortly."})
		return
	}
	pass := sha256.Sum256([]byte(r.PostFormValue("password")))
	if subtle.ConstantTimeCompare(pass[:], a.password[:]) != 1 || r.PostFormValue("username") != a.user {
		a.render(w, 401, page{View: "login", Title: "Sign in", Error: "The username or password is incorrect."})
		return
	}
	raw, err := randomToken()
	if err != nil {
		serverError(w, err)
		return
	}
	now := time.Now()
	a.mu.Lock()
	for k, s := range a.sessions {
		if !now.Before(s) {
			delete(a.sessions, k)
		}
	}
	if old, err := r.Cookie(sessionCookie); err == nil {
		delete(a.sessions, digest([]byte(old.Value)))
	}
	a.sessions[digest([]byte(raw))] = now.Add(12 * time.Hour)
	a.mu.Unlock()
	http.SetCookie(w, cookie(raw, 12*60*60))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}
func (a *web) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err == nil {
			a.mu.Lock()
			s, ok := a.sessions[digest([]byte(c.Value))]
			a.mu.Unlock()
			if ok && time.Now().Before(s) {
				next(w, r)
				return
			}
		}
		if r.Method == "GET" && r.URL.Path == "/admin" {
			a.render(w, 200, page{View: "login", Title: "Sign in"})
			return
		}
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	}
}
func (a *web) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie(sessionCookie)
	a.mu.Lock()
	delete(a.sessions, digest([]byte(c.Value)))
	a.mu.Unlock()
	http.SetCookie(w, cookie("", -1))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

type row struct {
	ID, Name, Producer                   string
	InviteExpires                        time.Time
	Enrolled                             bool
	State, Delivery, LocalRead, Snapshot string
	LastSeen, CertExpires                time.Time
	Sequence                             uint64
}
type page struct {
	View, Title, Error, Link, Origin, User string
	Nodes, Invites                         []row
	Detail                                 *row
	Fresh, Stale, Pending                  int
}

func (a *web) render(w http.ResponseWriter, status int, p page) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := pages.ExecuteTemplate(w, "layout", p); err != nil {
		log.Printf("render %s: %v", p.View, err)
	}
}
func serverError(w http.ResponseWriter, err error) {
	log.Printf("receiver: %v", err)
	http.Error(w, "Receiver storage or service error. Check the receiver logs.", 500)
}

func (f *fleet) listNodes() []node {
	f.mu.Lock()
	defer f.mu.Unlock()
	nodes := slices.Clone(f.nodes)
	slices.Reverse(nodes)
	return nodes
}

func adminRow(n node, now time.Time) row {
	return row{ID: n.ID, Name: n.Name, Producer: n.publicProducer(), InviteExpires: n.InviteExpires, Enrolled: len(n.Cert) > 0, State: n.state(now)}
}

func (a *web) dashboard(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	id := r.PathValue("id")
	p := page{View: "overview", Title: "Node overview", User: a.user}
	for _, n := range a.fleet.listNodes() {
		if id != "" && n.ID != id {
			continue
		}
		if len(n.Cert) == 0 {
			if n.state(now) == "Pending" {
				p.Pending++
			}
			continue
		}
		x := adminRow(n, now)
		s, err := collector.StreamStatus(a.fleet.dir, collector.Enrollment{Node: n.Name, Stream: n.ID}, reportFreshness, now)
		if err != nil {
			serverError(w, err)
			return
		}
		c, err := x509.ParseCertificate(n.Cert)
		if err != nil {
			serverError(w, err)
			return
		}
		x.CertExpires = c.NotAfter
		switch {
		case n.Revoked != nil:
			x.Delivery = "Revoked"
		case !now.Before(c.NotAfter):
			x.Delivery = "Certificate expired"
		case s.Delivery == "fresh":
			x.Delivery = "Fresh"
			p.Fresh++
		case s.Delivery == "stale":
			x.Delivery = "Stale"
			p.Stale++
		default:
			x.Delivery = "Awaiting report"
		}
		if s.Latest != nil {
			x.LastSeen = s.Latest.ReceivedAt
			x.LocalRead = s.Latest.Report.LocalRead
			x.Sequence = s.Latest.Report.Sequence
		}
		if id != "" {
			if s.Latest != nil {
				b, err := json.MarshalIndent(s, "", "  ")
				if err != nil {
					serverError(w, err)
					return
				}
				x.Snapshot = string(b)
			}
			p.Detail = &x
			p.View, p.Title = "detail", n.Name
			break
		}
		p.Nodes = append(p.Nodes, x)
	}
	if id != "" && p.Detail == nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, 200, p)
}

func (a *web) invitations(w http.ResponseWriter, status int, link, msg string) {
	p := page{View: "invites", Title: "Invitations", User: a.user, Link: link, Error: msg}
	now := time.Now().UTC()
	for _, n := range a.fleet.listNodes() {
		p.Invites = append(p.Invites, adminRow(n, now))
	}
	a.render(w, status, p)
}

func (a *web) invite(w http.ResponseWriter, r *http.Request) {
	raw, err := a.fleet.invite(r.PostFormValue("name"), time.Now().UTC())
	if errors.Is(err, errName) {
		a.invitations(w, 400, "", "Choose a node name of 1–80 characters. Each active node or pending invitation needs its own name.")
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	a.invitations(w, 201, a.origin+"/install#"+raw, "")
}
func (a *web) revoke(w http.ResponseWriter, r *http.Request) {
	err := a.fleet.revoke(r.PathValue("id"), time.Now().UTC())
	if errors.Is(err, errInactive) {
		http.Error(w, err.Error(), 409)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	http.Redirect(w, r, "/invitations", http.StatusSeeOther)
}

var tokenPattern = regexp.MustCompile(`^[0-9a-f-]{36}\.[A-Za-z0-9_-]{43}$`)

func (a *web) enroll(w http.ResponseWriter, r *http.Request) {
	if !a.enrollRate.Allow() {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "Try again shortly", 429)
		return
	}
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || !tokenPattern.MatchString(token) {
		http.Error(w, errInvite.Error(), 403)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Invalid certificate request size", 400)
		return
	}
	b, _ := pem.Decode(body)
	if b == nil || b.Type != "CERTIFICATE REQUEST" {
		http.Error(w, "Expected a PEM certificate request", 400)
		return
	}
	csr, err := x509.ParseCertificateRequest(b.Bytes)
	if err != nil {
		http.Error(w, "Invalid CSR", 400)
		return
	}
	creds, err := a.fleet.redeem(token, csr, a.endpoint, time.Now().UTC())
	if errors.Is(err, errInvite) {
		http.Error(w, err.Error(), 403)
		return
	}
	if errors.Is(err, errCSR) {
		http.Error(w, err.Error(), 400)
		return
	}
	if err != nil {
		serverError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(creds)
}

func (a *web) setProducer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := a.fleet.setProducer(id, strings.TrimSpace(r.PostFormValue("producer")))
	switch {
	case errors.Is(err, errProducer):
		http.Error(w, err.Error(), http.StatusBadRequest)
	case errors.Is(err, errInactive):
		http.NotFound(w, r)
	case err != nil:
		serverError(w, err)
	default:
		http.Redirect(w, r, "/nodes/"+id, http.StatusSeeOther)
	}
}
