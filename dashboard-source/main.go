// Vadvirág Irányítópult – rendelések, látogatottság, állapotfigyelés és ntfy értesítések.
//
// Az Umbrel csak bejelentkezés után engedi megnyitni (PROXY_AUTH_ADD), így csak
// a tulajdonos látja. Az adatokat a másik két app belső portjáról kérdezi le,
// amelyek nincsenek kivezetve se a gépre, se az internetre.
package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	_ "time/tzdata"
)

const version = "1.2.2"

//go:embed web
var webRoot embed.FS

var (
	budapest, _ = time.LoadLocation("Europe/Budapest")
	dataDir     = env("DATA_DIR", "./data")
	siteStats   = splitList(env("SITE_STATS", "http://vadvirag-weboldal_web_1:9090"))
	ordersDash  = splitList(env("ORDERS_DASH", "http://vadvirag-rendelesek_web_1:9091"))
	ordersPub   = splitList(env("ORDERS_PUBLIC", "http://vadvirag-rendelesek_web_1:8080,http://host.docker.internal:4747"))
	httpc       = &http.Client{Timeout: 6 * time.Second}
	started     = time.Now()
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

// ---------------------------------------------------------------- beállítások

type Settings struct {
	NtfyServer     string          `json:"ntfy_server"`
	NtfyTopic      string          `json:"ntfy_topic"`
	NtfyToken      string          `json:"ntfy_token,omitempty"`
	PublicURL      string          `json:"public_url"`
	OrdersURL      string          `json:"orders_url"` // a rendeléskezelő címe (az értesítésre koppintva ez nyílik meg)
	PendingMinutes int             `json:"pending_minutes"`
	DailySummary   bool            `json:"daily_summary"`
	SummaryTime    string          `json:"summary_time"`
	NotifyNewOrder bool            `json:"notify_new_order"`
	NotifyMessages bool            `json:"notify_messages"`
	Muted          map[string]bool `json:"muted,omitempty"` // kikapcsolt figyelések
}

func defaultSettings() Settings {
	return Settings{NtfyServer: "https://ntfy.sh", PublicURL: "https://www.vadviragetterem.com", PendingMinutes: 10,
		DailySummary: true, SummaryTime: "22:15", Muted: map[string]bool{}}
}

var (
	setMu    sync.Mutex
	settings = defaultSettings()
)

func loadJSON(name string, v any) {
	if b, err := os.ReadFile(filepath.Join(dataDir, name)); err == nil {
		json.Unmarshal(b, v)
	}
}

func saveJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dataDir, name)
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func getSettings() Settings {
	setMu.Lock()
	defer setMu.Unlock()
	s := settings
	m := map[string]bool{}
	for k, v := range settings.Muted {
		m[k] = v
	}
	s.Muted = m
	return s
}

// ---------------------------------------------------------------- adatlekérés a másik két apptól

