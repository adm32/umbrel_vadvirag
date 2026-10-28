package main

// Előrendelés alkalmi ajánlatra (most: Márton-nap).
//
// A vendég a weboldalon kiválasztja az ételeket, és megmondja, hogyan kéri: helyben (asztalfoglalás),
// elvitelre vagy kiszállítással – egy későbbi napra, a serve_from naptól. Ez nem kerül a mai rendelések
// közé (ott a konyha azonnal nekiállna), hanem az „Előrendelések” nézetbe, napok szerint csoportosítva.
// Amíg a személyzet nem kapcsolja „Elfogyott”-ra, a weboldalon látszik; utána azonnal eltűnik.
//
// Árak: az ételek ára az elorendeles.json szerint; elvitelnél és kiszállításnál adagonként egy
// fedeles habtálca (packTray), kiszállításnál a település szokásos díja.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

//go:embed elorendeles.json
var preOfferJSON []byte

type preDish struct {
	Name map[string]string `json:"name"`
	Desc map[string]string `json:"desc"`
}

type preOffer struct {
	ID           string            `json:"id"`
	Title        map[string]string `json:"title"`
	ServeFrom    string            `json:"serve_from"`
	MainPrice    int               `json:"main_price"`
	DessertPrice int               `json:"dessert_price"`
	Mains        []preDish         `json:"mains"`
	Desserts     []preDish         `json:"desserts"`
}

var preOff *preOffer

func init() {
	var o preOffer
	if err := json.Unmarshal(preOfferJSON, &o); err != nil {
		panic("hibás elorendeles.json: " + err.Error())
	}
	if o.ID != "" {
		preOff = &o
	}
}

// preItem: az ajánlat egy tétele (m0…: főétel, d0…: desszert).
type preItem struct {
	Key   string
	Name  string
	Price int
}

func (o *preOffer) items() []preItem {
	var out []preItem
	for i, d := range o.Mains {
		out = append(out, preItem{fmt.Sprintf("m%d", i), d.Name["hu"], o.MainPrice})
	}
	for i, d := range o.Desserts {
		out = append(out, preItem{fmt.Sprintf("d%d", i), d.Name["hu"], o.DessertPrice})
	}
	return out
}

const (
	preDineIn   = "helyben"
	preTakeaway = "elvitel"
	preDelivery = "kiszallitas"
)

var preModeName = map[string]string{preDineIn: "Helyben (asztalfoglalás)", preTakeaway: "Elvitel", preDelivery: "Kiszállítás"}

type PreLine struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Qty   int    `json:"qty"`
	Price int    `json:"price"` // egységár
}

type Preorder struct {
	ID       int       `json:"id"`
	At       time.Time `json:"at"`
	Offer    string    `json:"offer"`
	Mode     string    `json:"mode"` // helyben | elvitel | kiszallitas
	Date     string    `json:"date"` // mikorra (YYYY-MM-DD)
	Time     string    `json:"time"` // HH:MM
	Guests   int       `json:"guests,omitempty"`
	Name     string    `json:"name"`
	Phone    string    `json:"phone"`
	Email    string    `json:"email,omitempty"`
	City     string    `json:"city,omitempty"`
	Street   string    `json:"street,omitempty"`
	DriveMin int       `json:"drive_min,omitempty"`
	Note     string    `json:"note,omitempty"`
	Lines    []PreLine `json:"lines"`
	Food     int       `json:"food"`
	Pack     int       `json:"pack,omitempty"`
	Fee      int       `json:"fee,omitempty"`
	Total    int       `json:"total"`
	Payment  string    `json:"payment,omitempty"`
	Lang     string    `json:"lang,omitempty"`
	Status   string    `json:"status"` // uj | visszaigazolva | kesz | lemondva
	StatusAt time.Time `json:"status_at,omitempty"`
}

var preStatuses = map[string]bool{"uj": true, "visszaigazolva": true, "kesz": true, "lemondva": true}

var (
	preMu     sync.Mutex
	preTimes  []time.Time
	preRecent = map[string]time.Time{}
	dateRe    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRe    = regexp.MustCompile(`^\d{2}:\d{2}$`)
	digitRe   = regexp.MustCompile(`\d`)
)

