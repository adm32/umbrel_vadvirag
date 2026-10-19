package main

// Látogatottsági statisztika – sütik nélkül.
//
// A látogatót egy naponta cserélődő, véletlen "só" + IP + böngésző azonosító
// hash-ével ismerjük fel; az IP-címet nem tároljuk, és másnap már a hash sem
// köthető össze az előző napival. A böngésző a /api/t végpontra küld rövid
// jelzéseket (oldal megnyitása, életjel, megnézett szakasz, kattintások, hibák).
//
// Az összesítést csak a belső porton (STATS_ADDR, Umbrelen :9090) adjuk ki az
// Irányítópultnak; ez a port nincs kivezetve a gépre vagy az internetre.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

var budapest = func() *time.Location {
	l, err := time.LoadLocation("Europe/Budapest")
	if err != nil {
		return time.Local
	}
	return l
}()

func dayKey(t time.Time) string { return t.In(budapest).Format("2006-01-02") }

// Day – egy nap összesítése.
type Day struct {
	Date      string            `json:"date"`
	Views     int               `json:"views"`
	Visitors  int               `json:"visitors"`
	HourViews [24]int           `json:"hour_views"`
	HourVis   [24]int           `json:"hour_visitors"`
	Pages     map[string]int    `json:"pages"`
	Langs     map[string]int    `json:"langs"`
	Devices   map[string]int    `json:"devices"`
	Refs      map[string]int    `json:"refs"`
	Countries map[string]int    `json:"countries"`
	Sections  map[string]int    `json:"sections"`
	Events    map[string]int    `json:"events"`
	Adds      map[string]int    `json:"adds"`     // kosárba tett ételek (azonosító)
	Towns     map[string]int    `json:"towns"`    // kiszállítás keresőben talált települések
	TownsNo   map[string]int    `json:"towns_no"` // nem talált (nem szállítunk oda) – beírt szöveg
	Errors    int               `json:"js_errors"`
	Srv5xx    int               `json:"srv_5xx"`
	Upstream  int               `json:"upstream_errors"`
	Extra     map[string]string `json:"extra,omitempty"`
	seen      map[string]bool   // ma látott látogatók (nem mentjük)
	hourSeen  [24]map[string]bool
}

type ErrEntry struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"` // js | server | upstream
	Msg  string    `json:"msg"`
	Src  string    `json:"src,omitempty"`
	Page string    `json:"page,omitempty"`
	UA   string    `json:"ua,omitempty"`
}

type statsFile struct {
	Salt     string               `json:"salt"`
	SaltDay  string               `json:"salt_day"`
	Days     map[string]*Day      `json:"days"`
	Seen     map[string]bool      `json:"seen_today"` // a mai látogatók hash-e, hogy újraindítás után se számoljuk duplán
	HourSeen [24]map[string]bool  `json:"hour_seen_today"`
	Errors   []ErrEntry           `json:"errors"`
	Extra    map[string]time.Time `json:"extra,omitempty"`
}

var st = struct {
	sync.Mutex
	f        statsFile
	live     map[string]liveEntry
	path     string
	dirty    bool
	started  time.Time
	errCount int // összes hiba indulás óta (az irányítópult ebből látja, ha új jött)
}{live: map[string]liveEntry{}, started: time.Now()}

type liveEntry struct {
	At      time.Time
	Page    string
	Country string
	Device  string
}

func newDay(d string) *Day {
	x := &Day{Date: d}
	x.init()
	return x
}

func (d *Day) init() {
	m := func(p *map[string]int) {
		if *p == nil {
			*p = map[string]int{}
		}
	}
	m(&d.Pages)
	m(&d.Langs)
	m(&d.Devices)
	m(&d.Refs)
	m(&d.Countries)
	m(&d.Sections)
	m(&d.Events)
	m(&d.Adds)
	m(&d.Towns)
	m(&d.TownsNo)
	if d.seen == nil {
		d.seen = map[string]bool{}
	}
	for i := range d.hourSeen {
		if d.hourSeen[i] == nil {
			d.hourSeen[i] = map[string]bool{}
		}
	}
}

