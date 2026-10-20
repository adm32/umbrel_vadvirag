// Vadvirág Étterem – weboldal.
//
// Egyetlen kis program: kiszolgálja a weboldalt (a site/ mappa bele van fordítva),
// az étlapot élőben a rendeléskezelőtől kéri le (ha az nem elérhető, a beépített
// másolatot mutatja), és a /rendeles oldalt is továbbítja a rendeléskezelőhöz,
// így a vendég végig a saját domainen marad.
package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed site
var siteRoot embed.FS

//go:embed menu-fallback.json
var fallbackMenu []byte

const version = "1.2.0"

var (
	site    fs.FS
	siteURL string

	// A rendeléskezelő címe. Umbrelen a konténerek közös hálózaton vannak; ha az
	// ottani név nem érhető el, a gépen futó 4747-es portot próbáljuk.
	upMu       sync.RWMutex
	upstream   *url.URL
	candidates []*url.URL
)

func orderURL() *url.URL { upMu.RLock(); defer upMu.RUnlock(); return upstream }

// findUpstream végigpróbálja a lehetséges címeket, és az első működőt választja.
func findUpstream() {
	c := &http.Client{Timeout: 2 * time.Second}
	for _, u := range candidates {
		resp, err := c.Get(u.String() + "/healthz")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				upMu.Lock()
				changed := upstream == nil || upstream.String() != u.String()
				upstream = u
				upMu.Unlock()
				if changed {
					log.Printf("rendeléskezelő: %s", u)
				}
				return
			}
		}
	}
}

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func main() {
	var err error
	site, err = fs.Sub(siteRoot, "site")
	if err != nil {
		log.Fatal(err)
	}
	for _, raw := range strings.Split(env("ORDER_URL", "http://vadvirag-rendelesek_web_1:8080,http://host.docker.internal:4747,http://172.17.0.1:4747"), ",") {
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil || u.Host == "" {
			log.Fatal("ORDER_URL hibás: ", raw)
		}
		candidates = append(candidates, u)
	}
	upstream = candidates[0]
	findUpstream()
	go func() {
		for range time.Tick(15 * time.Second) {
			findUpstream()
		}
	}()
	siteURL = strings.TrimRight(env("SITE_URL", "https://www.vadviragetterem.com"), "/")
	statsInit()
	statsServer()
	addr := ":" + env("PORT", "8080")

	srv := &http.Server{Addr: addr, Handler: routes(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second}
	go func() {
		log.Printf("Vadvirág weboldal %s – %s (rendeléskezelő: %s)", version, addr, orderURL())
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	statsSave(true)
}

func routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /api/menu", handleMenu)
	mux.HandleFunc("POST /api/t", handleBeacon)
	mux.HandleFunc("GET /robots.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, "User-agent: *\nAllow: /\nDisallow: /api/\nSitemap: "+siteURL+"/sitemap.xml\n")
	})
	mux.HandleFunc("GET /sitemap.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9" xmlns:xhtml="http://www.w3.org/1999/xhtml">`)
		alts := `<xhtml:link rel="alternate" hreflang="hu" href="` + siteURL + `/"/><xhtml:link rel="alternate" hreflang="en" href="` + siteURL + `/en"/><xhtml:link rel="alternate" hreflang="de" href="` + siteURL + `/de"/>`
		for _, p := range []string{"/", "/en", "/de"} {
			b.WriteString("<url><loc>" + siteURL + p + "</loc>" + alts + "</url>")
		}
		b.WriteString(`<url><loc>` + siteURL + `/rendeles</loc></url></urlset>`)
		io.WriteString(w, b.String())
	})

	// A rendelési oldal és az API-ja a rendeléskezelőből jön, a vendég ebből semmit sem vesz észre.
	px := orderProxy()
	for _, p := range []string{"/rendeles", "/api/public/", "/logo.png", "/icon-192.png"} {
		mux.Handle(p, px)
	}

	mux.HandleFunc("/", serveStatic)
	return errorMW(gzipMW(mux))
}

// ---------------------------------------------------------------- statikus fájlok

func secHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
	h.Set("X-Frame-Options", "SAMEORIGIN")
	h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
	h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; font-src https://fonts.gstatic.com; script-src 'self'; connect-src 'self'; frame-ancestors 'self'; base-uri 'self'; form-action 'self'")
}

var langPages = map[string]bool{"/": true, "/en": true, "/de": true, "/hu": true, "/index.html": true}

func serveStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := path.Clean("/" + r.URL.Path)
	if p != "/" && strings.HasSuffix(p, "/") {
		p = strings.TrimRight(p, "/")
	}
	secHeaders(w)
	if langPages[p] || langPages[strings.TrimSuffix(p, "/")] {
		// a Chrome/Edge/Samsung böngészők ezután megmondják a telefon pontos típusát és a rendszer verzióját
		w.Header().Set("Accept-CH", "Sec-CH-UA-Model, Sec-CH-UA-Platform-Version")
		sendFile(w, r, "index.html", "no-cache")
		return
	}
	name := strings.TrimPrefix(p, "/")
	if st, err := fs.Stat(site, name); err != nil || st.IsDir() || strings.HasPrefix(path.Base(name), ".") {
		w.WriteHeader(http.StatusNotFound)
		sendFile(w, r, "404.html", "no-cache")
		return
	}
	cache := "public, max-age=3600"
	switch {
	case strings.HasPrefix(name, "img/"), strings.HasPrefix(name, "fonts/"):
		cache = "public, max-age=2592000"
	case r.URL.Query().Get("v") != "":
		cache = "public, max-age=31536000, immutable"
	case strings.HasSuffix(name, ".json"):
		cache = "public, max-age=300"
	}
	sendFile(w, r, name, cache)
}

var types = map[string]string{
	".html": "text/html; charset=utf-8", ".css": "text/css; charset=utf-8", ".js": "text/javascript; charset=utf-8",
	".json": "application/json; charset=utf-8", ".webp": "image/webp", ".png": "image/png", ".jpg": "image/jpeg",
	".svg": "image/svg+xml", ".ico": "image/x-icon", ".webmanifest": "application/manifest+json", ".woff2": "font/woff2", ".txt": "text/plain; charset=utf-8",
}

var started = time.Now()

// A HTML-ben a CSS/JS hivatkozások "?v=" értéke a fájl tartalmának ujjlenyomata,
// így minden frissítés után a böngésző és a Cloudflare is az új fájlt tölti le
// (a régit egy évig tárolhatják, mert az új címet kap).
var (
	htmlOnce sync.Once
	htmlMap  = map[string][]byte{}
	assetRe  = regexp.MustCompile(`(/[A-Za-z0-9_.-]+\.(?:css|js))\?v=[A-Za-z0-9]+`)
)

func fingerprintHTML() {
	entries, _ := fs.ReadDir(site, ".")
	for _, e := range entries {
		if e.IsDir() || path.Ext(e.Name()) != ".html" {
			continue
		}
		b, err := fs.ReadFile(site, e.Name())
		if err != nil {
			continue
		}
		htmlMap[e.Name()] = assetRe.ReplaceAllFunc(b, func(m []byte) []byte {
			p := assetRe.FindSubmatch(m)[1]
			data, err := fs.ReadFile(site, strings.TrimPrefix(string(p), "/"))
			if err != nil {
				return m
			}
			h := sha256.Sum256(data)
			return []byte(string(p) + "?v=" + hex.EncodeToString(h[:5]))
		})
	}
}

func sendFile(w http.ResponseWriter, r *http.Request, name, cache string) {
	htmlOnce.Do(fingerprintHTML)
	data, ok := htmlMap[name]
	if !ok {
		var err error
		data, err = fs.ReadFile(site, name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
	}
	if ct := types[path.Ext(name)]; ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", cache)
	http.ServeContent(w, r, name, started, bytes.NewReader(data))
}

// ---------------------------------------------------------------- étlap (élő, tartalékkal)

var (
	menuMu    sync.Mutex
	menuCache []byte
	menuAt    time.Time
	client    = &http.Client{Timeout: 4 * time.Second}
	menuRetry time.Time
)

func handleMenu(w http.ResponseWriter, r *http.Request) {
	menuMu.Lock()
	defer menuMu.Unlock()
	// Ha a rendeléskezelő nem elérhető, 15 mp-ig nem próbáljuk újra, hogy a
	// látogatók ne várjanak egymás után a lejáró kérésekre.
	if (menuCache == nil || time.Since(menuAt) > 30*time.Second) && time.Now().After(menuRetry) {
		if b, err := fetchMenu(); err == nil {
			menuCache, menuAt = b, time.Now()
		} else {
			log.Printf("étlap: a rendeléskezelő nem elérhető (%v) – a tárolt / beépített étlap látszik", err)
			menuRetry = time.Now().Add(15 * time.Second)
			go findUpstream()
			if menuCache == nil || time.Since(menuAt) > 10*time.Minute {
				menuCache, menuAt = fallbackMenu, time.Now().Add(-30*time.Second)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(menuCache)
}

func fetchMenu() ([]byte, error) {
	resp, err := client.Get(orderURL().String() + "/api/public/menu")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, &net.AddrError{Err: resp.Status, Addr: orderURL().Host}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil || !bytes.Contains(b, []byte(`"sections"`)) {
		return nil, io.ErrUnexpectedEOF
	}
	// jelöljük, hogy élő adat
	return append([]byte(`{"live":true,`), bytes.TrimPrefix(bytes.TrimSpace(b), []byte("{"))...), nil
}

// ---------------------------------------------------------------- továbbítás a rendeléskezelőhöz

func orderProxy() http.Handler {
	px := &httputil.ReverseProxy{}
	px.Director = func(r *http.Request) {
		u := orderURL()
		host := r.Host
		r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
		r.Host = u.Host
		r.Header.Set("X-Forwarded-Host", host)
		r.Header.Del("Tailscale-Funnel-Request")
		if _, ok := r.Header["User-Agent"]; !ok {
			r.Header.Set("User-Agent", "")
		}
	}
	px.ModifyResponse = func(resp *http.Response) error {
		resp.Header.Set("X-Vadvirag-Site", "1")
		return nil
	}
	px.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Printf("rendeléskezelő nem elérhető: %v", err)
		noteServerError("upstream", "A rendeléskezelő nem érhető el: "+err.Error(), r.URL.Path)
		go findUpstream()
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			io.WriteString(w, `{"error":"Az online rendelés most nem elérhető. Kérjük, rendelj telefonon: 06 20 339 5099"}`)
			return
		}
		secHeaders(w)
		w.WriteHeader(http.StatusBadGateway)
		sendFile(w, r, "offline.html", "no-cache")
	}
	return px
}

// ---------------------------------------------------------------- gzip

type gzw struct {
	http.ResponseWriter
	gz      *gzip.Writer
	decided bool
	use     bool
}

func (g *gzw) WriteHeader(code int) {
	if !g.decided {
		g.decided = true
		ct := g.Header().Get("Content-Type")
		g.use = code != http.StatusNotModified && g.Header().Get("Content-Encoding") == "" &&
			(strings.HasPrefix(ct, "text/") || strings.Contains(ct, "json") || strings.Contains(ct, "javascript") || strings.Contains(ct, "svg"))
		if g.use {
			g.Header().Del("Content-Length")
			g.Header().Set("Content-Encoding", "gzip")
			g.Header().Add("Vary", "Accept-Encoding")
			g.gz = gzip.NewWriter(g.ResponseWriter)
		}
	}
	g.ResponseWriter.WriteHeader(code)
}

func (g *gzw) Write(b []byte) (int, error) {
	if !g.decided {
		g.WriteHeader(http.StatusOK)
	}
	if g.use {
		return g.gz.Write(b)
	}
	return g.ResponseWriter.Write(b)
}

func (g *gzw) Flush() {
	if g.gz != nil {
		g.gz.Flush()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func gzipMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		g := &gzw{ResponseWriter: w}
		next.ServeHTTP(g, r)
		if g.gz != nil {
			g.gz.Close()
		}
	})
}
