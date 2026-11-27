package receiver

import (
	"log"
	"net/http"
	"strconv"
	"strings"
	"text/template"
	"time"
)

// A pillar name can contain Markdown punctuation. Keep it in one literal table cell.
var markdownCell = strings.NewReplacer(
	"\\", "\\\\", "|", "\\|", "*", "\\*", "_", "\\_", "`", "\\`",
	"[", "\\[", "]", "\\]", "~", "\\~", "&", "&amp;", "<", "&lt;", ">", "&gt;",
	"\n", " ", "\r", " ",
)

var discovery = template.Must(template.New("discovery").Funcs(template.FuncMap{
	"cell": markdownCell.Replace,
}).ParseFS(assets, "assets/discovery.txt"))

func (o observation) countLabel() string {
	if o.Value == nil {
		return "unknown"
	}
	n := strconv.FormatUint(*o.Value, 10)
	if o.Quality == "partial" {
		return n + " (partial)"
	}
	return n
}

func (p publicNode) StateLabel() string {
	switch {
	case p.State != "" && p.Phase != "" && p.Phase != "running":
		return p.State + " (" + p.Phase + ")"
	case p.State != "":
		return p.State
	case p.Phase != "":
		return p.Phase
	default:
		return "—"
	}
}

func (p publicNode) QueueCounts(network string) string {
	q, ok := p.Queues[network]
	if !ok {
		return "—"
	}
	return q.Wrap.countLabel() + " / " + q.Unwrap.countLabel()
}

func (a *web) publicPage(w http.ResponseWriter, r *http.Request) {
	snapshot, err := a.publicData(time.Now().UTC())
	p := page{View: "public", Title: "Zenon Bridge Pillar Status", Origin: a.canonicalOrigin, TwitterSite: a.twitterSite, Public: snapshot}
	if err != nil {
		log.Printf("public status: %v", err)
		w.Header().Set("Retry-After", "30")
		a.render(w, http.StatusServiceUnavailable, p)
		return
	}
	a.render(w, http.StatusOK, p)
}

func (a *web) discoveryRoutes(m *http.ServeMux) {
	for _, doc := range []struct{ name, mediaType string }{
		{"robots.txt", "text/plain"},
		{"llms.txt", "text/plain"},
		{"sitemap.xml", "application/xml"},
		{"index.md", "text/markdown"},
	} {
		m.HandleFunc("GET /"+doc.name, func(w http.ResponseWriter, r *http.Request) {
			p := page{Origin: a.canonicalOrigin}
			if doc.name == "index.md" {
				var err error
				p.Public, err = a.publicData(time.Now().UTC())
				if err != nil {
					serverError(w, err)
					return
				}
			}
			w.Header().Set("Content-Type", doc.mediaType+"; charset=utf-8")
			if err := discovery.ExecuteTemplate(w, doc.name, p); err != nil {
				log.Printf("render %s: %v", doc.name, err)
			}
		})
	}

	for _, file := range []struct{ name, mediaType string }{
		{"bridge-health.jpg", "image/jpeg"},
		{"favicon.ico", "image/x-icon"},
		{"favicon.svg", "image/svg+xml"},
		{"favicon-32x32.png", "image/png"},
		{"apple-touch-icon.png", "image/png"},
		{"icon-192.png", "image/png"},
		{"icon-512.png", "image/png"},
		{"site.webmanifest", "application/manifest+json"},
	} {
		m.HandleFunc("GET /"+file.name, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", file.mediaType)
			if file.name == "bridge-health.jpg" {
				w.Header().Set("Cross-Origin-Resource-Policy", "cross-origin")
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Cache-Control", "public, max-age=3600")
			http.ServeFileFS(w, r, assets, "assets/share/"+file.name)
		})
	}
}