func randHex(n int) string {
	b := make([]byte, n)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// ---------------------------------------------------------------- betöltés / mentés

func statsInit() {
	dir := env("DATA_DIR", "")
	if dir != "" {
		os.MkdirAll(dir, 0o755)
		st.path = filepath.Join(dir, "statisztika.json")
		if b, err := os.ReadFile(st.path); err == nil {
			json.Unmarshal(b, &st.f)
		}
	}
	if st.f.Days == nil {
		st.f.Days = map[string]*Day{}
	}
	for _, d := range st.f.Days {
		d.init()
	}
	today := dayKey(time.Now())
	if st.f.SaltDay != today || st.f.Salt == "" {
		st.f.Salt, st.f.SaltDay, st.f.Seen = randHex(16), today, nil
		st.f.HourSeen = [24]map[string]bool{}
	}
	if d := st.f.Days[today]; d != nil {
		if st.f.Seen != nil {
			d.seen = st.f.Seen
		}
		for i := range st.f.HourSeen {
			if st.f.HourSeen[i] != nil {
				d.hourSeen[i] = st.f.HourSeen[i]
			}
		}
	}
	go func() {
		for range time.Tick(time.Minute) {
			statsSave(false)
		}
	}()
}

func statsSave(force bool) {
	st.Lock()
	defer st.Unlock()
	if st.path == "" || (!st.dirty && !force) {
		return
	}
	// 400 napnál régebbi napok törlése
	cut := dayKey(time.Now().AddDate(0, 0, -400))
	for k := range st.f.Days {
		if k < cut {
			delete(st.f.Days, k)
		}
	}
	if d := st.f.Days[dayKey(time.Now())]; d != nil {
		st.f.Seen, st.f.HourSeen = d.seen, d.hourSeen
	}
	b, err := json.Marshal(st.f)
	if err != nil {
		return
	}
	tmp := st.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		os.Rename(tmp, st.path)
		st.dirty = false
	}
}

// today: a mai nap (lock alatt hívandó); éjfélkor új só, új nap.
func today() *Day {
	k := dayKey(time.Now())
	if st.f.SaltDay != k {
		st.f.Salt, st.f.SaltDay = randHex(16), k
	}
	d := st.f.Days[k]
	if d == nil {
		d = newDay(k)
		st.f.Days[k] = d
	}
	return d
}

// ---------------------------------------------------------------- látogató azonosítása

func clientIP(r *http.Request) string {
	if v := r.Header.Get("CF-Connecting-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		return strings.TrimSpace(strings.Split(v, ",")[0])
	}
	h, _, _ := net.SplitHostPort(r.RemoteAddr)
	return h
}

var botWords = []string{"bot", "crawl", "spider", "slurp", "preview", "headless", "lighthouse", "curl", "wget", "python", "go-http", "facebookexternalhit", "whatsapp", "monitor"}

func isBot(ua string) bool {
	l := strings.ToLower(ua)
	if l == "" {
		return true
	}
	for _, w := range botWords {
		if strings.Contains(l, w) {
			return true
		}
	}
	return false
}

func visitorID(r *http.Request) string {
	h := sha256.Sum256([]byte(st.f.Salt + "|" + clientIP(r) + "|" + r.UserAgent()))
	return hex.EncodeToString(h[:8])
}

func deviceOf(ua string, w int) string {
	l := strings.ToLower(ua)
	switch {
	case strings.Contains(l, "ipad") || strings.Contains(l, "tablet") || (strings.Contains(l, "android") && !strings.Contains(l, "mobile")):
		return "tablet"
	case strings.Contains(l, "mobi") || strings.Contains(l, "iphone") || (w > 0 && w < 700):
		return "telefon"
	}
	return "számítógép"
}

func countryOf(r *http.Request) string {
	c := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	if c == "" || c == "XX" || c == "T1" {
		return "helyi"
	}
	return c
}

