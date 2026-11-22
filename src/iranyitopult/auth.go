package main

// Jelszavas zár az Irányítópulthoz (ugyanúgy, mint a Rendelések appnál).
//
// Első megnyitáskor jelszót kell beállítani, utána belépés nélkül semmi nem látszik.
// A jelszót nem tároljuk, csak sózott, sokszor ismételt HMAC-SHA256 lenyomatát (jelszo.json).
// A belépés egy aláírt sütiben marad meg; jelszócserénél minden eszköz kijelentkezik.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

//go:embed authweb
var authWeb embed.FS

const (
	authFileName = "jelszo.json"
	cookieName   = "vv_dash"
	pwRounds     = 60000
)

type authData struct {
	Salt   string `json:"salt"`
	Hash   string `json:"hash"`
	Secret string `json:"secret"`
}

var (
	authMu    sync.Mutex
	auth      authData
	failTimes []time.Time
)

func loadAuth() {
	authMu.Lock()
	defer authMu.Unlock()
	loadJSON(authFileName, &auth)
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func pwHash(salt, pw string) string {
	k := []byte(salt)
	h := hmac.New(sha256.New, k)
	h.Write([]byte(pw))
	sum := h.Sum(nil)
	for i := 0; i < pwRounds; i++ {
		h = hmac.New(sha256.New, k)
		h.Write(sum)
		sum = h.Sum(nil)
	}
	return hex.EncodeToString(sum)
}

func hasPassword() bool {
	authMu.Lock()
	defer authMu.Unlock()
	return auth.Hash != ""
}

func sessionValue() string {
	authMu.Lock()
	defer authMu.Unlock()
	if auth.Secret == "" {
		return ""
	}
	m := hmac.New(sha256.New, []byte(auth.Secret))
	m.Write([]byte("vadvirag-iranyitopult-session"))
	return hex.EncodeToString(m.Sum(nil))
}

func authed(r *http.Request) bool {
	want := sessionValue()
	if want == "" {
		return false
	}
	c, err := r.Cookie(cookieName)
	return err == nil && subtle.ConstantTimeCompare([]byte(c.Value), []byte(want)) == 1
}

func setSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: sessionValue(), Path: "/", MaxAge: 180 * 24 * 3600,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
}

// setPassword: új jelszó és új titok (így a régi belépések érvénytelenek lesznek).
func setPassword(pw string) error {
	authMu.Lock()
	defer authMu.Unlock()
	salt := randHex(16)
	n := authData{Salt: salt, Hash: pwHash(salt, pw), Secret: randHex(32)}
	if err := saveJSON(authFileName, n); err != nil {
		return err
	}
	auth = n
	return nil
}

func checkPassword(pw string) bool {
	authMu.Lock()
	a := auth
	authMu.Unlock()
	if a.Hash == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(pwHash(a.Salt, pw)), []byte(a.Hash)) == 1
}

// túl sok hibás próbálkozás: 15 percen belül 8 hiba után 15 percig nem enged belépni
func locked() bool {
	authMu.Lock()
	defer authMu.Unlock()
	keep := failTimes[:0]
	for _, t := range failTimes {
		if time.Since(t) < 15*time.Minute {
			keep = append(keep, t)
		}
	}
	failTimes = keep
	return len(failTimes) >= 8
}

func failed() {
	authMu.Lock()
	failTimes = append(failTimes, time.Now())
	authMu.Unlock()
}

func servePage(w http.ResponseWriter, name string) {
	b, _ := authWeb.ReadFile("authweb/" + name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Write(b)
}

func validNewPassword(pw, pw2 string) string {
	if len([]rune(strings.TrimSpace(pw))) < 6 {
		return "rovid"
	}
	if pw != pw2 {
		return "nemegyezik"
	}
	return ""
}

// requireLogin: minden kérést megfog, amíg nincs belépés. Szabad: a belépő oldal, az ikon és a /healthz.
func requireLogin(next http.Handler) http.Handler {
	public := map[string]bool{"/healthz": true, "/icon.png": true, "/logo.svg": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case public[p]:
			next.ServeHTTP(w, r)
		case p == "/setup" && r.Method == http.MethodPost:
			handleSetup(w, r)
		case p == "/login" && r.Method == http.MethodPost:
			handleLogin(w, r)
		case p == "/logout":
			http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1})
			http.Redirect(w, r, "/", http.StatusSeeOther)
		case p == "/api/password" && r.Method == http.MethodPost:
			if !authed(r) {
				writeJSON(w, 401, map[string]string{"error": "Lejárt a belépés, töltsd újra az oldalt."})
				return
			}
			handlePasswordChange(w, r)
		case authed(r):
			next.ServeHTTP(w, r)
		case strings.HasPrefix(p, "/api/"):
			writeJSON(w, 401, map[string]string{"error": "Lejárt a belépés, töltsd újra az oldalt."})
		case !hasPassword():
			servePage(w, "setup.html")
		default:
			servePage(w, "login.html")
		}
	})
}

func handleSetup(w http.ResponseWriter, r *http.Request) {
	if hasPassword() {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	r.ParseForm()
	if bad := validNewPassword(r.FormValue("jelszo"), r.FormValue("jelszo2")); bad != "" {
		http.Redirect(w, r, "/?hiba="+bad, http.StatusSeeOther)
		return
	}
	if err := setPassword(r.FormValue("jelszo")); err != nil {
		log.Printf("jelszó mentése nem sikerült: %v", err)
		http.Redirect(w, r, "/?hiba=mentes", http.StatusSeeOther)
		return
	}
	log.Printf("Jelszó beállítva")
	setSessionCookie(w, r)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	r.ParseForm()
	if locked() {
		time.Sleep(1500 * time.Millisecond)
		http.Redirect(w, r, "/?hiba=2", http.StatusSeeOther)
		return
	}
	if checkPassword(r.FormValue("jelszo")) {
		setSessionCookie(w, r)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	failed()
	time.Sleep(1500 * time.Millisecond)
	http.Redirect(w, r, "/?hiba=1", http.StatusSeeOther)
}

// POST /api/password {current, new, new2} – jelszócsere; minden más eszköz kijelentkezik.
func handlePasswordChange(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
		New2    string `json:"new2"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	if locked() || !checkPassword(in.Current) {
		failed()
		time.Sleep(1200 * time.Millisecond)
		writeJSON(w, 403, map[string]string{"error": "A jelenlegi jelszó nem jó."})
		return
	}
	switch validNewPassword(in.New, in.New2) {
	case "rovid":
		writeJSON(w, 400, map[string]string{"error": "Az új jelszó legalább 6 karakter legyen."})
		return
	case "nemegyezik":
		writeJSON(w, 400, map[string]string{"error": "A két új jelszó nem egyezik."})
		return
	}
	if err := setPassword(in.New); err != nil {
		writeJSON(w, 500, map[string]string{"error": "Nem sikerült menteni: " + err.Error()})
		return
	}
	setSessionCookie(w, r)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
