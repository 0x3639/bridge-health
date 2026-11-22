package receiver

import (
	"encoding/json"
	"encoding/xml"
	"image/jpeg"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPublicDiscovery(t *testing.T) {
	a := testReceiver(t)
	a.canonicalOrigin = a.origin
	a.twitterSite = "@0x3639"
	h := a.handler()
	get := func(path, mediaType string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", a.statusOrigin+path, nil)
		r.Header.Set("Referer", "https://preview.example/")
		r.Header.Set("User-Agent", "Twitterbot/1.0")
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), mediaType) {
			t.Fatalf("%s: status %d, type %q", path, w.Code, w.Header().Get("Content-Type"))
		}
		return w
	}

	home := get("/", "text/html")
	if !strings.Contains(home.Header().Get("Content-Security-Policy"), "manifest-src 'self'") {
		t.Error("manifest blocked by page policy")
	}
	body := home.Body.String()
	for _, want := range []string{`rel="icon" href="/favicon.ico"`, `rel="canonical" href="` + a.canonicalOrigin + `/"`,
		`property="og:image" content="` + a.canonicalOrigin + `/bridge-health.jpg"`,
		`property="og:locale" content="en_US"`, `name="theme-color" content="#151515"`,
		`name="twitter:site" content="@0x3639"`, `name="twitter:image" content="` + a.canonicalOrigin + `/bridge-health.jpg"`,
		`href="/apple-touch-icon.png"`, `href="/favicon-32x32.png"`, `href="/favicon.svg"`, `rel="manifest" href="/site.webmanifest"`,
		`name="twitter:card" content="summary_large_image"`, `href="/llms.txt"`, `href="/index.md"`, "No enrolled pillars yet."} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	_, ld, ok := strings.Cut(body, `<script type="application/ld+json">`)
	ld, _, _ = strings.Cut(ld, "</script>")
	var schema map[string]string
	if !ok || json.Unmarshal([]byte(ld), &schema) != nil || schema["@type"] != "WebPage" || schema["url"] != a.canonicalOrigin+"/" || schema["image"] != a.canonicalOrigin+"/bridge-health.jpg" {
		t.Fatalf("invalid WebPage JSON-LD: %s", ld)
	}
	a.twitterSite = ""
	if strings.Contains(get("/", "text/html").Body.String(), "twitter:site") {
		t.Error("empty Twitter handle advertised")
	}
	robots := get("/robots.txt", "text/plain").Body.String()
	for _, want := range []string{"Sitemap: " + a.canonicalOrigin + "/sitemap.xml", "Disallow: /admin", "Disallow: /api/v1/"} {
		if !strings.Contains(robots, want) {
			t.Errorf("crawler rules missing %q", want)
		}
	}
	var sitemap struct {
		URLs []string `xml:"url>loc"`
	}
	if err := xml.Unmarshal(get("/sitemap.xml", "application/xml").Body.Bytes(), &sitemap); err != nil ||
		len(sitemap.URLs) != 2 || sitemap.URLs[0] != a.canonicalOrigin+"/" || sitemap.URLs[1] != a.canonicalOrigin+"/api/docs" {
		t.Fatalf("sitemap %v, %v", sitemap, err)
	}
	guide := get("/llms.txt", "text/plain").Body.String()
	for _, path := range []string{"/", "/index.md", "/api/status", "/api/docs"} {
		if !strings.Contains(guide, "]("+a.canonicalOrigin+path+")") {
			t.Errorf("LLM guide missing %s", path)
		}
	}
	card := get("/bridge-health.jpg", "image/jpeg")
	if card.Header().Get("Cross-Origin-Resource-Policy") != "cross-origin" || card.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Error("public card image cannot be embedded or read cross-origin")
	}
	image, err := jpeg.Decode(card.Body)
	if err != nil {
		t.Fatal(err)
	}
	if bounds := image.Bounds(); bounds.Dx() != 1200 || bounds.Dy() != 630 {
		t.Fatalf("wrong share image size: %v", bounds)
	}
	req := httptest.NewRequest("HEAD", a.canonicalOrigin+"/bridge-health.jpg", nil)
	req.Header.Set("Referer", "https://preview.example/")
	head := httptest.NewRecorder()
	h.ServeHTTP(head, req)
	if head.Code != 200 || head.Header().Get("Content-Type") != "image/jpeg" || head.Body.Len() != 0 {
		t.Fatalf("unexpected share image HEAD response: %d, %v", head.Code, head.Header())
	}
	icon := get("/favicon.ico", "image/").Body.Bytes()
	if len(icon) < 6 || string(icon[:4]) != "\x00\x00\x01\x00" {
		t.Fatal("invalid favicon")
	}

	for path, size := range map[string]int{"/favicon-32x32.png": 32, "/apple-touch-icon.png": 180, "/icon-192.png": 192, "/icon-512.png": 512} {
		cfg, err := png.DecodeConfig(get(path, "image/png").Body)
		if err != nil || cfg.Width != size || cfg.Height != size {
			t.Fatalf("%s has wrong dimensions: %v (%v)", path, cfg, err)
		}
	}
	var svg struct {
		XMLName xml.Name `xml:"svg"`
	}
	if err := xml.Unmarshal(get("/favicon.svg", "image/svg+xml").Body.Bytes(), &svg); err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		StartURL string `json:"start_url"`
		Icons    []struct{ Src, Sizes, Type string }
	}
	if err := json.Unmarshal(get("/site.webmanifest", "application/manifest+json").Body.Bytes(), &manifest); err != nil || manifest.StartURL != "/" || len(manifest.Icons) != 2 {
		t.Fatalf("invalid manifest: %+v, %v", manifest, err)
	}
	for _, icon := range manifest.Icons {
		get(icon.Src, icon.Type)
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
