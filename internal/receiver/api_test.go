package receiver

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/0x3639/bridge-health/internal/collector"
	"github.com/0x3639/bridge-health/internal/localstate"
	"github.com/0x3639/bridge-health/internal/wire"
)

func TestAPITokenLifecycle(t *testing.T) {
	a := testReceiver(t)
	h := a.handler()
	var session *http.Cookie
	call := func(method, path, form, site string, headers []string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, origin+path, strings.NewReader(form))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", site)
		req.Header["Authorization"] = headers
		if session != nil {
			req.AddCookie(session)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != want || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s: status %d, cache %q", method, req.URL.Path, w.Code, w.Header().Get("Cache-Control"))
		}
		return w
	}
	const manage = "/admin/tokens"
	const status = "/api/v1/status"
	const form = "name=dashboard&days=90"
	call("POST", manage, form, origin, nil, 303)
	session = call("POST", "/admin", "username=admin&password=receiver-test-password", origin, nil, 303).Result().Cookies()[0]
	call("POST", manage, form, "https://elsewhere.invalid", nil, 403)
	call("POST", manage, "name=dashboard&days=0", origin, nil, 400)
	call("POST", manage, "name=&days=30", origin, nil, 400)
	issued := call("POST", manage, form, origin, nil, 201)
	secret := regexp.MustCompile(`"IssuedToken":"(bhs_[A-Za-z0-9_-]{43})"`).FindStringSubmatch(issued.Body.String())
	if len(secret) != 2 {
		t.Fatal("creation response did not contain a bearer token")
	}
	raw := secret[1]
	tokens := a.tokens.list()
	if len(tokens) != 1 || tokens[0].Name != "dashboard" || tokens[0].Expires.Sub(tokens[0].Created) != 90*24*time.Hour {
		t.Fatal("invalid token metadata")
	}
	stored, err := os.ReadFile(filepath.Join(a.fleet.dir, "api-tokens.json"))
	if err != nil || strings.Contains(string(stored), raw) || !strings.Contains(string(stored), digest([]byte(raw))) {
		t.Fatal("token storage")
	}
	list := call("GET", manage, "", "", nil, 200).Body.String()
	if strings.Contains(list, raw) || strings.Contains(list, digest([]byte(raw))) || !strings.Contains(list, tokens[0].ID) {
		t.Fatal("listed token metadata")
	}
	restart := func() {
		t.Helper()
		var err error
		a, err = newWeb(a.fleet, a.origin, a.statusOrigin, a.endpoint, a.downloads, a.user, "receiver-test-password")
		if err != nil {
			t.Fatal(err)
		}
		h = a.handler()
		session = nil
	}
	bearer := []string{"Bearer " + raw}
	if w := call("GET", status, "", "", nil, 401); w.Header().Get("WWW-Authenticate") == "" {
		t.Fatal("missing bearer challenge")
	}
	call("GET", status+"?token="+raw, "", "", nil, 401)
	call("GET", status, "", "", []string{"Bearer " + session.Value}, 401)
	call("GET", status, "", "", []string{"Basic " + raw}, 401)
	call("GET", status, "", "", []string{bearer[0], bearer[0]}, 401)
	call("GET", status+"/unknown", "", "", nil, 401)
	restart()
	call("GET", status+"/unknown", "", "", bearer, 404)
	w := call("GET", status, "", "", []string{"bearer " + raw}, 200)
	if !strings.Contains(w.Body.String(), `"nodes":[]`) || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
		t.Fatal("empty fleet response")
	}
	call("GET", manage, "", "", bearer, 303)
	call("POST", manage, form, origin, bearer, 303)
	call("POST", "/v1/enroll", "", "", bearer, 403)
	for i := 0; i < 8; i++ {
		call("GET", status, "", "", bearer, 200)
	}
	if w := call("GET", status, "", "", bearer, 429); w.Header().Get("Retry-After") == "" {
		t.Fatal("rate limit missing retry interval")
	}
	if a.tokens.authorize(raw, tokens[0].Expires) != http.StatusUnauthorized {
		t.Fatal("token accepted at expiry")
	}
	session = call("POST", "/admin", "username=admin&password=receiver-test-password", origin, nil, 303).Result().Cookies()[0]
	path := manage + "/" + tokens[0].ID + "/revoke"
	call("POST", path, "", "https://elsewhere.invalid", nil, 403)
	call("POST", path, "", origin, nil, 303)
	call("GET", status, "", "", bearer, 401)
	restart()
	call("GET", status, "", "", bearer, 401)
}

