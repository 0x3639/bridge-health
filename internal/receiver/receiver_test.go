package receiver

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/wire"
)

const origin = "https://bridgehealth.0x3639.com"
const endpoint = origin + ":8443" + wire.Path

func testReceiver(t *testing.T) *web {
	t.Helper()
	dir := t.TempDir()
	f, err := openFleet(dir)
	if err != nil {
		t.Fatal(err)
	}
	downloads := t.TempDir()
	for _, arch := range []string{"arm64", "amd64"} {
		if err := os.WriteFile(filepath.Join(downloads, "bridge-health-reporter-linux-"+arch), []byte(arch), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err := newWeb(f, origin, "https://bridgestatus.0x3639.com", endpoint, downloads, "admin", "receiver-test-password")
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func operatorKey(t *testing.T) (*ecdsa.PrivateKey, *x509.CertificateRequest, []byte) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{DNSNames: []string{"unrequested.invalid"}}, k)
	if err != nil {
		t.Fatal(err)
	}
	csr, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatal(err)
	}
	return k, csr, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
}

// Serve real TLS and HTTP over in-memory sockets, including keep-alive requests.
type pipeListener struct {
	conns  chan net.Conn
	closed chan struct{}
	once   sync.Once
}

func (p *pipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-p.conns:
		return c, nil
	case <-p.closed:
		return nil, net.ErrClosed
	}
}
func (p *pipeListener) Close() error   { p.once.Do(func() { close(p.closed) }); return nil }
func (p *pipeListener) Addr() net.Addr { return &net.TCPAddr{} }
func (p *pipeListener) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	c, s := net.Pipe()
	select {
	case p.conns <- s:
		return c, nil
	case <-ctx.Done():
		c.Close()
		s.Close()
		return nil, ctx.Err()
	}
}

