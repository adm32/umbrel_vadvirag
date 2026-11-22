// Vadvirág Irányítópult – rendelések, látogatottság, állapotfigyelés és ntfy értesítések.
//
// Az Umbrel csak bejelentkezés után engedi megnyitni (PROXY_AUTH_ADD), így csak
// a tulajdonos látja. Az adatokat a másik két app belső portjáról kérdezi le,
// amelyek nincsenek kivezetve se a gépre, se az internetre.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
)

const version = "3.0.0"

//go:embed web
var webRoot embed.FS

var (
	budapest, _ = time.LoadLocation("Europe/Budapest")
	dataDir     = env("DATA_DIR", "./data")
	siteStats   = splitList(env("SITE_STATS", "http://vadvirag-weboldal_web_1:9090"))
	ordersDash  = splitList(env("ORDERS_DASH", "http://vadvirag-rendelesek_web_1:9091"))
	ordersPub   = splitList(env("ORDERS_PUBLIC", "http://vadvirag-rendelesek_web_1:8080,http://host.docker.internal:4747"))
	httpc       = &http.Client{Timeout: 6 * time.Second}
)

func env(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimRight(strings.TrimSpace(p), "/"); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func main() {
	os.MkdirAll(dataDir, 0o755)
	loadJSON("beallitasok.json", &settings)
	if settings.Muted == nil {
		settings.Muted = map[string]bool{}
	}
	var p persisted
	loadJSON("allapot.json", &p)
	mon.alerts, mon.lastErrAt, mon.lastSummary = p.Alerts, p.LastErrAt, p.LastSummary
	if p.PendingSent != nil {
		mon.pendingSent = p.PendingSent
	}
	if p.EmailSent != nil {
		mon.emailSent = p.EmailSent
	}
	if mon.lastErrAt.IsZero() {
		mon.lastErrAt = time.Now() // induláskor a régi hibákról nem szólunk
	}

	go func() {
		time.Sleep(3 * time.Second)
		for {
			func() {
				defer func() {
					if v := recover(); v != nil {
						log.Printf("figyelés hiba: %v", v)
					}
				}()
				runChecks()
			}()
			time.Sleep(time.Minute)
		}
	}()

	web, _ := fs.Sub(webRoot, "web")
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/overview", handleOverview)
	mux.HandleFunc("POST /api/settings", handleSettings)
	mux.HandleFunc("GET /api/sessions", func(w http.ResponseWriter, r *http.Request) {
		date := r.URL.Query().Get("date")
		if _, err := time.Parse("2006-01-02", date); err != nil {
			date = time.Now().In(budapest).Format("2006-01-02")
		}
		var out json.RawMessage
		if _, err := getJSON(siteStats, "/sessions?date="+date, &out); err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(out)
	})
	mux.HandleFunc("POST /api/test", handleTest)
	registerCleanup(mux)
	registerAutomation(mux)
	mux.HandleFunc("POST /api/check-now", func(w http.ResponseWriter, r *http.Request) {
		mon.Lock()
		mon.lastPublic = time.Time{}
		mon.Unlock()
		runChecks()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	files := http.FileServer(http.FS(web))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			if b, err := fs.ReadFile(web, "index.html"); err == nil {
				sendStatic(w, r, "index.html", b, "text/html; charset=utf-8", "no-cache")
				return
			}
		}
		w.Header().Set("Cache-Control", "no-cache")
		files.ServeHTTP(w, r)
	}))
	addr := ":" + env("PORT", "8080")
	loadAuth()
	srv := &http.Server{Addr: addr, Handler: requireLogin(mux), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("Vadvirág Irányítópult %s – %s", version, addr)
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
	mon.save()
}