func getJSON(bases []string, path string, v any) (string, error) {
	var last error
	for _, b := range bases {
		resp, err := httpc.Get(b + path)
		if err != nil {
			last = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			last = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if err := json.Unmarshal(body, v); err != nil {
			last = err
			continue
		}
		return b, nil
	}
	if last == nil {
		last = errors.New("nincs megadott cím")
	}
	return "", last
}

type Order struct {
	ID          int       `json:"id"`
	Source      string    `json:"source"`
	Created     time.Time `json:"created"`
	Name        string    `json:"name"`
	Mode        string    `json:"mode"`
	City        string    `json:"city"`
	Items       string    `json:"items"`
	Total       int       `json:"total"`
	DeliveryFee int       `json:"delivery_fee"`
	Payment     string    `json:"payment"`
	Status      string    `json:"status"`
	StatusAt    time.Time `json:"status_at"`
	AcceptedAt  time.Time `json:"accepted_at"`
	ETA         int       `json:"eta"`
	Reason      string    `json:"reason"`
	EmailState  string    `json:"email_state"`
	EmailErr    string    `json:"email_err"`
	Customer    string    `json:"cust"`
	Visitor     string    `json:"visitor,omitempty"`
}

type Message struct {
	ID     int       `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	Date   string    `json:"date"`
	Time   string    `json:"time"`
	Guests int       `json:"guests"`
	Done   bool      `json:"done"`
}

type siteHealth struct {
	Version          string    `json:"version"`
	Started          time.Time `json:"started"`
	Upstream         string    `json:"upstream"`
	UpstreamOK       bool      `json:"upstream_ok"`
	MenuLive         bool      `json:"menu_live"`
	ErrorsSinceStart int       `json:"errors_since_start"`
	SiteURL          string    `json:"site_url"`
}

type siteErr struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Msg  string    `json:"msg"`
	Src  string    `json:"src"`
	Page string    `json:"page"`
}

type siteStatsResp struct {
	Now              time.Time         `json:"now"`
	Live             []json.RawMessage `json:"live"`
	Days             []json.RawMessage `json:"days"`
	Errors           []siteErr         `json:"errors"`
	ErrorsSinceStart int               `json:"errors_since_start"`
	Version          string            `json:"version"`
}

// ---------------------------------------------------------------- állapotfigyelés

type Check struct {
	Key     string    `json:"key"`
	Label   string    `json:"label"`
	OK      bool      `json:"ok"`
	Known   bool      `json:"known"` // volt már mérés
	Detail  string    `json:"detail"`
	Since   time.Time `json:"since"` // mióta ebben az állapotban
	LastRun time.Time `json:"last_run"`
	fails   int
	alerted bool
}

type Alert struct {
	At       time.Time `json:"at"`
	Key      string    `json:"key"`
	Title    string    `json:"title"`
	Msg      string    `json:"msg"`
	Level    string    `json:"level"` // error | ok | info
	Sent     bool      `json:"sent"`
	SendErr  string    `json:"send_err,omitempty"`
	Priority int       `json:"priority"`
}

type monitorState struct {
	sync.Mutex
	checks        map[string]*Check
	order         []string
	alerts        []Alert
	pendingSent   map[int]bool
	emailSent     map[int]bool
	seenOrders    map[int]bool
	seenMsgs      map[int]bool
	ordersPrimed  bool
	msgsPrimed    bool
	lastErrAt     time.Time
	lastErrNotify time.Time
	errBuffer     []siteErr
	lastSummary   string
	lastPublic    time.Time
}

var mon = &monitorState{checks: map[string]*Check{}, pendingSent: map[int]bool{}, emailSent: map[int]bool{},
	seenOrders: map[int]bool{}, seenMsgs: map[int]bool{}}

type persisted struct {
	Alerts      []Alert      `json:"alerts"`
	PendingSent map[int]bool `json:"pending_sent"`
	EmailSent   map[int]bool `json:"email_sent"`
	LastErrAt   time.Time    `json:"last_err_at"`
	LastSummary string       `json:"last_summary"`
}

func (m *monitorState) save() {
	m.Lock()
	p := persisted{Alerts: m.alerts, PendingSent: m.pendingSent, EmailSent: m.emailSent, LastErrAt: m.lastErrAt, LastSummary: m.lastSummary}
	m.Unlock()
	saveJSON("allapot.json", p)
}

var checkDefs = []struct{ key, label string }{
	{"site", "Weboldal"},
	{"orders", "Rendeléskezelő"},
	{"link", "Online rendelés a weboldalon"},
	{"public", "vadviragetterem.com kívülről"},
	{"disk", "Tárhely"},
}

func (m *monitorState) check(key string) *Check {
	c := m.checks[key]
	if c == nil {
		label := key
		for _, d := range checkDefs {
			if d.key == key {
				label = d.label
			}
		}
		c = &Check{Key: key, Label: label, OK: true, Since: time.Now()}
		m.checks[key] = c
	}
	return c
}

// result: egy mérés eredménye. Két egymás utáni hiba után riaszt, javuláskor szól.
func result(key string, ok bool, detail, failTitle, failMsg, okMsg string, need int) {
	mon.Lock()
	c := mon.check(key)
	c.LastRun = time.Now()
	wasKnown := c.Known
	c.Known = true
	c.Detail = detail
	var send *Alert
	if ok {
		c.fails = 0
		if !c.OK || !wasKnown {
			c.Since = time.Now()
		}
		c.OK = true
		if c.alerted {
			c.alerted = false
			send = &Alert{Key: key, Title: "✅ Helyreállt: " + c.Label, Msg: okMsg, Level: "ok", Priority: 3}
		}
	} else {
		c.fails++
		if c.OK {
			c.Since = time.Now()
		}
		c.OK = false
		if c.fails >= need && !c.alerted {
			c.alerted = true
			send = &Alert{Key: key, Title: failTitle, Msg: failMsg + "\n" + detail, Level: "error", Priority: 5}
		}
	}
	mon.Unlock()
	if send != nil {
		notify(*send)
	}
}

func notify(a Alert) {
	a.At = time.Now()
	s := getSettings()
	if s.Muted[a.Key] && a.Level != "test" {
		a.SendErr = "kikapcsolva"
	} else if s.NtfyTopic == "" {
		a.SendErr = "nincs megadva ntfy téma"
	} else if err := sendNtfy(s, a); err != nil {
		a.SendErr = err.Error()
	} else {
		a.Sent = true
	}
	log.Printf("értesítés [%s] %s – %s (%v %s)", a.Key, a.Title, strings.ReplaceAll(a.Msg, "\n", " | "), a.Sent, a.SendErr)
	mon.Lock()
	mon.alerts = append(mon.alerts, a)
	if len(mon.alerts) > 300 {
		mon.alerts = mon.alerts[len(mon.alerts)-300:]
	}
	mon.Unlock()
	mon.save()
}

func sendNtfy(s Settings, a Alert) error {
	tags := map[string][]string{"error": {"rotating_light"}, "ok": {"white_check_mark"}, "info": {"tulip"}, "order": {"shopping_cart"}, "test": {"bell"}}[a.Level]
	body := map[string]any{"topic": s.NtfyTopic, "title": a.Title, "message": a.Msg, "priority": a.Priority, "tags": tags}
	if a.Key == "order" || a.Key == "pending" || a.Key == "email" {
		if s.OrdersURL != "" {
			body["click"] = s.OrdersURL
		}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", strings.TrimRight(s.NtfyServer, "/"), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.NtfyToken != "" {
		req.Header.Set("Authorization", "Bearer "+s.NtfyToken)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("ntfy %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}

func huf(n int) string {
	s := fmt.Sprint(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}

// runChecks: percenként
func runChecks() {
	s := getSettings()
	// 1. weboldal
	var h siteHealth
	_, err := getJSON(siteStats, "/health", &h)
	result("site", err == nil, errText(err, "verzió "+h.Version),
		"🚨 A weboldal nem válaszol", "A Vadvirág Weboldal app nem érhető el az Umbrelen. Nézd meg, fut-e az app.", "A weboldal újra válaszol.", 2)
	// 2. rendeléskezelő
	var info map[string]any
	_, err2 := getJSON(ordersDash, "/dash/info", &info)
	result("orders", err2 == nil, errText(err2, "fogadja a rendeléseket"),
		"🚨 A rendeléskezelő nem válaszol", "A Vadvirág Rendelések app nem érhető el. Online rendelést most nem lehet fogadni.", "A rendeléskezelő újra válaszol.", 2)
	// 3. a weboldal eléri-e a rendeléskezelőt
	if err == nil {
		result("link", h.UpstreamOK, map[bool]string{true: "a weboldal eléri a rendeléskezelőt", false: "a weboldal nem éri el a rendeléskezelőt (" + h.Upstream + ")"}[h.UpstreamOK],
			"🚨 Az online rendelés nem működik", "A weboldal nem éri el a rendeléskezelőt, a vendégek nem tudnak rendelni.", "A weboldal újra eléri a rendeléskezelőt.", 2)
	}
	// 4. kívülről (5 percenként)
	mon.Lock()
	duePublic := s.PublicURL != "" && time.Since(mon.lastPublic) >= 5*time.Minute
	if duePublic {
		mon.lastPublic = time.Now()
	}
	mon.Unlock()
	if duePublic {
		ok, detail := checkPublic(s.PublicURL)
		result("public", ok, detail, "🚨 A vadviragetterem.com nem érhető el", "A weboldal kívülről (az interneten át) nem tölt be.", "A vadviragetterem.com újra elérhető.", 2)
	}
	// 5. tárhely
	if free, total, err := diskFree(dataDir); err == nil && total > 0 {
		pct := float64(free) / float64(total) * 100
		result("disk", pct >= 10, fmt.Sprintf("%.0f%% szabad (%.1f GB / %.1f GB)", pct, float64(free)/1e9, float64(total)/1e9),
			"⚠️ Kevés a szabad hely az Umbrelen", "A tárhely majdnem megtelt; a rendelések mentése elakadhat.", "Van újra elég szabad hely.", 1)
	}
	if err2 == nil {
		checkOrders(s)
		checkMessages(s)
	}
	if err == nil {
		checkSiteErrors()
	}
	checkSummary(s)
}

func errText(err error, ok string) string {
	if err != nil {
		return "nem elérhető: " + err.Error()
	}
	return ok
}

func checkPublic(base string) (bool, string) {
	u := strings.TrimRight(base, "/") + "/healthz"
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Vadvirag-Iranyitopult/"+version+" (monitor)")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return false, "hiba: " + err.Error()
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode == 200 && strings.TrimSpace(string(b)) == "ok" {
		return true, "elérhető (" + u + ")"
	}
	d := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if cf := cfErrorCode(string(b)); cf != "" {
		d += ", Cloudflare hiba " + cf
	}
	return false, d + " – " + u
}

func cfErrorCode(s string) string {
	i := strings.Index(s, "Error ")
	if i < 0 {
		return ""
	}
	s = s[i+6:]
	j := 0
	for j < len(s) && j < 5 && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	return s[:j]
}

func diskFree(p string) (free, total uint64, err error) {
	var fs syscall.Statfs_t
	if err = syscall.Statfs(p, &fs); err != nil {
		return
	}
	return fs.Bavail * uint64(fs.Bsize), fs.Blocks * uint64(fs.Bsize), nil
}

func openNow(t time.Time) bool {
	h := t.In(budapest).Hour()
	return h >= 11 && h < 22
}

func checkOrders(s Settings) {
	var orders []Order
	if _, err := getJSON(ordersDash, "/dash/orders?days=2", &orders); err != nil {
		return
	}
	now := time.Now()
	var send []Alert
	mon.Lock()
	for _, o := range orders {
		// új rendelés
		if !mon.seenOrders[o.ID] {
			mon.seenOrders[o.ID] = true
			if mon.ordersPrimed && s.NotifyNewOrder {
				where := o.Mode
				if o.City != "" {
					where += ", " + o.City
				}
				t := ""
				if o.Total > 0 {
					t = " – " + huf(o.Total) + " Ft"
				}
				send = append(send, Alert{Key: "order", Title: fmt.Sprintf("🛒 Új rendelés #%d%s", o.ID, t), Msg: where + "\n" + o.Items, Level: "order", Priority: 4})
			}
		}
		// túl régóta vár
		if o.Status == "new" && s.PendingMinutes > 0 && now.Sub(o.Created) > time.Duration(s.PendingMinutes)*time.Minute &&
			now.Sub(o.Created) < 12*time.Hour && !mon.pendingSent[o.ID] {
			mon.pendingSent[o.ID] = true
			send = append(send, Alert{Key: "pending", Title: fmt.Sprintf("⏰ Rendelés #%d már %d perce vár", o.ID, int(now.Sub(o.Created).Minutes())),
				Msg: "Még senki nem fogadta el vagy utasította el.\n" + o.Items, Level: "error", Priority: 5})
		}
		// a vendégnek szóló e-mail nem ment el
		if o.EmailState == "failed" && !mon.emailSent[o.ID] && now.Sub(o.Created) < 24*time.Hour {
			mon.emailSent[o.ID] = true
			send = append(send, Alert{Key: "email", Title: fmt.Sprintf("✉️ Nem ment el az e-mail (#%d)", o.ID),
				Msg: "A vendég nem kapta meg a visszaigazoló e-mailt: " + o.EmailErr + "\nÉrdemes felhívni.", Level: "error", Priority: 4})
		}
	}
	mon.ordersPrimed = true
	mon.Unlock()
	for _, a := range send {
		notify(a)
	}
}

func checkMessages(s Settings) {
	var msgs []Message
	if _, err := getJSON(ordersDash, "/dash/messages", &msgs); err != nil {
		return
	}
	var send []Alert
	kinds := map[string]string{"asztal": "Asztalfoglalás", "rendezveny": "Rendezvény ajánlatkérés", "egyeb": "Kérdés"}
	mon.Lock()
	for _, m := range msgs {
		if !mon.seenMsgs[m.ID] {
			mon.seenMsgs[m.ID] = true
			if mon.msgsPrimed && s.NotifyMessages {
				d := m.Name
				if m.Date != "" {
					d += ", " + m.Date + " " + m.Time
				}
				if m.Guests > 0 {
					d += fmt.Sprintf(", %d fő", m.Guests)
				}
				send = append(send, Alert{Key: "message", Title: "✉️ Új üzenet a weboldalról: " + kinds[m.Kind], Msg: d, Level: "info", Priority: 3})
			}
		}
	}
	mon.msgsPrimed = true
	mon.Unlock()
	for _, a := range send {
		notify(a)
	}
}

// checkSiteErrors: a weboldal hibái (böngésző és szerver). Legfeljebb 30 percenként egy összesítő értesítés.
func checkSiteErrors() {
	var st siteStatsResp
	if _, err := getJSON(siteStats, "/stats?days=1", &st); err != nil {
		return
	}
	mon.Lock()
	var fresh []siteErr
	for _, e := range st.Errors {
		if e.At.After(mon.lastErrAt) {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) > 0 {
		mon.lastErrAt = fresh[len(fresh)-1].At
		for _, e := range fresh {
			if e.Kind != "upstream" { // azt a "link" figyelés jelzi
				mon.errBuffer = append(mon.errBuffer, e)
			}
		}
	}
	var send *Alert
	if len(mon.errBuffer) > 0 && time.Since(mon.lastErrNotify) > 30*time.Minute {
		var b strings.Builder
		for i, e := range mon.errBuffer {
			if i == 5 {
				fmt.Fprintf(&b, "… és még %d", len(mon.errBuffer)-5)
				break
			}
			kind := map[string]string{"js": "böngésző", "server": "szerver"}[e.Kind]
			fmt.Fprintf(&b, "• [%s] %s (%s)\n", kind, e.Msg, e.Page)
		}
		send = &Alert{Key: "errors", Title: fmt.Sprintf("⚠️ %d hiba a weboldalon", len(mon.errBuffer)), Msg: strings.TrimSpace(b.String()), Level: "error", Priority: 3}
		mon.errBuffer = nil
		mon.lastErrNotify = time.Now()
	}
	mon.Unlock()
	if send != nil {
		notify(*send)
	}
}

// checkSummary: napi összesítő a beállított időpontban
func checkSummary(s Settings) {
	if !s.DailySummary {
		return
	}
	now := time.Now().In(budapest)
	day := now.Format("2006-01-02")
	mon.Lock()
	done := mon.lastSummary == day
	mon.Unlock()
	// csak a beállított időpont utáni 3 órában (így egy késő esti újraindítás nem küldi újra / későn)
	at, err := time.ParseInLocation("15:04", s.SummaryTime, budapest)
	if done || err != nil {
		return
	}
	at = time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, budapest)
	if now.Before(at) || now.After(at.Add(3*time.Hour)) {
		return
	}
	mon.Lock()
	mon.lastSummary = day
	mon.Unlock()
	var b strings.Builder
	var st siteStatsResp
	if _, err := getJSON(siteStats, "/stats?days=1", &st); err == nil && len(st.Days) > 0 {
		var d struct {
			Views    int `json:"views"`
			Visitors int `json:"visitors"`
		}
		json.Unmarshal(st.Days[len(st.Days)-1], &d)
		fmt.Fprintf(&b, "👀 %d látogató, %d oldalmegnyitás\n", d.Visitors, d.Views)
	}
	var orders []Order
	if _, err := getJSON(ordersDash, "/dash/orders?days=2", &orders); err == nil {
		n, rev, rej := 0, 0, 0
		dish := map[string]int{}
		for _, o := range orders {
			if o.Created.In(budapest).Format("2006-01-02") != day {
				continue
			}
			n++
			if o.Status == "rejected" {
				rej++
				continue
			}
			rev += o.Total
			for _, l := range strings.Split(o.Items, "\n") {
				var q int
				var rest string
				if _, err := fmt.Sscanf(l, "%d×", &q); err == nil {
					if i := strings.Index(l, "× "); i >= 0 {
						rest = l[i+len("× "):]
						if j := strings.LastIndex(rest, " – "); j >= 0 {
							rest = rest[:j]
						}
						dish[rest] += q
					}
				}
			}
		}
		fmt.Fprintf(&b, "🛒 %d rendelés", n)
		if rej > 0 {
			fmt.Fprintf(&b, " (%d elutasítva)", rej)
		}
		if rev > 0 {
			fmt.Fprintf(&b, ", %s Ft online", huf(rev))
		}
		b.WriteString("\n")
		type kv struct {
			k string
			v int
		}
		var top []kv
		for k, v := range dish {
			top = append(top, kv{k, v})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
		if len(top) > 0 {
			fmt.Fprintf(&b, "⭐ Legnépszerűbb: %s (%d db)", top[0].k, top[0].v)
		}
	}
	mon.save()
	notify(Alert{Key: "summary", Title: "🌺 Vadvirág – napi összesítő", Msg: strings.TrimSpace(b.String()), Level: "info", Priority: 2})
}

// ---------------------------------------------------------------- HTTP

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

var (
	menuMu    sync.Mutex
	menuNames map[string]string
	menuAt    time.Time
)

func dishNames() map[string]string {
	menuMu.Lock()
	defer menuMu.Unlock()
	if menuNames != nil && time.Since(menuAt) < 10*time.Minute {
		return menuNames
	}
	var m struct {
		Sections []struct {
			Items []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"items"`
		} `json:"sections"`
	}
	if _, err := getJSON(ordersPub, "/api/public/menu", &m); err == nil {
		names := map[string]string{}
		for _, s := range m.Sections {
			for _, it := range s.Items {
				names[it.ID] = it.Name
			}
		}
		menuNames, menuAt = names, time.Now()
	}
	if menuNames == nil {
		return map[string]string{}
	}
	return menuNames
}

