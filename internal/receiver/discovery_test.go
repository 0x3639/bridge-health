package receiver

import (
	"encoding/json"
	"encoding/xml"
	"image/jpeg"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicDiscovery(t *testing.T) {
	a := testReceiver(t)
	a.statusOrigin = "https://status.example.org"
	h := a.handler()
	get := func(path, mediaType string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://alternate.example.org"+path, nil))
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), mediaType) {
			t.Fatalf("%s: status %d, type %q", path, w.Code, w.Header().Get("Content-Type"))
		}
		return w
	}

	body := get("/", "text/html").Body.String()
	for _, want := range []string{`rel="icon" href="/favicon.ico"`, `rel="canonical" href="` + a.statusOrigin + `/"`,
		`property="og:image" content="` + a.statusOrigin + `/bridge-health.jpg"`,
		`name="twitter:card" content="summary_large_image"`, `href="/llms.txt"`, `href="/index.md"`, "No enrolled pillars yet."} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	robots := get("/robots.txt", "text/plain").Body.String()
	for _, want := range []string{"Sitemap: " + a.statusOrigin + "/sitemap.xml", "Disallow: /admin", "Disallow: /api/v1/"} {
		if !strings.Contains(robots, want) {
			t.Errorf("crawler rules missing %q", want)
		}
	}
	var sitemap struct {
		URLs []string `xml:"url>loc"`
	}
	if err := xml.Unmarshal(get("/sitemap.xml", "application/xml").Body.Bytes(), &sitemap); err != nil ||
		len(sitemap.URLs) != 2 || sitemap.URLs[0] != a.statusOrigin+"/" || sitemap.URLs[1] != a.statusOrigin+"/api/docs" {
		t.Fatalf("sitemap %v, %v", sitemap, err)
	}
	guide := get("/llms.txt", "text/plain").Body.String()
	for _, path := range []string{"/", "/index.md", "/api/status", "/api/docs"} {
		if !strings.Contains(guide, "]("+a.statusOrigin+path+")") {
			t.Errorf("LLM guide missing %s", path)
		}
	}
	img, err := jpeg.DecodeConfig(get("/bridge-health.jpg", "image/jpeg").Body)
	if err != nil || img.Width != 1200 || img.Height != 630 {
		t.Fatalf("share image %v, %v", img, err)
	}
	icon := get("/favicon.ico", "image/").Body.Bytes()
	if len(icon) < 6 || string(icon[:4]) != "\x00\x00\x01\x00" {
		t.Fatal("invalid favicon")
	}

	now := time.Now().UTC()
	invite, err := a.fleet.invite("Pillar | <east> 192.0.2.7", now)
	if err != nil {
		t.Fatal(err)
	}
	_, csr, _ := operatorKey(t)
	creds, err := a.fleet.redeem(invite, csr, endpoint, now)
	if err != nil {
		t.Fatal(err)
	}
	body = get("/", "text/html").Body.String()
	if !strings.Contains(body, `role="region" aria-label="Pillar status table" tabindex="0"><table class="status-table">`) {
		t.Error("server-rendered pillar table needs a named, keyboard-focusable scroll region")
	}
	markdown := get("/index.md", "text/markdown").Body.String()
	if !strings.Contains(body, `<th scope="row" class="pillar">Pillar | &lt;east&gt; [redacted]</th>`) ||
		!strings.Contains(markdown, `| Pillar \| &lt;east&gt; \[redacted\] | Awaiting report |`) {
		t.Fatal("missing literal pillar name or status in server-rendered output")
	}
	_, payload, ok := strings.Cut(body, `id="public-snapshot">`)
	payload, _, _ = strings.Cut(payload, "</script>")
	api := get("/api/status", "application/json").Body.String()
	if !ok || !json.Valid([]byte(payload)) || strings.TrimSpace(payload) != strings.TrimSpace(api) {
		t.Fatal("Vue bootstrap and public JSON differ")
	}
	for _, s := range []string{body, api, markdown} {
		if strings.Contains(s, creds.Stream) || strings.Contains(s, "192.0.2.7") {
			t.Fatal("private data escaped public projection")
		}
	}
	if err := a.fleet.revoke(creds.Stream, now); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/", "/index.md"} {
		if !strings.Contains(get(path, "text/").Body.String(), "No enrolled pillars yet.") {
			t.Fatalf("%s retained a revoked pillar", path)
		}
	}
}