func (a *App) prePath() string { return filepath.Join(a.dataDir, "elorendelesek.json") }

func (a *App) loadPreorders() []Preorder {
	var ps []Preorder
	if b, err := os.ReadFile(a.prePath()); err == nil {
		json.Unmarshal(b, &ps)
	}
	return ps
}

func (a *App) savePreorders(ps []Preorder) error {
	b, _ := json.MarshalIndent(ps, "", " ")
	tmp := a.prePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.prePath())
}

func (a *App) preSoldOut() bool {
	return preOff == nil || a.conf().Elfogyott[preOff.ID]
}

// preCounts: visszaigazolásra váró (új) és a mai (nem lemondott, még nem kész) előrendelések száma.
func (a *App) preCounts() (fresh, today int) {
	if preOff == nil {
		return 0, 0
	}
	t := todayBudapest(time.Now())
	preMu.Lock()
	defer preMu.Unlock()
	for _, p := range a.loadPreorders() {
		if p.Status == "uj" {
			fresh++
		}
		if p.Date == t && (p.Status == "uj" || p.Status == "visszaigazolva") {
			today++
		}
	}
	return
}

func todayBudapest(now time.Time) string { return budapestNow(now).Format("2006-01-02") }

// GET /api/public/preorder – a weboldal ebből rajzolja ki az ajánlatot.
func (a *App) handlePublicPreorder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	if a.preSoldOut() {
		writeJSON(w, 200, map[string]any{"open": false})
		return
	}
	c := a.conf()
	fees := make([]feeView, 0, len(c.Dijak))
	for n, f := range c.Dijak {
		fees = append(fees, feeView{n, f})
	}
	sort.Slice(fees, func(i, j int) bool { return strings.Compare(norm(fees[i].Name), norm(fees[j].Name)) < 0 })
	writeJSON(w, 200, map[string]any{
		"open": true, "offer": preOff, "today": todayBudapest(time.Now()),
		"pack": packTray, "fees": fees, "hours": []string{"11:00", "21:30"},
	})
}

type preorderReq struct {
	Items   map[string]int `json:"items"`
	Mode    string         `json:"mode"`
	Date    string         `json:"date"`
	Time    string         `json:"time"`
	Guests  int            `json:"guests"`
	Name    string         `json:"name"`
	Phone   string         `json:"phone"`
	Email   string         `json:"email"`
	City    string         `json:"city"`
	Street  string         `json:"street"`
	Note    string         `json:"note"`
	Payment string         `json:"payment"`
	Lang    string         `json:"lang"`
	Website string         `json:"website"` // csapda a robotoknak
}