func handleOverview(w http.ResponseWriter, r *http.Request) {
	days := r.URL.Query().Get("days")
	if days == "" {
		days = "30"
	}
	out := map[string]any{"version": version, "now": time.Now()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	set := func(k string, v any) { mu.Lock(); out[k] = v; mu.Unlock() }
	wg.Add(5)
	go func() {
		defer wg.Done()
		var st json.RawMessage
		if _, err := getJSON(siteStats, "/stats?days="+days, &st); err != nil {
			set("site_error", err.Error())
		} else {
			set("site", st)
		}
	}()
	go func() {
		defer wg.Done()
		var o []Order
		if _, err := getJSON(ordersDash, "/dash/orders?days="+days, &o); err != nil {
			set("orders_error", err.Error())
		} else {
			set("orders", o)
		}
	}()
	go func() {
		defer wg.Done()
		var m []Message
		if _, err := getJSON(ordersDash, "/dash/messages", &m); err == nil {
			set("messages", m)
		}
	}()
	go func() {
		defer wg.Done()
		var i map[string]any
		if _, err := getJSON(ordersDash, "/dash/info", &i); err == nil {
			set("orders_info", i)
		}
	}()
	go func() { defer wg.Done(); set("dish_names", dishNames()) }()
	wg.Wait()
	// a sütit elfogadó vendégek rendeléseihez: honnan jöttek, hányadik látogatásukon rendeltek
	if os, ok := out["orders"].([]Order); ok {
		seen, keys := map[string]bool{}, []string{}
		for _, o := range os {
			if o.Visitor != "" && !seen[o.Visitor] && len(keys) < 400 {
				seen[o.Visitor] = true
				keys = append(keys, o.Visitor)
			}
		}
		if len(keys) > 0 {
			var known json.RawMessage
			if _, err := getJSON(siteStats, "/known?ck="+strings.Join(keys, ","), &known); err == nil {
				out["known"] = known
			}
		}
	}
	mon.Lock()
	checks := []Check{}
	for _, d := range checkDefs {
		if c := mon.checks[d.key]; c != nil {
			checks = append(checks, *c)
		} else {
			checks = append(checks, Check{Key: d.key, Label: d.label})
		}
	}
	alerts := append([]Alert(nil), mon.alerts...)
	mon.Unlock()
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].At.After(alerts[j].At) })
	if len(alerts) > 100 {
		alerts = alerts[:100]
	}
	out["checks"] = checks
	out["alerts"] = alerts
	s := getSettings()
	s.NtfyToken = map[bool]string{true: "••••", false: ""}[s.NtfyToken != ""]
	out["settings"] = s
	writeJSON(w, 200, out)
}

