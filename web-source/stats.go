package main

// Látogatottsági statisztika – sütik és a böngészőben tárolt azonosító nélkül.
//
// A látogatót egy naponta cserélődő, véletlen "só" + IP + böngésző azonosító
// hash-ével ismerjük fel; az IP-címet nem tároljuk, és másnap már a hash sem
// köthető össze az előző napival. A böngésző a /api/t végpontra küld rövid
// jelzéseket (oldal megnyitása, életjel, megnézett szakasz, kattintások, hibák).
//
// Az egyes látogatások részleteit (eszköz, böngésző, időtartam, mit csinált)
// 7 napig őrizzük, utána csak a napi összesítések maradnak meg.
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
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
	"unicode/utf8"
)

var budapest = func() *time.Location {
	l, err := time.LoadLocation("Europe/Budapest")
	if err != nil {
		return time.Local
	}
	return l
}()

func dayKey(t time.Time) string { return t.In(budapest).Format("2006-01-02") }

const sessionDays = 7 // ennyi napig őrizzük a látogatások részleteit

// Day – egy nap összesítése (ezt őrizzük hosszú távon).
type Day struct {
	Date      string         `json:"date"`
	Views     int            `json:"views"`
	Visitors  int            `json:"visitors"`
	HourViews [24]int        `json:"hour_views"`
	HourVis   [24]int        `json:"hour_visitors"`
	Pages     map[string]int `json:"pages"`
	Langs     map[string]int `json:"langs"`
	Devices   map[string]int `json:"devices"`
	Refs      map[string]int `json:"refs"`
	Countries map[string]int `json:"countries"`
	Sections  map[string]int `json:"sections"`
	Events    map[string]int `json:"events"`
	Adds      map[string]int `json:"adds"`     // kosárba tett ételek (azonosító)
	Towns     map[string]int `json:"towns"`    // kiszállítás keresőben talált települések
	TownsNo   map[string]int `json:"towns_no"` // nem talált (nem szállítunk oda) – beírt szöveg
	// látogatónként egyszer számolva
	OS        map[string]int `json:"os"`
	Browsers  map[string]int `json:"browsers"`
	Models    map[string]int `json:"models"`
	Screens   map[string]int `json:"screens"`
	Cities    map[string]int `json:"cities"`
	Campaigns map[string]int `json:"campaigns"`
	BLangs    map[string]int `json:"browser_langs"`
	Conns     map[string]int `json:"conns"`
	Dark      int            `json:"dark"`
	// a látogatások részleteiből számolva (az utolsó mentéskor)
	DurBuckets map[string]int `json:"dur_buckets"`
	ScrollB    map[string]int `json:"scroll_buckets"`
	DurSum     int            `json:"dur_sum"` // másodperc, összesen
	Bounces    int            `json:"bounces"` // 10 mp-en belül, kattintás nélkül távozott
	Engaged    int            `json:"engaged"` // legalább 30 mp vagy kattintott valamire
	Ordered    int            `json:"ordered"` // rendelést küldött
	// látogatótól a rendelésig – egyedi látogatók
	FunMenu  int `json:"fun_menu"`
	FunAdd   int `json:"fun_add"`
	FunOrder int `json:"fun_order_page"`
	Errors   int `json:"js_errors"`
	Srv5xx   int `json:"srv_5xx"`
	Upstream int `json:"upstream_errors"`
	seen     map[string]bool
	hourSeen [24]map[string]bool
}

// Session – egy látogató egy napon (névtelen, naponta új azonosító).
type Session struct {
	ID        string    `json:"id"` // rövid, csak megjelenítéshez
	First     time.Time `json:"first"`
	Last      time.Time `json:"last"`
	Views     int       `json:"views"`
	Pages     []string  `json:"pages"`
	Entry     string    `json:"entry"`
	Ref       string    `json:"ref"`
	Campaign  string    `json:"campaign,omitempty"`
	Device    string    `json:"device"`
	OS        string    `json:"os"`
	Browser   string    `json:"browser"`
	Model     string    `json:"model,omitempty"`
	Screen    string    `json:"screen,omitempty"`
	Dark      bool      `json:"dark,omitempty"`
	Conn      string    `json:"conn,omitempty"`
	BLang     string    `json:"blang,omitempty"`
	Lang      string    `json:"lang"`
	Country   string    `json:"country"`
	City      string    `json:"city,omitempty"`
	Region    string    `json:"region,omitempty"`
	Sections  []string  `json:"sections"`
	Section   string    `json:"section,omitempty"` // amit éppen néz
	MaxScroll int       `json:"scroll"`
	Events    []SessEv  `json:"events"`
	Added     int       `json:"added"`
	Ordered   bool      `json:"ordered,omitempty"`
}