// buildPreorder: ellenőrzés és árazás. Hibánál a vendégnek szóló üzenettel tér vissza.
func (a *App) buildPreorder(in preorderReq, now time.Time) (*Preorder, string) {
	if preOff == nil || a.preSoldOut() {
		return nil, "Ez az ajánlat sajnos elfogyott. Hívj minket, hátha még tudunk segíteni!"
	}
	in.Name, in.Phone, in.Email = clip(strings.TrimSpace(in.Name), 80), clip(strings.TrimSpace(in.Phone), 30), clip(strings.TrimSpace(in.Email), 120)
	in.City, in.Street, in.Note = clip(strings.TrimSpace(in.City), 80), clip(strings.TrimSpace(in.Street), 160), clip(strings.TrimSpace(in.Note), 600)
	p := &Preorder{Offer: preOff.ID, Mode: in.Mode, Date: in.Date, Time: in.Time, Name: in.Name, Phone: in.Phone, Email: in.Email,
		Note: in.Note, Lang: clip(in.Lang, 5), Status: "uj"}
	n := 0
	for _, it := range preOff.items() {
		q := in.Items[it.Key]
		if q < 0 || q > 50 {
			return nil, "Egy ételből legfeljebb 50 adagot lehet előrendelni – nagyobb mennyiségért hívj minket!"
		}
		if q > 0 {
			p.Lines = append(p.Lines, PreLine{it.Key, it.Name, q, it.Price})
			p.Food += q * it.Price
			n += q
		}
	}
	if n == 0 {
		return nil, "Válassz legalább egy ételt."
	}
	if _, ok := preModeName[in.Mode]; !ok {
		return nil, "Válaszd ki, hogyan kéred: helyben, elvitelre vagy kiszállítással."
	}
	today := todayBudapest(now)
	if !dateRe.MatchString(in.Date) {
		return nil, "Válaszd ki a napot."
	}
	if in.Date < preOff.ServeFrom {
		return nil, "Ezt az ajánlatot " + huDate(preOff.ServeFrom) + "-től lehet kérni – válassz attól kezdve egy napot."
	}
	if in.Date < today {
		return nil, "Múltbeli napot nem lehet választani."
	}
	if d, err := time.Parse("2006-01-02", in.Date); err != nil || d.Sub(now) > 120*24*time.Hour {
		return nil, "Válassz egy közelebbi napot."
	}
	if !timeRe.MatchString(in.Time) || in.Time < "11:00" || in.Time > "21:30" {
		return nil, "11:00 és 21:30 között válassz időpontot."
	}
	if in.Date == today {
		if bn := budapestNow(now); in.Time < bn.Add(30*time.Minute).Format("15:04") {
			return nil, "Mára legalább fél órával későbbi időpontot válassz."
		}
	}
	if in.Name == "" {
		return nil, "Kérjük, add meg a neved."
	}
	if len(digitRe.FindAllString(in.Phone, -1)) < 6 {
		return nil, "Add meg a telefonszámod, hogy visszaigazolhassuk az előrendelést."
	}
	if in.Email != "" && !emailRe.MatchString(in.Email) {
		return nil, "Ez az e-mail cím nem tűnik helyesnek."
	}
	switch in.Mode {
	case preDineIn:
		if in.Guests < 1 || in.Guests > 200 {
			return nil, "Add meg, hányan lesztek (1–200 fő)."
		}
		p.Guests = in.Guests
	case preTakeaway, preDelivery:
		p.Pack = n * packTray
		if in.Payment == "Kártya" || in.Payment == "Készpénz" {
			p.Payment = in.Payment
		}
		if in.Mode == preDelivery {
			f := a.match(in.City)
			if in.City == "" || f == nil {
				return nil, "Válaszd ki a települést a listából. Ha a tiéd nincs köztük, válaszd az elvitelt, vagy hívj minket."
			}
			if in.Street == "" {
				return nil, "Add meg a címet (utca, házszám)."
			}
			p.City, p.Street, p.Fee, p.DriveMin = f.name, in.Street, f.fee, f.mins
		}
	}
	p.Total = p.Food + p.Pack + p.Fee
	return p, ""
}

var huMonths = []string{"", "január", "február", "március", "április", "május", "június", "július", "augusztus", "szeptember", "október", "november", "december"}

// huDate: „november 10.” – a -tól/-től toldalékot a hívó teszi hozzá.
func huDate(iso string) string {
	t, err := time.Parse("2006-01-02", iso)
	if err != nil {
		return iso
	}
	return fmt.Sprintf("%s %d", huMonths[t.Month()], t.Day())
}