func refHost(ref, host string) string {
	if ref == "" {
		return "közvetlen"
	}
	u, err := url.Parse(ref)
	if err != nil || u.Host == "" {
		return "közvetlen"
	}
	h := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if h == strings.TrimPrefix(strings.ToLower(host), "www.") || strings.HasSuffix(h, "vadviragetterem.com") {
		return ""
	}
	switch {
	case strings.Contains(h, "facebook") || h == "fb.me" || h == "l.facebook.com" || h == "m.facebook.com":
		return "Facebook"
	case strings.Contains(h, "instagram"):
		return "Instagram"
	case strings.Contains(h, "google"):
		return "Google"
	case strings.Contains(h, "bing"):
		return "Bing"
	}
	return h
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func inc(m map[string]int, k string) {
	if k == "" || len(m) > 500 && m[k] == 0 { // védelem a végtelen sok kulcs ellen
		return
	}
	m[k]++
}

// ---------------------------------------------------------------- böngészőből jövő jelzések

type beacon struct {
	T   string `json:"t"`   // view | ping | sec | ev | err
	P   string `json:"p"`   // oldal (/, /en, /de, /rendeles)
	L   string `json:"l"`   // nyelv
	R   string `json:"r"`   // referrer
	W   int    `json:"w"`   // képernyő szélesség
	N   string `json:"n"`   // esemény / szakasz neve
	V   string `json:"v"`   // érték (pl. étel azonosító, település)
	Ok  *bool  `json:"ok"`  // település: szállítunk-e oda
	Msg string `json:"msg"` // hibaüzenet
	Src string `json:"src"` // hiba helye
}

var allowedEvents = map[string]bool{"add": true, "wheel": true, "wheel_order": true, "town": true, "form": true, "lang": true, "call": true,
	"route": true, "fb": true, "order_sent": true, "order_page": true, "order_btn": true, "drinks": true, "search": true, "gallery": true, "alg": true, "quick": true}
var allowedSections = map[string]bool{"rolunk": true, "etlap": true, "rendezvenyek": true, "kiszallitas": true, "galeria": true, "kapcsolat": true}

func handleBeacon(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(204)
	if isBot(r.UserAgent()) {
		return
	}
	var b beacon
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&b); err != nil {
		return
	}
	now := time.Now()
	st.Lock()
	defer st.Unlock()
	d := today()
	id := visitorID(r)
	page := clip(b.P, 40)
	if page == "" {
		page = "/"
	}
	st.dirty = true
	switch b.T {
	case "view":
		h := now.In(budapest).Hour()
		d.Views++
		d.HourViews[h]++
		if !d.seen[id] {
			d.seen[id] = true
			d.Visitors++
			inc(d.Devices, deviceOf(r.UserAgent(), b.W))
			inc(d.Countries, countryOf(r))
			if ref := refHost(b.R, r.Host); ref != "" {
				inc(d.Refs, clip(ref, 60))
			}
		}
		if !d.hourSeen[h][id] {
			d.hourSeen[h][id] = true
			d.HourVis[h]++
		}
		inc(d.Pages, page)
		if b.L == "hu" || b.L == "en" || b.L == "de" {
			inc(d.Langs, b.L)
		}
		st.live[id] = liveEntry{At: now, Page: page, Country: countryOf(r), Device: deviceOf(r.UserAgent(), b.W)}
	case "ping":
		if e, ok := st.live[id]; ok {
			e.At, e.Page = now, page
			st.live[id] = e
		} else {
			st.live[id] = liveEntry{At: now, Page: page, Country: countryOf(r), Device: deviceOf(r.UserAgent(), b.W)}
		}
	case "sec":
		if allowedSections[b.N] {
			inc(d.Sections, b.N)
		}
	case "ev":
		if !allowedEvents[b.N] {
			return
		}
		inc(d.Events, b.N)
		switch b.N {
		case "add", "wheel_order":
			inc(d.Adds, clip(b.V, 60))
		case "town":
			if b.Ok != nil && *b.Ok {
				inc(d.Towns, clip(b.V, 40))
			} else {
				inc(d.TownsNo, strings.ToLower(clip(b.V, 40)))
			}
		}
	case "err":
		d.Errors++
		addErr(ErrEntry{At: now, Kind: "js", Msg: clip(b.Msg, 300), Src: clip(b.Src, 200), Page: page, UA: clip(r.UserAgent(), 160)})
	}
}

// addErr: lock alatt hívandó
func addErr(e ErrEntry) {
	st.errCount++
	st.f.Errors = append(st.f.Errors, e)
	if len(st.f.Errors) > 200 {
		st.f.Errors = st.f.Errors[len(st.f.Errors)-200:]
	}
	st.dirty = true
}

