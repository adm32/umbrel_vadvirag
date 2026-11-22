// Vadvirág Rendelések – HTTP: belépés, munkamenet, a kezelőprogram API-ja és az útvonalak (routes).

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- HTTP

func (a *App) token() string { return sessionToken(a.conf().Titok) }

func (a *App) setSession(w http.ResponseWriter, tok string) {
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: tok, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Expires: time.Now().Add(90 * 24 * time.Hour)})
}

func sessionToken(titok string) string {
	m := hmac.New(sha256.New, []byte(titok))
	m.Write([]byte("vadvirag-session"))
	return hex.EncodeToString(m.Sum(nil))
}

func (a *App) authed(r *http.Request) bool {
	if a.needsSetup() {
		return false
	}
	c, err := r.Cookie("sid")
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(a.token())) == 1
}

func (a *App) requireAuth(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !a.authed(r) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func lanURLs(port int) []string {
	var out []string
	addrs, _ := net.InterfaceAddrs()
	for _, ad := range addrs {
		if ipn, ok := ad.(*net.IPNet); ok && !ipn.IP.IsLoopback() {
			if ip4 := ipn.IP.To4(); ip4 != nil {
				out = append(out, fmt.Sprintf("http://%s:%d", ip4, port))
			}
		}
	}
	return out
}

type OrderView struct {
	*Order
	Mail *MailDraft `json:"mail,omitempty"`
}

func (a *App) handleList(w http.ResponseWriter, r *http.Request) {
	preNew, preToday := a.preCounts()
	a.st.mu.Lock()
	views := make([]OrderView, 0, len(a.st.Orders))
	for _, o := range a.st.Orders {
		cp := *o
		views = append(views, OrderView{&cp, a.draft(&cp)})
	}
	a.st.mu.Unlock()
	a.wixMu.Lock()
	wix := map[string]any{"enabled": a.conf().Wix.APIKulcs != "", "error": a.wixErr}
	if !a.wixLastOK.IsZero() {
		wix["last_ok"] = a.wixLastOK
	}
	a.wixMu.Unlock()
	writeJSON(w, 200, map[string]any{
		"orders": views,
		"info": map[string]any{
			"restaurant":    a.conf().EtteremNeve,
			"lan":           a.lan(),
			"smtp":          a.smtpEnabled(),
			"wix":           wix,
			"eta":           a.conf().IdoOpciok,
			"reasons":       a.conf().Okok,
			"cities":        a.cityNames(),
			"now":           time.Now(),
			"online_paused": a.conf().OnlineSzunetel,
			"online_note":   a.conf().OnlineUzenet,
			"messages":      a.unreadMessages(),
			"preorder_on":   preOff != nil,
			"closed_today":  a.closedToday(),
			"preorders":     preNew,
			"preorders_day": preToday,
		},
	})
}

func (a *App) lan() []string {
	if a.container {
		return nil // a konténer belső címei a felhasználónak nem használhatók
	}
	return lanURLs(a.conf().Port)
}

func (a *App) cityNames() []string {
	var out []string
	for n := range a.conf().Dijak {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func (a *App) orderID(r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	return id, err == nil
}

func (a *App) handleAccept(w http.ResponseWriter, r *http.Request) {
	id, ok := a.orderID(r)
	var in struct {
		ETA int `json:"eta"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in)
	a.st.mu.Lock()
	o := a.find(id)
	if !ok || o == nil {
		a.st.mu.Unlock()
		writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
		return
	}
	o.Status, o.ETA, o.Reason, o.StatusAt = StPreparing, in.ETA, "", time.Now()
	o.AcceptedAt = o.StatusAt
	o.EmailState = ""
	a.st.saveLocked()
	a.st.mu.Unlock()
	go a.emailAsync(id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleReject(w http.ResponseWriter, r *http.Request) {
	id, ok := a.orderID(r)
	var in struct {
		Reason string `json:"reason"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in)
	a.st.mu.Lock()
	o := a.find(id)
	if !ok || o == nil {
		a.st.mu.Unlock()
		writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
		return
	}
	o.Status, o.ETA, o.Reason, o.StatusAt = StRejected, 0, strings.TrimSpace(in.Reason), time.Now()
	o.EmailState = ""
	a.st.saveLocked()
	a.st.mu.Unlock()
	go a.emailAsync(id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleSimple(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := a.orderID(r)
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		o := a.find(id)
		if !ok || o == nil {
			writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
			return
		}
		o.Status, o.StatusAt = status, time.Now()
		if status == StNew {
			o.ETA, o.Reason, o.EmailState, o.EmailErr = 0, "", "", ""
			o.AcceptedAt = time.Time{}
		}
		a.st.saveLocked()
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

// POST /api/orders/{id}/remove {"how":"close"|"delete"} – eltávolítás a listából a vendég
// értesítése nélkül (pl. ha egy már lezárt rendelést véletlenül visszaállítottak újnak).
// "close": lezárt (kész) lesz, a statisztikában megmarad; "delete": véglegesen törlődik.
func (a *App) handleRemove(w http.ResponseWriter, r *http.Request) {
	var in struct {
		How string `json:"how"`
	}
	readJSON(r, &in, 1<<10)
	id, ok := a.orderID(r)
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	o := a.find(id)
	if !ok || o == nil {
		writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
		return
	}
	switch in.How {
	case "delete":
		for i, x := range a.st.Orders {
			if x == o {
				a.st.Orders = append(a.st.Orders[:i], a.st.Orders[i+1:]...)
				break
			}
		}
	default:
		o.Status, o.StatusAt = StDone, time.Now()
	}
	a.st.saveLocked()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleResend(w http.ResponseWriter, r *http.Request) {
	id, ok := a.orderID(r)
	if !ok {
		writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
		return
	}
	go a.emailAsync(id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleMarkMailed(w http.ResponseWriter, r *http.Request) {
	id, ok := a.orderID(r)
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	o := a.find(id)
	if !ok || o == nil {
		writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
		return
	}
	o.EmailState, o.EmailErr, o.EmailAt = "manual", "", time.Now()
	a.st.saveLocked()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) handleManual(w http.ResponseWriter, r *http.Request) {
	var in Order
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<18)).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás adat"})
		return
	}
	o := &Order{
		Source: "manual", Name: strings.TrimSpace(in.Name), Email: strings.TrimSpace(in.Email),
		Phone: strings.TrimSpace(in.Phone), City: strings.TrimSpace(in.City), Street: strings.TrimSpace(in.Street),
		Items: strings.TrimSpace(in.Items), Note: strings.TrimSpace(in.Note), Mode: normMode(in.Mode),
	}
	if in.Mode == modePickup {
		o.Mode = modePickup
	}
	if o.Name == "" || o.Items == "" {
		writeJSON(w, 400, map[string]string{"error": "a név és a rendelés kötelező"})
		return
	}
	a.ingest(o)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

var idKeys = []string{"id", "_id", "submissionId", "submission_id", "submission.id"}

func (a *App) handleWebhook(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.PathValue("key")), []byte(a.conf().WebhookKulcs)) != 1 {
		http.Error(w, "forbidden", 403)
		return
	}
	raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	kv := map[string]string{}
	var parsed any
	if err := json.Unmarshal(raw, &parsed); err == nil {
		if lk, id := wixSubmissions(parsed); lk != nil {
			for k, v := range lk {
				kv[k] = v
			}
			if id != "" {
				kv["submissionId"] = id
			}
		} else {
			flatten("", parsed, kv)
		}
	} else {
		r.Body = io.NopCloser(strings.NewReader(string(raw)))
		if r.ParseForm() == nil {
			for k, v := range r.Form {
				kv[k] = strings.Join(v, ", ")
			}
		}
	}
	if len(kv) == 0 {
		http.Error(w, "üres kérés", 400)
		return
	}
	// a "data." / "payload." előtagot levesszük a kulcsokról
	clean := map[string]string{}
	for k, v := range kv {
		for _, p := range []string{"data.", "payload.", "formData.", "fields."} {
			k = strings.TrimPrefix(k, p)
		}
		clean[k] = v
	}
	extID := ""
	for _, k := range idKeys {
		if v, ok := clean[k]; ok && v != "" {
			if extID == "" {
				extID = "hook:" + v
			}
			delete(clean, k)
		}
	}
	o := mapFields(clean, nil)
	o.Source = "webhook"
	o.ExtID = extID
	if o.Items == "" && o.Name == "" {
		log.Printf("Webhook: nem ismertem fel a mezőket: %v", clean)
	}
	a.ingest(o)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (a *App) serveIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	name := "web/index.html"
	switch {
	case a.needsSetup():
		name = "web/setup.html"
	case !a.authed(r):
		name = "web/login.html"
	}
	data, _ := webFS.ReadFile(name)
	sendStatic(w, r, name, data, "text/html; charset=utf-8", "no-store")
}

func (a *App) handleLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	remote := viaFunnel(r)
	if remote && loginLocked() {
		time.Sleep(1500 * time.Millisecond)
		http.Redirect(w, r, "/?hiba=2", http.StatusSeeOther)
		return
	}
	if !a.needsSetup() && subtle.ConstantTimeCompare([]byte(r.FormValue("jelszo")), []byte(a.conf().Jelszo)) == 1 {
		a.setSession(w, a.token())
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if remote {
		loginFailed()
	}
	time.Sleep(1500 * time.Millisecond)
	http.Redirect(w, r, "/?hiba=1", http.StatusSeeOther)
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", a.serveIndex)
	mux.HandleFunc("POST /login", a.handleLogin)
	mux.HandleFunc("GET /logout", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "sid", Value: "", Path: "/", MaxAge: -1})
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /api/orders", a.requireAuth(a.handleList))
	mux.HandleFunc("POST /api/orders", a.requireAuth(a.handleManual))
	mux.HandleFunc("POST /api/orders/{id}/accept", a.requireAuth(a.handleAccept))
	mux.HandleFunc("POST /api/orders/{id}/reject", a.requireAuth(a.handleReject))
	mux.HandleFunc("POST /api/orders/{id}/total", a.requireAuth(a.handleSetTotal))
	mux.HandleFunc("POST /api/orders/{id}/done", a.requireAuth(a.handleSimple(StDone)))
	mux.HandleFunc("POST /api/orders/{id}/reopen", a.requireAuth(a.handleSimple(StNew)))
	mux.HandleFunc("POST /api/orders/{id}/resend", a.requireAuth(a.handleResend))
	mux.HandleFunc("POST /api/orders/{id}/remove", a.requireAuth(a.handleRemove))
	mux.HandleFunc("POST /api/orders/{id}/mailed", a.requireAuth(a.handleMarkMailed))
	mux.HandleFunc("POST /webhook/{key}", a.handleWebhook)
	mux.HandleFunc("GET /api/push/key", a.requireAuth(a.handlePushKey))
	mux.HandleFunc("POST /api/push/subscribe", a.requireAuth(a.handlePushSub))
	mux.HandleFunc("POST /api/push/unsubscribe", a.requireAuth(a.handlePushUnsub))
	mux.HandleFunc("POST /api/push/test", a.requireAuth(a.handlePushTest))
	mux.HandleFunc("POST /api/setup", a.handleSetup)
	mux.HandleFunc("GET /api/settings", a.requireAuth(a.handleSettings))
	mux.HandleFunc("POST /api/settings/email", a.requireAuth(a.handleSettingsEmail))
	mux.HandleFunc("POST /api/settings/test-email", a.requireAuth(a.handleTestEmail))
	mux.HandleFunc("POST /api/settings/password", a.requireAuth(a.handleSettingsPassword))
	mux.HandleFunc("POST /api/settings/online", a.requireAuth(a.handleSettingsOnline))
	mux.HandleFunc("POST /api/settings/closed", a.requireAuth(a.handleSettingsClosed))
	mux.HandleFunc("POST /api/settings/ntfy", a.requireAuth(a.handleSettingsNtfy))
	mux.HandleFunc("POST /api/settings/test-ntfy", a.requireAuth(a.handleTestNtfy))
	mux.HandleFunc("GET /api/raffle", a.requireAuth(a.handleRaffleList))
	mux.HandleFunc("POST /api/raffle", a.requireAuth(a.handleRaffleDraw))
	mux.HandleFunc("POST /api/raffle/preview", a.requireAuth(a.handleRafflePreview))
	mux.HandleFunc("GET /api/giveaway", a.requireAuth(a.handleGiveaway))
	mux.HandleFunc("POST /api/giveaway/notify", a.requireAuth(a.handleGiveawayNotify))
	mux.HandleFunc("GET /api/backup", a.requireAuth(a.handleBackup))
	mux.HandleFunc("POST /api/import", a.requireAuth(a.handleImport))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /rendeles", a.servePublicPage)
	mux.HandleFunc("GET /api/public/menu", a.handlePublicMenu)
	mux.HandleFunc("GET /api/public/giveaway", a.handlePublicGiveaway)
	mux.HandleFunc("GET /api/public/promo", a.handlePublicPromo)
	mux.HandleFunc("POST /api/public/order", a.handlePublicOrder)
	mux.HandleFunc("POST /api/public/quick", a.handlePublicQuick)
	mux.HandleFunc("GET /api/public/order/{token}", a.handlePublicStatus)
	mux.HandleFunc("POST /api/public/message", a.handlePublicMessage)
	mux.HandleFunc("GET /api/public/preorder", a.handlePublicPreorder)
	mux.HandleFunc("POST /api/public/preorder", a.handlePublicPreorderPost)
	mux.HandleFunc("GET /api/preorders", a.requireAuth(a.handlePreorders))
	mux.HandleFunc("POST /api/preorders/soldout", a.requireAuth(a.handlePreorderSoldOut))
	mux.HandleFunc("POST /api/preorders/{id}/status", a.requireAuth(a.handlePreorderStatus))
	mux.HandleFunc("GET /api/messages", a.requireAuth(a.handleMessages))
	mux.HandleFunc("POST /api/messages/{id}/done", a.requireAuth(a.handleMessageDone))
	// a vendégek telefonjára tehető rendelő alkalmazás (a weboldal ezeket is továbbítja)
	for _, f := range []string{"rendeles-app.webmanifest", "rendeles-sw.js"} {
		f := f
		mux.HandleFunc("GET /"+f, func(w http.ResponseWriter, r *http.Request) {
			data, _ := webFS.ReadFile("web/" + f)
			if strings.HasSuffix(f, ".js") {
				w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			} else {
				w.Header().Set("Content-Type", "application/manifest+json")
			}
			w.Header().Set("Cache-Control", "no-cache")
			w.Write(data)
		})
	}
	for _, f := range []string{"sw.js", "manifest.json", "icon-192.png", "icon-512.png", "apple-touch-icon.png", "logo.png"} {
		f := f
		mux.HandleFunc("GET /"+f, func(w http.ResponseWriter, r *http.Request) {
			data, err := webFS.ReadFile("web/" + f)
			if err != nil {
				http.NotFound(w, r)
				return
			}
			ct := map[string]string{".js": "text/javascript; charset=utf-8", ".json": "application/manifest+json", ".png": "image/png"}[filepath.Ext(f)]
			w.Header().Set("Content-Type", ct)
			if f == "sw.js" {
				w.Header().Set("Cache-Control", "no-cache")
				w.Header().Set("Service-Worker-Allowed", "/")
			}
			w.Write(data)
		})
	}
	return secHeaders(mux)
}