// POST /api/public/preorder
func (a *App) handlePublicPreorderPost(w http.ResponseWriter, r *http.Request) {
	var in preorderReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 16<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "Hibás kérés.")
		return
	}
	if in.Website != "" { // robot
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	now := time.Now()
	p, msg := a.buildPreorder(in, now)
	if msg != "" {
		publicErr(w, 400, msg)
		return
	}
	key := norm(p.Phone + "|" + p.Date + "|" + p.Time + "|" + fmt.Sprint(p.Lines))
	preMu.Lock()
	keep := preTimes[:0]
	for _, t := range preTimes {
		if now.Sub(t) < 10*time.Minute {
			keep = append(keep, t)
		}
	}
	preTimes = keep
	if t, ok := preRecent[key]; ok && now.Sub(t) < 5*time.Minute { // dupla kattintás: már megvan
		preMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if len(preTimes) >= 30 {
		preMu.Unlock()
		publicErr(w, 429, "Most nem tudjuk fogadni – kérjük, hívj minket telefonon!")
		return
	}
	preTimes = append(preTimes, now)
	preRecent[key] = now
	for k, t := range preRecent {
		if now.Sub(t) > 10*time.Minute {
			delete(preRecent, k)
		}
	}
	ps := a.loadPreorders()
	p.ID = 1
	if len(ps) > 0 {
		p.ID = ps[len(ps)-1].ID + 1
	}
	p.At, p.StatusAt = now, now
	ps = append(ps, *p)
	err := a.savePreorders(ps)
	preMu.Unlock()
	if err != nil {
		publicErr(w, 500, "Nem sikerült elmenteni. Kérjük, hívj minket telefonon!")
		return
	}
	log.Printf("Előrendelés #%d (%s, %s %s, %s Ft)", p.ID, preModeName[p.Mode], p.Date, p.Time, formatHUF(p.Total))
	go a.notifyPreorder(*p)
	writeJSON(w, 200, map[string]any{"ok": true, "id": p.ID, "total": p.Total})
}

func (a *App) notifyPreorder(p Preorder) {
	if push == nil || push.count() == 0 {
		return
	}
	var ls []string
	for _, l := range p.Lines {
		ls = append(ls, fmt.Sprintf("%d× %s", l.Qty, l.Name))
	}
	where := preModeName[p.Mode]
	if p.Mode == preDelivery {
		where += " – " + p.City
	} else if p.Mode == preDineIn {
		where = fmt.Sprintf("Helyben, %d fő", p.Guests)
	}
	body := fmt.Sprintf("%s %s · %s\n%s", huDate(p.Date)+".", p.Time, where, strings.Join(ls, ", "))
	if len([]rune(body)) > 160 {
		body = string([]rune(body)[:160]) + "…"
	}
	push.broadcast(map[string]any{"title": "Előrendelés – " + p.Name, "body": body, "tag": fmt.Sprintf("pre-%d", p.ID), "url": "/#elorendeles"})
}

// GET /api/preorders – a személyzetnek: az ajánlat és az összes előrendelés.
func (a *App) handlePreorders(w http.ResponseWriter, r *http.Request) {
	preMu.Lock()
	ps := a.loadPreorders()
	preMu.Unlock()
	var offer any
	if preOff != nil {
		offer = map[string]any{"id": preOff.ID, "title": preOff.Title["hu"], "serve_from": preOff.ServeFrom, "sold_out": a.preSoldOut()}
	}
	writeJSON(w, 200, map[string]any{"offer": offer, "items": ps, "today": todayBudapest(time.Now())})
}

// POST /api/preorders/{id}/status {"status":"visszaigazolva"}
func (a *App) handlePreorderStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var in struct{ Status string }
	json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&in)
	if !preStatuses[in.Status] {
		writeJSON(w, 400, map[string]any{"error": "ismeretlen állapot"})
		return
	}
	preMu.Lock()
	defer preMu.Unlock()
	ps := a.loadPreorders()
	for i := range ps {
		if ps[i].ID == id {
			ps[i].Status, ps[i].StatusAt = in.Status, time.Now()
			if err := a.savePreorders(ps); err != nil {
				writeJSON(w, 500, map[string]any{"error": "mentési hiba"})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
	}
	writeJSON(w, 404, map[string]any{"error": "nincs ilyen előrendelés"})
}

// POST /api/preorders/soldout {"sold_out":true} – elfogyott: a weboldalról azonnal eltűnik.
func (a *App) handlePreorderSoldOut(w http.ResponseWriter, r *http.Request) {
	var in struct {
		SoldOut bool `json:"sold_out"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&in)
	if preOff == nil {
		writeJSON(w, 400, map[string]any{"error": "nincs előrendelhető ajánlat"})
		return
	}
	err := a.updateConf(func(c *Config) {
		m := map[string]bool{}
		for k, v := range c.Elfogyott {
			m[k] = v
		}
		if in.SoldOut {
			m[preOff.ID] = true
		} else {
			delete(m, preOff.ID)
		}
		c.Elfogyott = m
	})
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	log.Printf("Előrendelés (%s): elfogyott=%v", preOff.ID, in.SoldOut)
	writeJSON(w, 200, map[string]any{"ok": true, "sold_out": in.SoldOut})
}