func TestManagedReceiver(t *testing.T) {
	a := testReceiver(t)
	handler := a.handler()
	var cookie *http.Cookie
	call := func(method, path, body, site, token string, want int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, origin+path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if site != "" {
			r.Header.Set("Origin", site)
		}
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatalf("%s %s: got %d, want %d: %.240s", method, path, w.Code, want, w.Body.String())
		}
		return w
	}
	call("GET", "/", "", "", "", 200)
	call("GET", "/admin", "", "", "", 200)
	call("POST", "/invitations", "name=blocked", origin, "", 303)
	call("POST", "/login", "username=admin&password=wrong", origin, "", 401)
	result := call("POST", "/login", "username=admin&password=receiver-test-password", origin, "", 303)
	cookie = result.Result().Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("session cookie protections missing")
	}
	call("POST", "/invitations", "name=blocked", "https://elsewhere.invalid", "", 403)
	call("GET", "/admin", "", "", "", 200)
	page := call("POST", "/invitations", "name=pilot-01", origin, "", 201).Body.String()
	matches := regexp.MustCompile(regexp.QuoteMeta(origin) + `/install#([a-zA-Z0-9_.-]+)"`).FindStringSubmatch(page)
	if len(matches) != 2 {
		t.Fatal("private URL missing")
	}
	token := matches[1]
	call("GET", "/install", "", "", "", 200)
	call("GET", "/install", "", "", "", 200)
	if strings.Contains(call("GET", "/invitations", "", "", "", 200).Body.String(), token) {
		t.Fatal("secret redisplayed")
	}
	b, err := os.ReadFile(filepath.Join(a.fleet.dir, "fleet.json"))
	if err != nil || bytes.Contains(b, []byte(token)) {
		t.Fatal("invitation not stored as a digest")
	}
	script := call("GET", "/install.py", "", "", "", 200).Body.String()
	for _, arch := range []string{"arm64", "amd64"} {
		bin := call("GET", "/downloads/bridge-health-reporter-linux-"+arch, "", "", "", 200)
		if !strings.Contains(script, digest(bin.Body.Bytes())) || strings.Contains(script, "@@") {
			t.Fatal("installer checksum mismatch")
		}
	}
	call("GET", "/downloads/authority.json", "", "", "", 404)
	key, _, csr := operatorKey(t)
	r := call("POST", "/v1/enroll", string(csr), "", token, 200)
	var bundle credentials
	if err = json.Unmarshal(r.Body.Bytes(), &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Collector != endpoint || !strings.HasPrefix(token, bundle.Stream+".") {
		t.Fatal("wrong assigned stream or endpoint")
	}
	again := call("POST", "/v1/enroll", string(csr), "", token, 200)
	if again.Body.String() != r.Body.String() {
		t.Fatal("same-key retry issued a different certificate")
	}
	_, _, other := operatorKey(t)
	call("POST", "/v1/enroll", string(other), "", token, 403)

	// Restart from disk, then use the credentials returned by the public enrollment endpoint.
	f, err := openFleet(a.fleet.dir)
	if err != nil {
		t.Fatal(err)
	}
	a.fleet = f
	cfg, err := f.ca.serverTLS("bridgehealth.0x3639.com")
	if err != nil {
		t.Fatal(err)
	}
	l := &pipeListener{make(chan net.Conn), make(chan struct{}), sync.Once{}}
	server := &http.Server{Handler: f.collector, TLSConfig: cfg}
	go server.ServeTLS(l, "", "")
	defer server.Close()
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM([]byte(bundle.CA)) {
		t.Fatal("invalid returned CA")
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair([]byte(bundle.Certificate), pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der}))
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || len(leaf.DNSNames) != 0 || leaf.IsCA || len(leaf.ExtKeyUsage) != 1 || leaf.ExtKeyUsage[0] != x509.ExtKeyUsageClientAuth {
		t.Fatal("unexpected certificate scope")
	}
	client := func(certs []tls.Certificate) *http.Client {
		return &http.Client{Timeout: 3 * time.Second, Transport: &http.Transport{DialContext: l.dial,
			TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: certs, MinVersion: tls.VersionTLS13}}}
	}
	guest := client(nil)
	defer guest.CloseIdleConnections()
	if resp, err := guest.Get(endpoint); err == nil {
		resp.Body.Close()
		t.Fatal("ingestion accepted a client without a certificate")
	}
	op := client([]tls.Certificate{pair})
	defer op.CloseIdleConnections()
	body, err := json.Marshal(wire.Report{Version: 1, Stream: bundle.Stream, Sequence: 1, SentAt: time.Now().UTC(), LocalRead: "unreachable"})
	if err != nil {
		t.Fatal(err)
	}
	post := func(want int) {
		t.Helper()
		req, err := http.NewRequest("POST", endpoint, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(wire.LocalReadHeader, "1430")
		res, err := op.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("report returned %d, want %d", res.StatusCode, want)
		}
	}
	post(204)
	detail := call("GET", "/nodes/"+bundle.Stream, "", "", "", 200).Body.String()
	if !strings.Contains(detail, "Fresh") || !strings.Contains(detail, "unreachable") {
		t.Fatal("missing delivery or local socket status")
	}
	public := call("GET", "/api/status", "", "", "", 200).Body.String()
	if !strings.Contains(public, "Socket unreachable") || strings.Contains(public, bundle.Stream) || strings.Contains(public, "certificate") || strings.Contains(public, "readMicros") {
		t.Fatal("invalid public projection", public)
	}
	fixture, err := os.ReadFile("../wire/schemas/testdata/startup.heartbeat.json")
	if err != nil {
		t.Fatal(err)
	}
	var good wire.Report
	now := time.Now().UTC()
	fixture = bytes.ReplaceAll(fixture, []byte("2026-11-21T12:00:01Z"), []byte(now.Format(time.RFC3339Nano)))
	if err := json.Unmarshal(fixture, &good); err != nil {
		t.Fatal(err)
	}
	good.Stream, good.Sequence = bundle.Stream, 2
	body, err = json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	post(204)
	call("POST", "/nodes/"+bundle.Stream+"/producer", "producer=invalid", origin, "", 400)
	const producer = "z1qqrzv35d4wk6n7fkfygay5g2zpgxyaamk84rla"
	call("POST", "/nodes/"+bundle.Stream+"/producer", "producer="+producer, origin, "", 303)
	online := call("GET", "/api/status", "", "", "", 200).Body.String()
	if !strings.Contains(online, `"readMicros":1430`) || !strings.Contains(online, `"status":"Online"`) {
		t.Fatal("successful read timing lost", online)
	}
	if !strings.Contains(online, producer) {
		t.Fatal("public producer edit lost")
	}
	call("POST", "/nodes/"+bundle.Stream+"/revoke", "", origin, "", 303)
	if strings.Contains(call("GET", "/api/status", "", "", "", 200).Body.String(), "pilot-01") {
		t.Fatal("revoked node still public")
	}
	post(403)
	call("POST", "/v1/enroll", string(csr), "", token, 403)
	call("POST", "/logout", "", origin, "", 303)
	call("GET", "/invitations", "", "", "", 303)
	restarted, err := openFleet(f.dir)
	if err != nil || len(restarted.nodes) != 1 || restarted.nodes[0].Revoked == nil {
		t.Fatal("revocation lost on restart")
	}
}