type SessEv struct {
	At time.Time `json:"at"`
	N  string    `json:"n"`
	V  string    `json:"v,omitempty"`
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
	Salt     string                         `json:"salt"`
	SaltDay  string                         `json:"salt_day"`
	Days     map[string]*Day                `json:"days"`
	Seen     map[string]bool                `json:"seen_today"`
	HourSeen [24]map[string]bool            `json:"hour_seen_today"`
	Sessions map[string]map[string]*Session `json:"sessions"` // nap -> látogató hash -> részletek
	Errors   []ErrEntry                     `json:"errors"`
}

var st = struct {
	sync.Mutex
	f        statsFile
	path     string
	dirty    bool
	started  time.Time
	errCount int
}{started: time.Now()}

func newDay(d string) *Day {
	x := &Day{Date: d}
	x.init()
	return x
}

func (d *Day) init() {
	for _, p := range []*map[string]int{&d.Pages, &d.Langs, &d.Devices, &d.Refs, &d.Countries, &d.Sections, &d.Events, &d.Adds, &d.Towns, &d.TownsNo,
		&d.OS, &d.Browsers, &d.Models, &d.Screens, &d.Cities, &d.Campaigns, &d.BLangs, &d.Conns, &d.DurBuckets, &d.ScrollB} {
		if *p == nil {
			*p = map[string]int{}
		}
	}
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
			if err := json.Unmarshal(b, &st.f); err != nil {
				log.Printf("statisztika: a mentett fájl nem olvasható (%v) – új kezdődik", err)
				os.Rename(st.path, st.path+".hibas")
				st.f = statsFile{}
			}
		}
	}
	if st.f.Days == nil {
		st.f.Days = map[string]*Day{}
	}
	if st.f.Sessions == nil {
		st.f.Sessions = map[string]map[string]*Session{}
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
		// régi mentés: a látogatók halmaza a munkamenetekből is visszaállítható
		for id := range st.f.Sessions[today] {
			d.seen[id] = true
		}
	}
	go func() {
		for range time.Tick(time.Minute) {
			statsSave(false)
		}
	}()
}

// recompute: a látogatások részleteiből számolt napi értékek (lock alatt).
func recompute() {
	for day, ss := range st.f.Sessions {
		d := st.f.Days[day]
		if d == nil {
			continue
		}
		d.DurBuckets, d.ScrollB = map[string]int{}, map[string]int{}
		d.DurSum, d.Bounces, d.Engaged, d.Ordered = 0, 0, 0, 0
		d.FunMenu, d.FunAdd, d.FunOrder = 0, 0, 0
		for _, s := range ss {
			dur := int(s.Last.Sub(s.First).Seconds())
			d.DurSum += dur
			d.DurBuckets[durBucket(dur)]++
			d.ScrollB[scrollBucket(s.MaxScroll)]++
			acts := 0
			for _, e := range s.Events {
				if e.N != "lang" {
					acts++
				}
			}
			if dur < 10 && acts == 0 && s.Views <= 1 {
				d.Bounces++
			}
			if dur >= 30 || acts > 0 {
				d.Engaged++
			}
			if s.Ordered {
				d.Ordered++
			}
			menu, page := false, false
			for _, x := range s.Sections {
				menu = menu || x == "etlap"
			}
			for _, e := range s.Events {
				page = page || e.N == "order_page"
			}
			if menu || s.Added > 0 || page || s.Ordered {
				d.FunMenu++
			}
			if s.Added > 0 || s.Ordered {
				d.FunAdd++
			}
			if page || s.Ordered {
				d.FunOrder++
			}
		}
	}
}

func durBucket(s int) string {
	switch {
	case s < 10:
		return "0-10 mp"
	case s < 30:
		return "10-30 mp"
	case s < 120:
		return "30 mp-2 perc"
	case s < 300:
		return "2-5 perc"
	case s < 900:
		return "5-15 perc"
	}
	return "15+ perc"
}