func TestAPIStatusAndHistory(t *testing.T) {
	a := testReceiver(t)
	now := time.Now().UTC()
	secret, err := a.tokens.issue("monitor", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	name := "pillar-192.0.2.7"
	invite, err := a.fleet.invite(name, now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, _ := operatorKey(t)
	creds, err := a.fleet.redeem(invite, csr, endpoint, now)
	if err != nil {
		t.Fatal(err)
	}
	producer := explorerProducers["nomlabz.org"]
	if err := a.fleet.setProducer(creds.Stream, producer); err != nil {
		t.Fatal(err)
	}
	if _, err := a.fleet.invite("pending-private", now); err != nil {
		t.Fatal(err)
	}
	h := a.handler()
	read := func(path string) string {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/v1/status"+path, nil)
		req.Header.Set("Authorization", "Bearer "+secret)
		req.Header.Set("X-Forwarded-For", "198.51.100.88")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		body := w.Body.String()
		if w.Code != 200 {
			t.Fatalf("status %d: %s", w.Code, body)
		}
		for _, private := range []string{"192.0.2.7", "2001:db8", "198.51.100", "clientCertificate", "tokenHash", "pending-private", invite, secret, "rpcEndpoint"} {
			if strings.Contains(body, private) {
				t.Fatal("private field present in response")
			}
		}
		return body
	}
	list := read("")
	if !strings.Contains(list, `"status":"Awaiting report"`) || !strings.Contains(list, `"latest":null`) {
		t.Fatal("missing waiting reporter")
	}
	b, err := os.ReadFile("../wire/schemas/testdata/startup.heartbeat.json")
	if err != nil {
		t.Fatal(err)
	}
	var success wire.Report
	if err := json.Unmarshal(b, &success); err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal(success.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	snap["build"].(map[string]any)["version"] = "release@[2001:db8::42]"
	snap["networksKnown"] = true
	asOf := "2026-11-21T12:00:01Z"
	obs := func(value any) any {
		return map[string]any{"value": value, "asOf": asOf, "quality": "complete"}
	}
	work := map[string]any{"awaitingFinality": obs(0), "unsignedObserved": obs(2), "selected": obs(map[string]int{"window": 7, "count": 1}),
		"signedAwaitingSubmission": obs(3), "oldestPendingSince": obs(asOf), "lastSignedWindow": obs(6)}
	network := map[string]any{"chainId": 73405, "networkClass": 2, "nodeSync": obs("synced"), "headHeight": obs(987654),
		"scannedThrough": obs(987650), "lastRangeAt": asOf, "wrap": work, "unwrap": work}
	snap["networks"] = []any{network}
	// Simulate an extra field in a future stored schema. It must not be exported.
	network["rpcEndpoint"] = "https://198.51.100.88"
	success.Snapshot, err = json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	success.Stream = creds.Stream
	micros := uint32(1234)
	good := collector.Stored{Node: name, ReceivedAt: now.Add(-time.Minute), ReadMicros: &micros, Digest: "original-digest", Report: success}
	failed := collector.Stored{Node: name, ReceivedAt: now, Digest: "second-digest", Report: wire.Report{
		Version: 1, Stream: creds.Stream, Sequence: 2, SentAt: now, LocalRead: "timeout", LastSuccess: &good.ReceivedAt}}
	saved := struct {
		collector.Stored
		History           []collector.Stored `json:"history"`
		HistoricalSuccess *collector.Stored  `json:"historicalSuccess"`
	}{failed, []collector.Stored{good}, &good}
	if err := localstate.Write(filepath.Join(a.fleet.dir, creds.Stream+".json"), saved); err != nil {
		t.Fatal(err)
	}
	list = read("")
	for _, want := range []string{producer, creds.Stream, `"delivery":"fresh"`, `"status":"Unavailable"`, `"historicalSuccess"`, `"readMicros":1234`, `"snapshot":null`, "pillar-[redacted]"} {
		if !strings.Contains(list, want) {
			t.Fatalf("missing %s in fleet response", want)
		}
	}
	var detail struct {
		Node struct {
			collector.Status
			Access string
		}
		History []collector.Stored
	}
	if err := json.Unmarshal([]byte(read("/"+creds.Stream)), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.History) != 1 || detail.History[0].Report.Sequence != 1 || detail.Node.Latest.Report.Sequence != 2 || detail.Node.Access != "active" {
		t.Fatal("unexpected stream history")
	}
	delete(network, "rpcEndpoint")
	snap["build"].(map[string]any)["version"] = "release@[[redacted]]"
	wantJSON, err := json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	var actual, expected any
	if err := json.Unmarshal(wantJSON, &expected); err != nil {
		t.Fatal(err)
	}
	for _, r := range []*collector.Stored{&detail.History[0], detail.Node.HistoricalSuccess} {
		if r == nil || json.Unmarshal(r.Report.Snapshot, &actual) != nil || !reflect.DeepEqual(actual, expected) {
			t.Fatal("snapshot projection lost telemetry")
		}
	}
	if err := a.fleet.revoke(creds.Stream, now); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(read(""), `"access":"revoked"`) {
		t.Fatal("retired stream missing")
	}
}

func TestAPIStatusProjectsReceiveHold(t *testing.T) {
	a := testReceiver(t)
	now := time.Now().UTC()
	secret, err := a.tokens.issue("monitor", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	invite, err := a.fleet.invite("held-pillar", now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, _ := operatorKey(t)
	creds, err := a.fleet.redeem(invite, csr, endpoint, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("../wire/schemas/testdata/startup.heartbeat.json")
	if err != nil {
		t.Fatal(err)
	}
	var report wire.Report
	if err := json.Unmarshal(b, &report); err != nil {
		t.Fatal(err)
	}
	var snap map[string]any
	if err := json.Unmarshal(report.Snapshot, &snap); err != nil {
		t.Fatal(err)
	}
	zenon := snap["zenon"].(map[string]any)
	zenon["receiveHold"] = map[string]any{
		"height":   16221,
		"since":    "2026-11-21T12:00:00Z",
		"attempts": 3,
		"cause":    "private-error",
	}
	report.Snapshot, err = json.Marshal(snap)
	if err != nil {
		t.Fatal(err)
	}
	report.Stream = creds.Stream
	stored := collector.Stored{Node: "held-pillar", ReceivedAt: now, Digest: "held-digest", Report: report}
	if err := localstate.Write(filepath.Join(a.fleet.dir, creds.Stream+".json"), stored); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/api/v1/status/"+creds.Stream, nil)
	req.Header.Set("Authorization", "Bearer "+secret)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, req)
	body := w.Body.String()
	if w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, body)
	}
	if strings.Contains(body, "private-error") {
		t.Fatalf("extra receiveHold field leaked into API response: %s", body)
	}
	for _, want := range []string{`"receiveHold":{"height":16221,"since":"2026-11-21T12:00:00Z","attempts":3}`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in response: %s", want, body)
		}
	}
}

func TestStatusAddressPrivacy(t *testing.T) {
	for _, ip := range []string{"192.0.2.17", "010.000.002.017", "999.2.3.192.0.2.1", "2001:db8::4", "::1", "::ffff:192.0.2.1", "fe80::3%eth0", "2001:0DB8:0000:0000:0000:0000:0000:0001", "0:0:0:0:0:ffff:192.0.2.1"} {
		for _, field := range []string{ip, "node-" + ip, "https://[" + ip + "]:8443/path", "/ip6/" + ip + "/tcp/443"} {
			if got := redactIPText(field); strings.Contains(got, ip) || !strings.Contains(got, "[redacted]") {
				t.Errorf("IP literal survived: %s", field)
			}
		}
	}
	for _, field := range []string{"NoMLabz.org", "v0.0.9a", "1.22.12", "2026.11.22.1", "release-0x3639", "0x3639.com"} {
		if got := redactIPText(field); got != field {
			t.Errorf("modified %s", field)
		}
	}
}

func TestPublicAPIGuide(t *testing.T) {
	a := testReceiver(t)
	w := httptest.NewRecorder()
	a.handler().ServeHTTP(w, httptest.NewRequest("GET", "/api/docs", nil))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("public guide returned %d, %s", w.Code, w.Header().Get("Content-Type"))
	}
	body := w.Body.String()
	for _, want := range []string{a.statusOrigin + "/api/v1/status", "Authorization: Bearer", "/api/v1/status/{streamId}", "Retry-After"} {
		if !strings.Contains(body, want) {
			t.Errorf("guide missing %q", want)
		}
	}
	if strings.Contains(body, "<script") {
		t.Fatal("guide should render without loading an app or embedding session data")
	}
}

func TestAPITokenRevokeWriteFailures(t *testing.T) {
	dir := t.TempDir()
	s, err := openAPITokens(dir)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	raw, err := s.issue("monitor", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	id := s.list()[0].ID

	// A rename onto a directory fails before the live file can be replaced.
	path := s.path
	s.path = dir
	err = s.revoke(id)
	s.path = path
	if err == nil || s.authorize(raw, now) != http.StatusOK {
		t.Fatal("failed rename changed live authorization")
	}
	reopened, err := openAPITokens(dir)
	if err != nil || reopened.authorize(raw, now) != http.StatusOK {
		t.Fatal("failed rename changed persisted authorization", err)
	}

	// Use the real file replacement but fail the following directory sync.
	syncErr := errors.New("directory sync failed")
	s.syncDir = func(string) error { return syncErr }
	if err := s.revoke(id); !errors.Is(err, syncErr) {
		t.Fatalf("revoke returned %v, want directory sync error", err)
	}
	if s.authorize(raw, now) != http.StatusUnauthorized {
		t.Fatal("revoked token still authorized after rename")
	}
	s.syncDir = localstate.SyncDirectory
	replacement, err := s.issue("replacement", time.Hour, now)
	if err != nil {
		t.Fatal(err)
	}
	reopened, err = openAPITokens(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.authorize(raw, now) != http.StatusUnauthorized || reopened.authorize(replacement, now) != http.StatusOK {
		t.Fatal("later write restored revoked access or lost the replacement")
	}
}