func handleSettings(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	in.NtfyServer = strings.TrimRight(strings.TrimSpace(in.NtfyServer), "/")
	if in.NtfyServer == "" {
		in.NtfyServer = "https://ntfy.sh"
	}
	if !strings.HasPrefix(in.NtfyServer, "http://") && !strings.HasPrefix(in.NtfyServer, "https://") {
		writeJSON(w, 400, map[string]string{"error": "Az ntfy szerver címe http:// vagy https:// kezdetű legyen."})
		return
	}
	in.NtfyTopic = strings.Trim(strings.TrimSpace(in.NtfyTopic), "/")
	for _, c := range in.NtfyTopic {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			writeJSON(w, 400, map[string]string{"error": "A témanév csak betűt, számot, kötőjelet és aláhúzást tartalmazhat (ékezet és szóköz nélkül)."})
			return
		}
	}
	if in.PendingMinutes < 0 || in.PendingMinutes > 240 {
		in.PendingMinutes = 10
	}
	if _, err := time.Parse("15:04", in.SummaryTime); err != nil {
		in.SummaryTime = "22:15"
	}
	setMu.Lock()
	if in.NtfyToken == "••••" {
		in.NtfyToken = settings.NtfyToken
	}
	if in.Muted == nil {
		in.Muted = map[string]bool{}
	}
	settings = in
	err := saveJSON("beallitasok.json", settings)
	setMu.Unlock()
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "Nem sikerült menteni: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func handleTest(w http.ResponseWriter, r *http.Request) {
	s := getSettings()
	if s.NtfyTopic == "" {
		writeJSON(w, 400, map[string]string{"error": "Előbb add meg és mentsd el az ntfy témát."})
		return
	}
	a := Alert{At: time.Now(), Key: "test", Title: "🔔 Vadvirág Irányítópult – próba", Msg: "Ha ezt látod a telefonodon, az értesítések működnek.", Level: "test", Priority: 4}
	if err := sendNtfy(s, a); err != nil {
		a.SendErr = err.Error()
		mon.Lock()
		mon.alerts = append(mon.alerts, a)
		mon.Unlock()
		writeJSON(w, 502, map[string]string{"error": "Nem sikerült elküldeni: " + err.Error()})
		return
	}
	a.Sent = true
	mon.Lock()
	mon.alerts = append(mon.alerts, a)
	mon.Unlock()
	mon.save()
	writeJSON(w, 200, map[string]bool{"ok": true})
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
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		files.ServeHTTP(w, r)
	}))
	addr := ":" + env("PORT", "8080")
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
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
