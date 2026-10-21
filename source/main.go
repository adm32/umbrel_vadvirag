package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed web/*
var webFS embed.FS

// ---------------------------------------------------------------- beállítások

type SMTPConf struct {
	Szerver     string `json:"szerver"`
	Port        int    `json:"port"`
	Felhasznalo string `json:"felhasznalo"`
	Jelszo      string `json:"jelszo"`
	FeladoNev   string `json:"felado_neve"`
	FeladoEmail string `json:"felado_email"`
}

type WixConf struct {
	APIKulcs      string `json:"api_kulcs"`
	SiteID        string `json:"site_id"`
	FormID        string `json:"form_id"`
	APICim        string `json:"api_cim"`
	LekerdezesSec int    `json:"lekerdezes_masodperc"`
}

type Config struct {
	Port           int            `json:"port"`
	Jelszo         string         `json:"jelszo"`
	EtteremNeve    string         `json:"etterem_neve"`
	EtteremTelefon string         `json:"etterem_telefon"`
	Titok          string         `json:"belso_titok"`
	WebhookKulcs   string         `json:"webhook_kulcs"`
	VapidPrivat    string         `json:"vapid_privat"`
	VapidPublikus  string         `json:"vapid_publikus"`
	SMTP           SMTPConf       `json:"email"`
	Wix            WixConf        `json:"wix"`
	Dijak          map[string]int `json:"szallitasi_dijak"`
	DijakVerzio    int            `json:"szallitasi_dijak_verzio"` // ha kisebb, mint a beépített tábla verziója, frissítjük
	IdoOpciok      []int          `json:"elkeszulesi_percek"`
	Okok           []string       `json:"elutasitas_okai"`
	// Online rendelés szüneteltetése (pl. zárva vagy túl sok a munka) – a vendég ezt látja.
	OnlineSzunetel bool   `json:"online_szunetel,omitempty"`
	OnlineUzenet   string `json:"online_uzenet,omitempty"`
}

func randStr(n int, alphabet string) string {
	b := make([]byte, n)
	rand.Read(b)
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out)
}

func defaultConfig() Config {
	return Config{
		Port:           8080,
		Jelszo:         randStr(8, "abcdefghjkmnpqrstuvwxyz23456789"),
		EtteremNeve:    "Vadvirág Étterem",
		EtteremTelefon: "+36 20 339 5099",
		Titok:          randStr(40, "abcdef0123456789"),
		WebhookKulcs:   randStr(24, "abcdefghjkmnpqrstuvwxyz23456789"),
		SMTP:           SMTPConf{Szerver: "", Port: 587, FeladoNev: "Vadvirág Étterem"},
		Wix: WixConf{
			SiteID:        "a2cb67a5-41c9-4cfb-bb42-2282a8a16198",
			FormID:        "eacf2b73-6d4f-420a-98a8-c51d026da098",
			APICim:        "https://www.wixapis.com",
			LekerdezesSec: 20,
		},
		Dijak:       defaultFees(),
		DijakVerzio: feeTableVersion,
		IdoOpciok:   []int{15, 20, 30, 45, 60, 90},
		Okok: []string{
			"Sajnos már túl késő van, a konyha bezárt",
			"Az egyik étel elfogyott",
			"Ide most nem tudunk kiszállítani",
			"Jelenleg nagyon sok a rendelés",
		},
	}
}

// Szállítási díjak: az étterem saját, 2026 októberi táblázata (44 település, valódi autós
// távolság alapján). Csak ezekre a településekre szállítunk.
// Ha a táblát módosítod, emeld a feeTableVersion értékét: induláskor a régi tábla lecserélődik.
const feeTableVersion = 5

func defaultFees() map[string]int {
	return map[string]int{
		"Zalaudvarnok": 200,
		"Kisszentgrót": 500, "Sénye": 500, "Zalaszentgrót": 500, "Zalaszentlászló": 500,
		"Aranyod": 700, "Gyülevész": 700, "Zalakoppány": 700,
		"Barátsziget": 900, "Csáford": 900, "Tüskeszentpéter": 900,
		"Kisgörbő": 1000,
		"Kallósd":  1100, "Tekenye": 1100, "Zalaköveskút": 1100,
		"Türje":  1200,
		"Kehida": 1300, "Kehidakustány": 1300, "Nagygörbő": 1300, "Zalabér": 1300, "Óhíd": 1300,
		"Döbröce":  1400,
		"Nemesbük": 1500, "Vindornyafok": 1500,
		"Batyk": 1600, "Szalapa": 1600, "Vindornyaszőlős": 1600,
		"Karmacs": 1700, "Mihályfa": 1700, "Sümegcsehi": 1700,
		"Pakod": 1800, "Zalacsány": 1800,
		"Zalavég":      1900,
		"Kisvásárhely": 2000, "Vindornyalak": 2000,
		"Bérbaltavár": 2100, "Bókaháza": 2100, "Dötk": 2100, "Felsőpáhok": 2100, "Szentgyörgyvár": 2100,
		"Ligetfalva": 2200,
		"Dabronc":    2300, "Gógánfa": 2300, "Tilaj": 2300,
	}
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, saveConfig(path, cfg)
	}
	if err != nil {
		return cfg, err
	}
	// a mentett tábla ne keveredjen a beépítettel, és a régi (verzió nélküli) fájl frissüljön
	cfg.Dijak, cfg.DijakVerzio = nil, 0
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("a beallitasok.json hibás (%v) – javítsd, vagy töröld, hogy újra létrejöjjön", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Wix.LekerdezesSec < 5 {
		cfg.Wix.LekerdezesSec = 20
	}
	if cfg.Wix.APICim == "" {
		cfg.Wix.APICim = "https://www.wixapis.com"
	}
	if cfg.Titok == "" {
		cfg.Titok = randStr(40, "abcdef0123456789")
	}
	if cfg.DijakVerzio < feeTableVersion || len(cfg.Dijak) == 0 {
		log.Printf("Szállítási díjak frissítése: %d település (tábla v%d)", len(defaultFees()), feeTableVersion)
		cfg.Dijak, cfg.DijakVerzio = defaultFees(), feeTableVersion
		if err := saveConfig(path, cfg); err != nil {
			log.Printf("a frissített díjtábla mentése nem sikerült: %v", err)
		}
	}
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, data, 0o600)
}

// ---------------------------------------------------------------- rendelések

const (
	StNew       = "new"
	StPreparing = "preparing"
	StDone      = "done"
	StRejected  = "rejected"
)

type Order struct {
	ID          int               `json:"id"`
	Source      string            `json:"source"`
	ExtID       string            `json:"ext_id,omitempty"`
	Created     time.Time         `json:"created"`
	Name        string            `json:"name"`
	Email       string            `json:"email"`
	Phone       string            `json:"phone"`
	Mode        string            `json:"mode"` // "Személyes átvétel" | "Kiszállítás"
	City        string            `json:"city"`
	Street      string            `json:"street"`
	Items       string            `json:"items"`
	Note        string            `json:"note,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	DeliveryFee int               `json:"delivery_fee"` // -1 = ismeretlen település
	Status      string            `json:"status"`
	ETA         int               `json:"eta,omitempty"`
	Reason      string            `json:"reason,omitempty"`
	StatusAt    time.Time         `json:"status_at"`
	AcceptedAt  time.Time         `json:"accepted_at,omitempty"` // mikor fogadtuk el (a vendég ehhez képest látja a várható időt)
	EmailState  string            `json:"email_state,omitempty"` // sending|sent|failed|none
	EmailErr    string            `json:"email_err,omitempty"`
	EmailAt     time.Time         `json:"email_at,omitempty"`
	Total       int               `json:"total,omitempty"`   // weboldali rendelésnél: végösszeg (szállítással, ha ismert)
	Payment     string            `json:"payment,omitempty"` // Készpénz | Kártya
	Token       string            `json:"token,omitempty"`   // a vendég ezzel követi a rendelése állapotát
	Visitor     string            `json:"visitor,omitempty"` // a weboldal sütijének hash-e (csak ha a vendég elfogadta) – az irányítópult ebből látja, honnan jött
}

const modePickup = "Személyes átvétel"
const modeDelivery = "Kiszállítás"

type Store struct {
	mu     sync.Mutex
	path   string
	Orders []*Order `json:"orders"`
	NextID int      `json:"next_id"`
}

func openStore(path string) (*Store, error) {
	s := &Store{path: path, NextID: 1}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("a rendelesek.json sérült: %v", err)
	}
	if s.NextID < 1 {
		s.NextID = 1
	}
	return s, nil
}

// saveLocked: hívó tartja a zárat.
func (s *Store) saveLocked() {
	data, _ := json.MarshalIndent(s, "", " ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("MENTÉSI HIBA: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("MENTÉSI HIBA: %v", err)
	}
}

// ---------------------------------------------------------------- app

type App struct {
	cfgMu     sync.RWMutex
	cfg       Config
	cfgPath   string
	dataDir   string
	container bool // Dockerben / Umbrelen fut
	st        *Store
	feeIx     []feeEntry

	wixMu     sync.Mutex
	wixLastOK time.Time
	wixErr    string
	labels    map[string]map[string]string
	labelTry  map[string]time.Time
}

type feeEntry struct {
	norm string
	name string
	fee  int
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ö", "o", "ő", "o", "ú", "u", "ü", "u", "ű", "u").Replace(s)
}

func newApp(cfg Config, st *Store) *App {
	a := &App{cfg: cfg, st: st, labels: map[string]map[string]string{}, labelTry: map[string]time.Time{}}
	for name, fee := range cfg.Dijak {
		a.feeIx = append(a.feeIx, feeEntry{norm(name), name, fee})
	}
	sort.Slice(a.feeIx, func(i, j int) bool { return len(a.feeIx[i].norm) > len(a.feeIx[j].norm) })
	return a
}

// conf: a beállítások pillanatnyi másolata (a beállítások menüből futás közben is változhatnak).
func (a *App) conf() Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

// updateConf: beállítás módosítása és mentése egy lépésben.
func (a *App) updateConf(fn func(c *Config)) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	c := a.cfg
	fn(&c)
	if err := saveConfig(a.cfgPath, c); err != nil {
		return err
	}
	a.cfg = c
	return nil
}

func (a *App) needsSetup() bool { return a.conf().Jelszo == "" }

func (a *App) fee(mode, city string) int {
	if mode != modeDelivery {
		return 0
	}
	c := norm(city)
	if c == "" {
		return -1
	}
	// A leghosszabb nevek jönnek először, és a megtalált nevet kitöröljük, hogy egy rövidebb
	// név ne találjon bele (Sümegcsehi ≠ Csehi, Nagytilaj ≠ Tilaj). Ha több település is
	// szerepel („Zalaszentgrót-Csáford”), a messzebbi (drágább) számít.
	best := -1
	for _, f := range a.feeIx {
		if strings.Contains(c, f.norm) {
			c = strings.ReplaceAll(c, f.norm, "|")
			best = max(best, f.fee)
		}
	}
	return best
}

// ingest: új rendelés felvétele. Visszatér: felvettük-e (false = már megvolt).
func (a *App) ingest(o *Order) bool {
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	if o.ExtID != "" {
		for _, x := range a.st.Orders {
			if x.ExtID == o.ExtID {
				return false
			}
		}
	}
	o.ID = a.st.NextID
	a.st.NextID++
	if o.Created.IsZero() {
		o.Created = time.Now()
	}
	o.Status = StNew
	o.StatusAt = time.Now()
	o.DeliveryFee = a.fee(o.Mode, o.City)
	a.st.Orders = append(a.st.Orders, o)
	a.st.saveLocked()
	log.Printf("Új rendelés #%d (%s, %s)", o.ID, o.Name, o.Source)
	go a.notifyNewOrder(*o)
	return true
}

// ---------------------------------------------------------------- mezők felismerése

var digitsRe = regexp.MustCompile(`\d`)

func classify(label string) string {
	n := norm(label)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(n, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("mail"):
		return "email"
	case has("telefon", "phone", "mobil"):
		return "phone"
	case has("atvetel", "pickup", "delivery_mode", "szallitas modja"):
		return "mode"
	case has("telepules", "varos", "city"):
		return "city"
	case has("utca", "hazszam", "street", "address", "cim"):
		return "street"
	case has("rendel", "etel", "order", "items"):
		return "items"
	case has("megjegyz", "note", "comment", "message"):
		return "note"
	case n == "nev" || strings.HasPrefix(n, "nev ") || strings.HasPrefix(n, "nev(") || has("name", "first_name", "last_name"):
		return "name"
	}
	return ""
}

func normMode(v string) string {
	if strings.Contains(norm(v), "kisz") || strings.Contains(norm(v), "deliver") || strings.Contains(norm(v), "haz") {
		return modeDelivery
	}
	return modePickup
}

// mapFields: kulcs->érték párokból rendelés. labels: kulcs->címke (ha ismert).
func mapFields(kv map[string]string, labels map[string]string) *Order {
	o := &Order{Extra: map[string]string{}}
	var names []string
	var rest [][2]string
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(kv[k])
		if v == "" {
			continue
		}
		label := k
		if l, ok := labels[k]; ok && l != "" {
			label = l
		}
		switch classify(label) {
		case "email":
			o.Email = v
		case "phone":
			o.Phone = v
		case "mode":
			o.Mode = normMode(v)
		case "city":
			o.City = v
		case "street":
			o.Street = v
		case "items":
			o.Items = v
		case "note":
			o.Note = v
		case "name":
			names = append(names, v)
		default:
			rest = append(rest, [2]string{label, v})
		}
	}
	o.Name = strings.Join(names, " ")
	// érték alapú tippelés a fel nem ismert mezőkre
	var leftovers [][2]string
	for _, p := range rest {
		v := p[1]
		switch {
		case o.Email == "" && strings.Contains(v, "@") && !strings.ContainsAny(v, " \n"):
			o.Email = v
		case o.Mode == "" && (norm(v) == "kiszallitas" || norm(v) == "szemelyes atvetel"):
			o.Mode = normMode(v)
		case o.Phone == "" && len(digitsRe.FindAllString(v, -1)) >= 8 && len(v) <= 20 && !strings.ContainsAny(v, "abcdefghijklmnopqrstuvwxyzáéíóöőúüű"):
			o.Phone = v
		default:
			leftovers = append(leftovers, p)
		}
	}
	// a leghosszabb maradék szöveg legyen a rendelés, ha még nincs
	if o.Items == "" && len(leftovers) > 0 {
		bi := 0
		for i, p := range leftovers {
			if len(p[1]) > len(leftovers[bi][1]) {
				bi = i
			}
		}
		o.Items = leftovers[bi][1]
		leftovers = append(leftovers[:bi], leftovers[bi+1:]...)
	}
	for _, p := range leftovers {
		o.Extra[p[0]] = p[1]
	}
	if len(o.Extra) == 0 {
		o.Extra = nil
	}
	if o.Mode == "" {
		if o.City != "" || o.Street != "" {
			o.Mode = modeDelivery
		} else {
			o.Mode = modePickup
		}
	}
	return o
}

func valToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "igen"
		}
		return "nem"
	case []any:
		var parts []string
		for _, x := range t {
			if s := valToString(x); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, k := range []string{"stringValue", "numberValue", "boolValue"} {
			if x, ok := t[k]; ok {
				return valToString(x)
			}
		}
		if lv, ok := t["listValue"].(map[string]any); ok {
			return valToString(lv["values"])
		}
		// pl. cím objektum
		if f, ok := t["formatted"].(string); ok {
			return f
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			if s := valToString(t[k]); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func flatten(prefix string, v any, out map[string]string) {
	if m, ok := v.(map[string]any); ok {
		simple := false
		for _, k := range []string{"stringValue", "numberValue", "boolValue", "listValue"} {
			if _, has := m[k]; has {
				simple = true
			}
		}
		if !simple {
			for k, x := range m {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}
				flatten(key, x, out)
			}
			return
		}
	}
	if arr, ok := v.([]any); ok {
		allSimple := true
		for _, x := range arr {
			switch x.(type) {
			case map[string]any, []any:
				allSimple = false
			}
		}
		if !allSimple {
			for i, x := range arr {
				flatten(fmt.Sprintf("%s.%d", prefix, i), x, out)
			}
			return
		}
	}
	if s := valToString(v); s != "" {
		out[prefix] = s
	}
}

// ---------------------------------------------------------------- e-mail

type MailDraft struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (a *App) addressLine(o *Order) string {
	if o.Mode == modeDelivery {
		s := "Kiszállítás: " + strings.TrimSpace(o.City+", "+o.Street)
		s = strings.TrimSuffix(strings.TrimSpace(s), ",")
		if o.DeliveryFee > 0 {
			s += fmt.Sprintf(" (szállítási díj: %s Ft)", formatHUF(o.DeliveryFee))
		}
		return s
	}
	return "Átvétel: személyesen az étteremben"
}

func formatHUF(n int) string {
	s := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}

func (a *App) draft(o *Order) *MailDraft {
	if o.Email == "" || (o.Status != StPreparing && o.Status != StRejected) {
		return nil
	}
	name := o.Name
	if name == "" {
		name = "Vendégünk"
	}
	var subj, body string
	if o.Status == StPreparing {
		subj = fmt.Sprintf("%s – a rendelését készítjük (#%d)", a.conf().EtteremNeve, o.ID)
		body = fmt.Sprintf("Kedves %s!\n\nRendelését megkaptuk, elkezdtük készíteni.\n", name)
		if o.ETA > 0 {
			body += fmt.Sprintf("Várható elkészülési idő: kb. %d perc.\n", o.ETA)
		}
		body += "\n" + a.addressLine(o) + "\n\nAz Ön rendelése:\n" + o.Items + "\n\nKöszönjük, hogy minket választott!\n"
	} else {
		subj = fmt.Sprintf("%s – a rendelését sajnos nem tudjuk teljesíteni (#%d)", a.conf().EtteremNeve, o.ID)
		body = fmt.Sprintf("Kedves %s!\n\nSajnáljuk, de a rendelését most nem tudjuk teljesíteni.\n", name)
		if o.Reason != "" {
			body += "Ok: " + o.Reason + "\n"
		}
		body += "\nA rendelése:\n" + o.Items + "\n\nElnézést kérünk, és szeretettel várjuk legközelebb!\n"
	}
	body += "\n" + a.conf().EtteremNeve
	if a.conf().EtteremTelefon != "" {
		body += " · " + a.conf().EtteremTelefon
	}
	body += "\n"
	return &MailDraft{To: o.Email, Subject: subj, Body: body}
}

func (a *App) smtpEnabled() bool {
	return a.conf().SMTP.Szerver != "" && (a.conf().SMTP.FeladoEmail != "" || a.conf().SMTP.Felhasznalo != "")
}

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

func sendMail(c SMTPConf, to, subject, body string) error {
	from := c.FeladoEmail
	if from == "" {
		from = c.Felhasznalo
	}
	if c.Port == 0 {
		c.Port = 587
	}
	fromHdr := from
	if c.FeladoNev != "" {
		fromHdr = mime.QEncoding.Encode("utf-8", c.FeladoNev) + " <" + from + ">"
	}
	msg := "From: " + fromHdr + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <" + randStr(20, "abcdef0123456789") + "@vadvirag.local>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		wrap76(base64.StdEncoding.EncodeToString([]byte(body))) + "\r\n"

	addr := net.JoinHostPort(c.Szerver, strconv.Itoa(c.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if c.Port == 465 {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: c.Szerver})
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(45 * time.Second))
	cl, err := smtp.NewClient(conn, c.Szerver)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if c.Port != 465 {
		if ok, _ := cl.Extension("STARTTLS"); ok {
			if err := cl.StartTLS(&tls.Config{ServerName: c.Szerver}); err != nil {
				return err
			}
		}
	}
	if c.Felhasznalo != "" {
		if err := cl.Auth(loginAuth(c.Felhasznalo, c.Jelszo, c.Szerver)); err != nil {
			return err
		}
	}
	if err := cl.Mail(from); err != nil {
		return err
	}
	if err := cl.Rcpt(to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// PLAIN hitelesítés, localhoston titkosítás nélkül is (teszthez), egyébként csak TLS-en.
type plainAuth struct{ user, pass, host string }

func loginAuth(user, pass, host string) smtp.Auth { return &plainAuth{user, pass, host} }
func (p *plainAuth) Start(si *smtp.ServerInfo) (string, []byte, error) {
	if !si.TLS && !(p.host == "localhost" || p.host == "127.0.0.1") {
		return "", nil, errors.New("a kapcsolat nem titkosított, a jelszót nem küldöm el")
	}
	return "PLAIN", []byte("\x00" + p.user + "\x00" + p.pass), nil
}
func (p *plainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("váratlan szerver válasz")
	}
	return nil, nil
}

func (a *App) emailAsync(id int) {
	a.st.mu.Lock()
	o := a.find(id)
	if o == nil {
		a.st.mu.Unlock()
		return
	}
	d := a.draft(o)
	switch {
	case d == nil:
		o.EmailState = "none"
		o.EmailErr = "nincs e-mail cím"
		a.st.saveLocked()
		a.st.mu.Unlock()
		return
	case !a.smtpEnabled():
		o.EmailState = "none"
		o.EmailErr = "az e-mail küldés nincs beállítva"
		a.st.saveLocked()
		a.st.mu.Unlock()
		return
	}
	o.EmailState = "sending"
	o.EmailErr = ""
	a.st.saveLocked()
	a.st.mu.Unlock()

	err := sendMail(a.conf().SMTP, d.To, d.Subject, d.Body)

	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	if o := a.find(id); o != nil {
		o.EmailAt = time.Now()
		if err != nil {
			o.EmailState = "failed"
			o.EmailErr = err.Error()
			log.Printf("E-mail hiba (#%d): %v", id, err)
		} else {
			o.EmailState = "sent"
			o.EmailErr = ""
			log.Printf("E-mail elküldve (#%d → %s)", id, d.To)
		}
		a.st.saveLocked()
	}
}

func (a *App) find(id int) *Order {
	for _, o := range a.st.Orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}

// ---------------------------------------------------------------- Wix lekérdezés

func (a *App) wixDo(method, path string, body any) (map[string]any, error) {
	c := a.conf().Wix
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, strings.TrimRight(c.APICim, "/")+path, rd)
	req.Header.Set("Authorization", c.APIKulcs)
	req.Header.Set("wix-site-id", c.SiteID)
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("Wix hiba %d: %s", resp.StatusCode, msg)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectLabels(v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if target, ok := t["target"].(string); ok && target != "" {
			for _, k := range []string{"label", "title", "name", "displayName"} {
				if s, ok := t[k].(string); ok && s != "" {
					out[target] = s
					break
				}
			}
			if _, done := out[target]; !done {
				for _, sub := range t {
					if m, ok := sub.(map[string]any); ok {
						for _, k := range []string{"label", "title", "name", "displayName"} {
							if s, ok := m[k].(string); ok && s != "" {
								out[target] = s
								break
							}
						}
					}
					if _, done := out[target]; done {
						break
					}
				}
			}
		}
		for _, x := range t {
			collectLabels(x, out)
		}
	case []any:
		for _, x := range t {
			collectLabels(x, out)
		}
	}
}

func (a *App) formLabels(formID string) map[string]string {
	a.wixMu.Lock()
	if l, ok := a.labels[formID]; ok {
		a.wixMu.Unlock()
		return l
	}
	if t, ok := a.labelTry[formID]; ok && time.Since(t) < 5*time.Minute {
		a.wixMu.Unlock()
		return nil
	}
	a.labelTry[formID] = time.Now()
	a.wixMu.Unlock()

	res, err := a.wixDo("GET", "/form-schema-service/v4/forms/"+formID, nil)
	if err != nil {
		log.Printf("Wix űrlap-címkék nem érhetők el (%v) – találgatással dolgozom", err)
		return nil
	}
	l := map[string]string{}
	collectLabels(res, l)
	a.wixMu.Lock()
	a.labels[formID] = l
	a.wixMu.Unlock()
	return l
}

func (a *App) pollWix() error {
	c := a.conf().Wix
	filter := map[string]any{"namespace": "wix.form_app.form"}
	if c.FormID != "" {
		filter["formId"] = c.FormID
	}
	body := map[string]any{"query": map[string]any{
		"filter":       filter,
		"sort":         []map[string]string{{"fieldName": "createdDate", "order": "DESC"}},
		"cursorPaging": map[string]any{"limit": 100},
	}}
	res, err := a.wixDo("POST", "/form-submission-service/v4/submissions/namespace/query", body)
	if err != nil {
		return err
	}
	subs, _ := res["submissions"].([]any)
	for i := len(subs) - 1; i >= 0; i-- { // régebbi előbb
		m, _ := subs[i].(map[string]any)
		if m == nil {
			continue
		}
		if st, _ := m["status"].(string); st == "PENDING" || st == "PAYMENT_WAITING" || st == "PAYMENT_CANCELED" {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			id, _ = m["_id"].(string)
		}
		formID, _ := m["formId"].(string)
		vals := map[string]string{}
		if sv, ok := m["submissions"].(map[string]any); ok {
			for k, v := range sv {
				flatten(k, v, vals)
			}
		}
		if len(vals) == 0 {
			continue
		}
		o := mapFields(vals, a.formLabels(formID))
		o.Source = "wix"
		o.ExtID = "wix:" + id
		if cd, _ := m["createdDate"].(string); cd != "" {
			if t, err := time.Parse(time.RFC3339, cd); err == nil {
				o.Created = t
			}
		}
		a.ingest(o)
	}
	return nil
}

func (a *App) wixLoop() {
	if a.conf().Wix.APIKulcs == "" {
		log.Printf("Wix lekérdezés: nincs API kulcs megadva (beallitasok.json) – a rendelések csak webhookon / kézzel érkeznek.")
		return
	}
	for {
		err := a.pollWix()
		a.wixMu.Lock()
		if err != nil {
			a.wixErr = err.Error()
			log.Printf("Wix lekérdezés hiba: %v", err)
		} else {
			a.wixErr = ""
			a.wixLastOK = time.Now()
		}
		a.wixMu.Unlock()
		time.Sleep(time.Duration(a.conf().Wix.LekerdezesSec) * time.Second)
	}
}

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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(data)
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
	mux.HandleFunc("POST /api/orders/{id}/done", a.requireAuth(a.handleSimple(StDone)))
	mux.HandleFunc("POST /api/orders/{id}/reopen", a.requireAuth(a.handleSimple(StNew)))
	mux.HandleFunc("POST /api/orders/{id}/resend", a.requireAuth(a.handleResend))
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
	mux.HandleFunc("GET /api/raffle", a.requireAuth(a.handleRaffleList))
	mux.HandleFunc("POST /api/raffle", a.requireAuth(a.handleRaffleDraw))
	mux.HandleFunc("POST /api/raffle/preview", a.requireAuth(a.handleRafflePreview))
	mux.HandleFunc("GET /api/backup", a.requireAuth(a.handleBackup))
	mux.HandleFunc("POST /api/import", a.requireAuth(a.handleImport))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok")) })
	mux.HandleFunc("GET /rendeles", a.servePublicPage)
	mux.HandleFunc("GET /api/public/menu", a.handlePublicMenu)
	mux.HandleFunc("POST /api/public/order", a.handlePublicOrder)
	mux.HandleFunc("GET /api/public/order/{token}", a.handlePublicStatus)
	mux.HandleFunc("POST /api/public/message", a.handlePublicMessage)
	mux.HandleFunc("GET /api/messages", a.requireAuth(a.handleMessages))
	mux.HandleFunc("POST /api/messages/{id}/done", a.requireAuth(a.handleMessageDone))
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

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func fatal(container bool, a ...any) {
	fmt.Println(append([]any{"HIBA:"}, a...)...)
	if !container {
		fmt.Println("Nyomj Entert a kilépéshez.")
		fmt.Scanln()
	}
	os.Exit(1)
}

func main() {
	dirFlag := flag.String("dir", "", "adatmappa (alapból a program mappája, vagy a DATA_DIR környezeti változó)")
	noBrowser := flag.Bool("nobrowser", false, "ne nyissa meg a böngészőt")
	flag.Parse()

	// Dockerben (Umbrel) a DATA_DIR mondja meg, hova mentsünk.
	container := os.Getenv("DATA_DIR") != ""
	dir := *dirFlag
	if dir == "" {
		dir = os.Getenv("DATA_DIR")
	}
	if dir == "" {
		if exe, err := os.Executable(); err == nil {
			dir = filepath.Dir(exe)
		} else {
			dir = "."
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(container, "nem hozható létre az adatmappa:", err)
	}
	cfgPath := filepath.Join(dir, "beallitasok.json")
	_, existed := os.Stat(cfgPath)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fatal(container, err)
	}
	if existed != nil && container {
		// Umbrelen nincs konzolablak: a jelszót az első megnyitáskor a böngészőben kell megadni.
		cfg.Jelszo = ""
		if err := saveConfig(cfgPath, cfg); err != nil {
			fatal(container, err)
		}
	}
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
		cfg.Port = p
	}
	st, err := openStore(filepath.Join(dir, "rendelesek.json"))
	if err != nil {
		fatal(container, err)
	}
	if ensureVapid(&cfg) {
		if err := saveConfig(cfgPath, cfg); err != nil {
			log.Printf("Nem sikerült menteni a beállításokat: %v", err)
		}
	}
	if p, err := newPusher(cfg, filepath.Join(dir, "ertesitesek.json")); err != nil {
		log.Printf("Értesítések nem elérhetők: %v", err)
	} else {
		push = p
	}
	app := newApp(cfg, st)
	app.cfgPath, app.dataDir, app.container = cfgPath, dir, container
	go app.wixLoop()

	fmt.Println("==================================================")
	fmt.Println("  " + cfg.EtteremNeve + " – rendeléskezelő " + appVersion)
	fmt.Println("==================================================")
	local := fmt.Sprintf("http://localhost:%d", cfg.Port)
	if container {
		fmt.Println("  Adatmappa:", dir, " · belső port:", cfg.Port)
		if app.needsSetup() {
			fmt.Println("  Első indítás: nyisd meg az alkalmazást az Umbrel kezdőlapjáról, és add meg a jelszót.")
		}
	} else {
		fmt.Println("  Ezen a gépen:  ", local)
		for _, u := range lanURLs(cfg.Port) {
			fmt.Println("  Telefonról:    ", u, " (ugyanazon a Wi-Fi-n)")
		}
		if app.needsSetup() {
			fmt.Println("  Első indítás: nyisd meg a fenti címet, és add meg a jelszót.")
		} else {
			fmt.Println("  Belépési jelszó:", cfg.Jelszo, "  (módosítható a program Beállítások menüjében)")
		}
		if existed != nil {
			fmt.Println("  (Első indítás – létrejött a beallitasok.json)")
		}
		fmt.Println("  Ezt az ablakot hagyd nyitva, amíg fogadod a rendeléseket!")
	}
	fmt.Println("==================================================")

	app.startDash()
	srv := &http.Server{Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second}
	// Újraindításkor a régi példány még pár pillanatig foglalhatja a portot.
	var ln net.Listener
	for i := 0; ; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err == nil {
			break
		}
		if i >= 20 {
			fatal(container, "a szerver nem indul:", err, "(lehet, hogy a program már fut, vagy a", cfg.Port, "port foglalt)")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !*noBrowser && !container {
		go func() { time.Sleep(700 * time.Millisecond); openBrowser(local) }()
	}
	if err := srv.Serve(ln); err != nil {
		fatal(container, "a szerver leállt:", err)
	}
}

// wixSubmissions: a Wix "Send HTTP request" tartalmából (data.submissions[] = {label,value})
// címke->érték párokat készít. Ha nincs ilyen tömb, nil-t ad vissza.
func wixSubmissions(parsed any) (map[string]string, string) {
	root, _ := parsed.(map[string]any)
	if root == nil {
		return nil, ""
	}
	d, _ := root["data"].(map[string]any)
	if d == nil {
		d = root
	}
	arr, _ := d["submissions"].([]any)
	if len(arr) == 0 {
		return nil, ""
	}
	out := map[string]string{}
	for i, x := range arr {
		m, _ := x.(map[string]any)
		if m == nil {
			continue
		}
		label, _ := m["label"].(string)
		if label == "" {
			label = fmt.Sprintf("mező %d", i+1)
		}
		v := valToString(m["value"])
		if v == "" {
			continue
		}
		if old, ok := out[label]; ok {
			v = old + ", " + v
		}
		out[label] = v
	}
	id, _ := d["submissionId"].(string)
	return out, id
}