func scrollBucket(p int) string {
	switch {
	case p < 25:
		return "0-25%"
	case p < 50:
		return "25-50%"
	case p < 75:
		return "50-75%"
	case p < 95:
		return "75-95%"
	}
	return "végig"
}

func statsSave(force bool) {
	st.Lock()
	defer st.Unlock()
	if st.path == "" || (!st.dirty && !force) {
		return
	}
	recompute()
	now := time.Now()
	cut := dayKey(now.AddDate(0, 0, -400))
	for k := range st.f.Days {
		if k < cut {
			delete(st.f.Days, k)
		}
	}
	scut := dayKey(now.AddDate(0, 0, -sessionDays))
	for k := range st.f.Sessions {
		if k < scut {
			delete(st.f.Sessions, k)
		}
	}
	if d := st.f.Days[dayKey(now)]; d != nil {
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

var botWords = []string{"bot", "crawl", "spider", "slurp", "preview", "headless", "lighthouse", "curl", "wget", "python", "go-http", "facebookexternalhit", "whatsapp", "monitor", "vadvirag-iranyitopult"}

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

func countryOf(r *http.Request) string {
	c := strings.ToUpper(strings.TrimSpace(r.Header.Get("CF-IPCountry")))
	if c == "" || c == "XX" || c == "T1" {
		return "helyi"
	}
	return c
}

// város/régió: csak ha a Cloudflare-ben be van kapcsolva a "látogató helye" fejléc
func cityOf(r *http.Request) (city, region string) {
	dec := func(s string) string {
		if u, err := url.QueryUnescape(s); err == nil {
			s = u
		}
		if !utf8.ValidString(s) { // Latin-1 bájtok (pl. „Zalaszentgrót”) → UTF-8
			r := make([]rune, 0, len(s))
			for i := 0; i < len(s); i++ {
				r = append(r, rune(s[i]))
			}
			s = string(r)
		}
		return clip(s, 40)
	}
	return dec(r.Header.Get("CF-IPCity")), dec(r.Header.Get("CF-Region"))
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
	case strings.Contains(h, "facebook") || h == "fb.me" || strings.HasSuffix(h, "fb.com"):
		return "Facebook"
	case strings.Contains(h, "instagram"):
		return "Instagram"
	case strings.Contains(h, "google"):
		return "Google"
	case strings.Contains(h, "bing"):
		return "Bing"
	case strings.Contains(h, "tiktok"):
		return "TikTok"
	case strings.Contains(h, "youtube"):
		return "YouTube"
	case strings.Contains(h, "messenger"):
		return "Messenger"
	}
	return h
}

// ---------------------------------------------------------------- eszköz, rendszer, böngésző

var (
	reAndroid   = regexp.MustCompile(`Android (\d+)`)
	reAndModel  = regexp.MustCompile(`Android [\d.]+; (?:[a-zA-Z-]+; )?([^;)]+?)(?: Build/[^;)]*)?\)`)
	reIOS       = regexp.MustCompile(`OS (\d+)[_\d]* like Mac OS X`)
	reWin       = regexp.MustCompile(`Windows NT (\d+\.\d+)`)
	reSamsung   = regexp.MustCompile(`^SM-([A-Z])(\d)(\d)`)
	reChromeVer = regexp.MustCompile(`(?:Chrome|CriOS)/(\d+)`)
)

// Samsung típuskódok → nevek (a legelterjedtebbek)
var samsungS = map[string]string{"9": "Galaxy S25", "2": "Galaxy S24", "1": "Galaxy S23", "0": "Galaxy S22"}

func niceModel(m string) string {
	m = strings.TrimSpace(m)
	if m == "" || m == "K" || strings.EqualFold(m, "wv") {
		return ""
	}
	if g := reSamsung.FindStringSubmatch(m); g != nil {
		switch g[1] {
		case "S":
			if g[2] == "9" {
				if n, ok := samsungS[g[3]]; ok {
					return n + " (" + m + ")"
				}
			}
		case "A":
			return "Galaxy A" + g[2] + g[3] + " (" + m + ")"
		case "G":
			return "Galaxy (" + m + ")"
		case "F":
			return "Galaxy Z (" + m + ")"
		case "M":
			return "Galaxy M" + g[2] + g[3] + " (" + m + ")"
		case "X", "T", "P":
			return "Galaxy Tab (" + m + ")"
		}
		return "Samsung " + m
	}
	return clip(m, 40)
}

type uaInfo struct{ Device, OS, Browser, Model string }

func parseUA(r *http.Request, width int) uaInfo {
	ua := r.UserAgent()
	l := strings.ToLower(ua)
	var u uaInfo
	// rendszer
	switch {
	case strings.Contains(ua, "iPhone"):
		u.OS, u.Model = "iOS", "iPhone"
	case strings.Contains(ua, "iPad"):
		u.OS, u.Model = "iPadOS", "iPad"
	case strings.Contains(ua, "Android"):
		u.OS = "Android"
	case strings.Contains(ua, "Windows"):
		u.OS = "Windows"
	case strings.Contains(ua, "Mac OS X") || strings.Contains(ua, "Macintosh"):
		u.OS = "macOS"
	case strings.Contains(ua, "CrOS"):
		u.OS = "ChromeOS"
	case strings.Contains(ua, "Linux"):
		u.OS = "Linux"
	default:
		u.OS = "egyéb"
	}
	if g := reIOS.FindStringSubmatch(ua); g != nil && (u.OS == "iOS" || u.OS == "iPadOS") {
		u.OS += " " + g[1]
	}
	if g := reAndroid.FindStringSubmatch(ua); g != nil {
		u.OS = "Android " + g[1]
	}
	if g := reWin.FindStringSubmatch(ua); g != nil && u.OS == "Windows" {
		u.OS = "Windows " + map[string]string{"10.0": "10/11", "6.3": "8.1", "6.1": "7"}[g[1]]
		u.OS = strings.TrimSpace(u.OS)
	}
	// ügyféltippek (Chrome/Edge/Samsung küldi, ha a weboldal kéri): pontos típus és rendszerverzió
	if m := strings.Trim(r.Header.Get("Sec-CH-UA-Model"), `"`); m != "" {
		u.Model = m
	} else if g := reAndModel.FindStringSubmatch(ua); g != nil && strings.HasPrefix(u.OS, "Android") {
		u.Model = g[1]
	}
	if pv := strings.Trim(r.Header.Get("Sec-CH-UA-Platform-Version"), `"`); pv != "" {
		major := strings.Split(pv, ".")[0]
		switch {
		case strings.HasPrefix(u.OS, "Android"):
			u.OS = "Android " + major
		case strings.HasPrefix(u.OS, "Windows"):
			if n, _ := strconv.Atoi(major); n >= 13 {
				u.OS = "Windows 11"
			} else if n > 0 {
				u.OS = "Windows 10"
			}
		}
	}
	if u.Model != "iPhone" && u.Model != "iPad" {
		u.Model = niceModel(u.Model)
	}
	// böngésző
	switch {
	case strings.Contains(ua, "FBAN") || strings.Contains(ua, "FBAV") || strings.Contains(ua, "FB_IAB"):
		u.Browser = "Facebook app"
	case strings.Contains(ua, "Instagram"):
		u.Browser = "Instagram app"
	case strings.Contains(l, "messenger"):
		u.Browser = "Messenger app"
	case strings.Contains(ua, "SamsungBrowser"):
		u.Browser = "Samsung Internet"
	case strings.Contains(ua, "EdgA") || strings.Contains(ua, "EdgiOS") || strings.Contains(ua, "Edg/"):
		u.Browser = "Edge"
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera"):
		u.Browser = "Opera"
	case strings.Contains(ua, "Firefox") || strings.Contains(ua, "FxiOS"):
		u.Browser = "Firefox"
	case strings.Contains(ua, "CriOS") || strings.Contains(ua, "Chrome"):
		u.Browser = "Chrome"
	case strings.Contains(ua, "Safari"):
		u.Browser = "Safari"
	default:
		u.Browser = "egyéb"
	}
	// eszköz típusa
	mob := r.Header.Get("Sec-CH-UA-Mobile")
	switch {
	case strings.Contains(ua, "iPad") || strings.Contains(l, "tablet") || (strings.Contains(ua, "Android") && !strings.Contains(ua, "Mobile") && mob != "?1"):
		u.Device = "tablet"
	case strings.Contains(l, "mobi") || strings.Contains(ua, "iPhone") || mob == "?1" || (width > 0 && width < 700):
		u.Device = "telefon"
	default:
		u.Device = "számítógép"
	}
	return u
}

func screenBucket(w, h int) string {
	if w <= 0 {
		return ""
	}
	return strconv.Itoa(w) + "×" + strconv.Itoa(h)
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

func addUnique(list []string, v string, max int) []string {
	if v == "" {
		return list
	}
	for _, x := range list {
		if x == v {
			return list
		}
	}
	if len(list) >= max {
		return list
	}
	return append(list, v)
}

// ---------------------------------------------------------------- böngészőből jövő jelzések

type beacon struct {
	T   string `json:"t"`   // view | ping | sec | ev | err
	P   string `json:"p"`   // oldal (/, /en, /de, /rendeles)
	L   string `json:"l"`   // az oldal nyelve
	R   string `json:"r"`   // honnan jött (referrer)
	Q   string `json:"q"`   // az URL lekérdezése (utm_… kampány)
	W   int    `json:"w"`   // képernyő szélesség
	H   int    `json:"h"`   // képernyő magasság
	Dk  bool   `json:"dk"`  // sötét mód
	C   string `json:"c"`   // kapcsolat (4g, 3g…)
	BL  string `json:"bl"`  // a böngésző nyelve
	S   string `json:"s"`   // éppen nézett szakasz
	D   int    `json:"d"`   // mennyit görgetett le (%)
	N   string `json:"n"`   // esemény / szakasz neve
	V   string `json:"v"`   // érték (pl. étel azonosító, település)
	Ok  *bool  `json:"ok"`  // település: szállítunk-e oda
	Msg string `json:"msg"` // hibaüzenet
	Src string `json:"src"` // hiba helye
}

var allowedEvents = map[string]bool{"add": true, "wheel": true, "wheel_order": true, "town": true, "form": true, "lang": true, "call": true,
	"route": true, "fb": true, "order_sent": true, "order_page": true, "order_btn": true, "drinks": true, "search": true, "gallery": true, "alg": true, "quick": true}
var allowedSections = map[string]bool{"hero": true, "rolunk": true, "etlap": true, "rendezvenyek": true, "kiszallitas": true, "galeria": true, "kapcsolat": true}

func campaignOf(q string) string {
	v, err := url.ParseQuery(strings.TrimPrefix(q, "?"))
	if err != nil {
		return ""
	}
	c := v.Get("utm_campaign")
	if s := v.Get("utm_source"); s != "" {
		if c != "" {
			c = s + " · " + c
		} else {
			c = s
		}
	}
	if c == "" && v.Get("fbclid") != "" {
		c = "Facebook link"
	}
	return clip(c, 60)
}

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
	ss := st.f.Sessions[d.Date]
	if ss == nil {
		ss = map[string]*Session{}
		st.f.Sessions[d.Date] = ss
	}
	s := ss[id]
	newSession := s == nil
	if newSession {
		if len(ss) >= 3000 { // védelem: egy napra legfeljebb ennyi részletes látogatás
			s = &Session{}
		} else {
			u := parseUA(r, b.W)
			city, region := cityOf(r)
			s = &Session{ID: id[:6], First: now, Last: now, Entry: page, Device: u.Device, OS: u.OS, Browser: u.Browser, Model: u.Model,
				Country: countryOf(r), City: city, Region: region, Lang: b.L, Pages: []string{}, Sections: []string{}, Events: []SessEv{}}
			ss[id] = s
		}
	}
	s.Last = now
	if b.S != "" && allowedSections[b.S] {
		s.Section = b.S
	}
	if b.D > s.MaxScroll && b.D <= 100 {
		s.MaxScroll = b.D
	}
	addEv := func(n, v string) {
		if len(s.Events) < 60 {
			s.Events = append(s.Events, SessEv{At: now, N: n, V: clip(v, 60)})
		}
	}
	switch b.T {
	case "view":
		h := now.In(budapest).Hour()
		d.Views++
		d.HourViews[h]++
		s.Views++
		s.Pages = addUnique(s.Pages, page, 10)
		if s.Screen == "" {
			bl := strings.ToLower(strings.Split(strings.Split(b.BL, "@")[0], ".")[0]) // pl. "en-us@posix" → "en-us"
			s.Screen, s.Dark, s.Conn, s.BLang = screenBucket(b.W, b.H), b.Dk, clip(b.C, 10), clip(strings.ReplaceAll(bl, "_", "-"), 12)
			if s.Ref == "" {
				s.Ref = refHost(b.R, r.Host)
			}
			s.Campaign = campaignOf(b.Q)
		}
		if !d.seen[id] {
			d.seen[id] = true
			d.Visitors++
			inc(d.Devices, s.Device)
			inc(d.Countries, s.Country)
			inc(d.OS, s.OS)
			inc(d.Browsers, s.Browser)
			inc(d.Models, s.Model)
			inc(d.Screens, s.Screen)
			inc(d.Conns, s.Conn)
			inc(d.BLangs, strings.Split(s.BLang, "-")[0])
			inc(d.Campaigns, s.Campaign)
			if s.City != "" {
				inc(d.Cities, s.City)
			}
			if s.Dark {
				d.Dark++
			}
			if s.Ref != "" {
				inc(d.Refs, clip(s.Ref, 60))
			}
		}
		if !d.hourSeen[h][id] {
			d.hourSeen[h][id] = true
			d.HourVis[h]++
		}
		inc(d.Pages, page)
		if b.L == "hu" || b.L == "en" || b.L == "de" {
			inc(d.Langs, b.L)
			s.Lang = b.L
		}
		if s.Views > 1 {
			addEv("page", page)
		}
	case "ping":
		// csak életjel: a fenti s.Last / szakasz / görgetés frissítés elég
	case "sec":
		if allowedSections[b.N] {
			inc(d.Sections, b.N)
			s.Sections = addUnique(s.Sections, b.N, 10)
		}
	case "ev":
		if !allowedEvents[b.N] {
			return
		}
		inc(d.Events, b.N)
		v := b.V
		switch b.N {
		case "add", "wheel_order":
			inc(d.Adds, clip(b.V, 60))
			s.Added++
		case "town":
			if b.Ok != nil && *b.Ok {
				inc(d.Towns, clip(b.V, 40))
			} else {
				inc(d.TownsNo, strings.ToLower(clip(b.V, 40)))
				v = b.V + " (nem szállítunk oda)"
			}
		case "order_sent":
			s.Ordered = true
		}
		addEv(b.N, v)
	case "err":
		d.Errors++
		addEv("error", b.Msg)
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
	mux.HandleFunc("GET /sessions", handleSessions)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		upMu.RLock()
		up := upstream.String()
		upMu.RUnlock()
		ok := fetchHealth(orderURL().String())
		st.Lock()
		ec := st.errCount
		st.Unlock()
		writeJSONs(w, map[string]any{"version": version, "started": st.started, "upstream": up, "upstream_ok": ok,
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

type liveOut struct {
	*Session
	Ago int `json:"ago"` // hány másodperce jelentkezett utoljára
	Dur int `json:"dur"` // mióta van az oldalon (mp)
}

// liveSessions: az utolsó 90 másodpercben jelentkezett látogatók (lock alatt).
func liveSessions(now time.Time) []liveOut {
	out := []liveOut{}
	for _, day := range []string{dayKey(now), dayKey(now.Add(-2 * time.Minute))} {
		for _, s := range st.f.Sessions[day] {
			if now.Sub(s.Last) <= 90*time.Second {
				out = append(out, liveOut{Session: s, Ago: int(now.Sub(s.Last).Seconds()), Dur: int(s.Last.Sub(s.First).Seconds())})
			}
		}
		if dayKey(now) == dayKey(now.Add(-2*time.Minute)) {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].First.After(out[j].First) })
	return out
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
	recompute()
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
	sdays := []string{}
	for k := range st.f.Sessions {
		sdays = append(sdays, k)
	}
	sort.Strings(sdays)
	writeJSONs(w, map[string]any{"now": now, "live": liveSessions(now), "days": days, "errors": errs,
		"errors_since_start": st.errCount, "version": version, "session_days": sdays})
}

// GET /sessions?date=2026-10-20 – egy nap látogatásai részletesen (legfeljebb 7 napra visszamenőleg)
func handleSessions(w http.ResponseWriter, r *http.Request) {
	now := time.Now()
	date := r.URL.Query().Get("date")
	if date == "" {
		date = dayKey(now)
	}
	st.Lock()
	defer st.Unlock()
	out := []liveOut{}
	for _, s := range st.f.Sessions[date] {
		out = append(out, liveOut{Session: s, Ago: int(now.Sub(s.Last).Seconds()), Dur: int(s.Last.Sub(s.First).Seconds())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].First.After(out[j].First) })
	writeJSONs(w, map[string]any{"date": date, "sessions": out})
}