func TestInviteExpiryCancellationAndConcurrentRedemption(t *testing.T) {
	f := testReceiver(t).fleet
	now := time.Now().UTC()
	token, err := f.invite("pilot", now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, _ := operatorKey(t)
	if _, err = f.redeem(token, csr, endpoint, now.Add(24*time.Hour)); !errors.Is(err, errInvite) {
		t.Fatal("expired link accepted")
	}
	if err = f.revoke(f.nodes[0].ID, now); err != nil {
		t.Fatal(err)
	}
	if _, err = f.redeem(token, csr, endpoint, now); !errors.Is(err, errInvite) {
		t.Fatal("cancelled link accepted")
	}
	token, err = f.invite("pilot", now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr2, _ := operatorKey(t)
	results := make(chan error, 2)
	for _, c := range []*x509.CertificateRequest{csr, csr2} {
		go func() { _, err := f.redeem(token, c, endpoint, now); results <- err }()
	}
	first, second := <-results, <-results
	if !((first == nil && errors.Is(second, errInvite)) || (second == nil && errors.Is(first, errInvite))) {
		t.Fatal("expected exactly one winning node key", first, second)
	}
}

func TestStatusOriginAndPublicRoutes(t *testing.T) {
	a := testReceiver(t)
	h := a.handler()
	call := func(method, host, path, site, body string, c *http.Cookie) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, host+path, strings.NewReader(body))
		r.Header.Set("Origin", site)
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if c != nil {
			r.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	site := a.statusOrigin
	loginBody := "username=admin&password=receiver-test-password"
	for _, pair := range [][2]string{{site, origin}, {origin, site}, {site, ""}, {site, "https://elsewhere.invalid"}} {
		if got := call("POST", pair[0], "/admin", pair[1], loginBody, nil).Code; got != 403 {
			t.Fatalf("accepted mismatched origin %v: %d", pair, got)
		}
	}
	login := call("POST", site, "/admin", site, loginBody, nil)
	if login.Code != 303 || login.Header().Get("Location") != "/admin" {
		t.Fatal("status hostname login failed")
	}
	c := login.Result().Cookies()[0]
	for _, session := range []*http.Cookie{nil, c} {
		w := call("GET", site, "/", "", "", session)
		if w.Code != 200 || !strings.Contains(w.Body.String(), `data-view="public"`) || strings.Contains(w.Body.String(), "/admin") {
			t.Fatal("public homepage leaked a management link or required authentication")
		}
	}
	secret := "restricted-pillar"
	w := call("POST", site, "/invitations", site, "name="+secret, c)
	if w.Code != 201 || !strings.Contains(w.Body.String(), origin+"/install#") {
		t.Fatal("wrong installer origin")
	}
	if w := call("GET", site, "/api/status", "", "", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"nodes":[]`) {
		t.Fatal("pending invitation became public")
	}
	for _, path := range []string{"/nodes/unknown", "/invitations"} {
		w := call("GET", site, path, "", "", nil)
		if w.Code != 303 || w.Header().Get("Location") != "/admin" {
			t.Fatalf("unprotected %s", path)
		}
	}
	for _, file := range []string{"app.js", "vue.js", "PublicStatus.js", "Management.js", "app.css", "bridge.svg", "space-grotesk-latin-wght-normal.woff2", "jetbrains-mono-latin-wght-normal.woff2"} {
		w := call("GET", site, "/assets/ui/"+file, "", "", nil)
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatalf("missing asset %s", file)
		}
	}
}