func noteServerError(kind, msg, page string) {
	st.Lock()
	defer st.Unlock()
	d := today()
	if kind == "upstream" {
		d.Upstream++
	} else {
		d.Srv5xx++
	}
	addErr(ErrEntry{At: time.Now(), Kind: kind, Msg: clip(msg, 300), Page: clip(page, 80)})
}

// statusRecorder: a szerver 5xx válaszait is számoljuk
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (s *statusRecorder) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func errorMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		defer func() {
			if v := recover(); v != nil {
				log.Printf("hiba: %v (%s)", v, r.URL.Path)
				noteServerError("server", "Programhiba: "+clip(strings.TrimSpace(strings.ReplaceAll(toString(v), "\n", " ")), 200), r.URL.Path)
				if rec.code == 200 {
					http.Error(w, "Hiba történt", 500)
				}
				return
			}
			// a rendeléskezelő kiesését külön jelezzük (ErrorHandler), itt csak a weboldal saját hibái
			if rec.code >= 500 && !isProxyPath(r.URL.Path) {
				noteServerError("server", "HTTP "+strconv.Itoa(rec.code), r.URL.Path)
			}
		}()
		next.ServeHTTP(rec, r)
	})
}

func toString(v any) string {
	switch x := v.(type) {
	case error:
		return x.Error()
	case string:
		return x
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func isProxyPath(p string) bool {
	return strings.HasPrefix(p, "/rendeles") || strings.HasPrefix(p, "/api/public/") || p == "/logo.png" || p == "/icon-192.png"
}

// ---------------------------------------------------------------- belső port az irányítópultnak

func statsServer() {
	addr := env("STATS_ADDR", "")
	if addr == "" {
		return
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /stats", handleStats)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		upMu.RLock()
		up := upstream.String()
		upMu.RUnlock()
		ok := fetchHealth(orderURL().String())
		st.Lock()
		ec := st.errCount
		st.Unlock()
		menuMu.Lock()
		live := menuCache != nil && len(menuCache) > 12 && string(menuCache[:12]) == `{"live":true`
		menuMu.Unlock()
		writeJSONs(w, map[string]any{"version": version, "started": st.started, "upstream": up, "upstream_ok": ok, "menu_live": live,
			"errors_since_start": ec, "site_url": siteURL})
	})
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("statisztika belső port: %s", addr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("statisztika port nem indult: %v", err)
		}
	}()
}

func fetchHealth(base string) bool {
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(base + "/healthz")
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode == 200
}

func writeJSONs(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(v)
}

// GET /stats?days=30
func handleStats(w http.ResponseWriter, r *http.Request) {
	n, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if n <= 0 || n > 400 {
		n = 30
	}
	now := time.Now()
	st.Lock()
	defer st.Unlock()
	today()
	// élő látogatók: az utolsó 90 másodpercben jelentkeztek
	type liveOut struct {
		Page    string `json:"page"`
		Country string `json:"country"`
		Device  string `json:"device"`
		Ago     int    `json:"ago"`
	}
	live := []liveOut{}
	for id, e := range st.live {
		if now.Sub(e.At) > 90*time.Second {
			if now.Sub(e.At) > 10*time.Minute {
				delete(st.live, id)
			}
			continue
		}
		live = append(live, liveOut{Page: e.Page, Country: e.Country, Device: e.Device, Ago: int(now.Sub(e.At).Seconds())})
	}
	sort.Slice(live, func(i, j int) bool { return live[i].Ago < live[j].Ago })
	days := make([]*Day, 0, n)
	for i := n - 1; i >= 0; i-- {
		k := dayKey(now.AddDate(0, 0, -i))
		if d := st.f.Days[k]; d != nil {
			days = append(days, d)
		} else {
			days = append(days, newDay(k))
		}
	}
	errs := append([]ErrEntry{}, st.f.Errors...)
	if len(errs) > 50 {
		errs = errs[len(errs)-50:]
	}
	writeJSONs(w, map[string]any{"now": now, "live": live, "days": days, "errors": errs, "errors_since_start": st.errCount, "version": version})
}
