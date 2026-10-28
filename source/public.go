package main

// Nyilvános rendelőoldal (/rendeles): a vendég kattintással állítja össze a rendelést,
// a szerver a saját étlapja alapján számolja az árat, majd a vendég élőben látja az állapotát.

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

//go:embed menu.json
var menuJSON []byte

type MenuVariant struct {
	Label string `json:"label"`
	Price int    `json:"price"`
	Pack  int    `json:"pack"` // csomagolás díja darabonként (a szerver tölti ki)
}

type MenuItem struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Desc     string        `json:"desc,omitempty"`
	Alg      string        `json:"alg,omitempty"`
	Price    int           `json:"price,omitempty"`
	Variants []MenuVariant `json:"variants,omitempty"`
	Pack     int           `json:"pack"`           // csomagolás díja darabonként (a szerver tölti ki)
	Role     string        `json:"role,omitempty"` // "main": köretre szoruló főétel, "side": köret (közös doboz)
}

type MenuSection struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Short string     `json:"short"`
	Off   bool       `json:"off,omitempty"` // elfogyott / nincs szezonja: nem látszik és nem rendelhető, de az ára megmarad a régi rendelésekhez
	Items []MenuItem `json:"items"`
}

var (
	menuSections []MenuSection // a teljes étlap (az árbecslés ezt használja)
	publicMenu   []MenuSection // amit a vendég lát: a kikapcsolt részek nélkül
	menuIdx      = map[string]*MenuItem{}
	menuOff      = map[string]bool{} // a kikapcsolt részek ételei
)

func init() {
	var m struct {
		Sections []MenuSection `json:"sections"`
	}
	if err := json.Unmarshal(menuJSON, &m); err != nil {
		panic("hibás menu.json: " + err.Error())
	}
	menuSections = m.Sections
	for si := range menuSections {
		for ii := range menuSections[si].Items {
			it := &menuSections[si].Items[ii]
			it.Pack = packFor(menuSections[si].ID, it, "")
			it.Role = roleFor(menuSections[si].ID, it)
			for vi := range it.Variants {
				it.Variants[vi].Pack = packFor(menuSections[si].ID, it, it.Variants[vi].Label)
			}
			menuIdx[it.ID] = it
			if menuSections[si].Off {
				menuOff[it.ID] = true
			}
		}
		if !menuSections[si].Off {
			publicMenu = append(publicMenu, menuSections[si])
		}
	}
}

func (a *App) servePublicPage(w http.ResponseWriter, r *http.Request) {
	data, _ := webFS.ReadFile("web/rendeles.html")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(data)
}

type feeView struct {
	Name string `json:"name"`
	Fee  int    `json:"fee"`
}

// GET /api/public/menu
func (a *App) handlePublicMenu(w http.ResponseWriter, r *http.Request) {
	c := a.conf()
	fees := make([]feeView, 0, len(c.Dijak))
	for n, f := range c.Dijak {
		fees = append(fees, feeView{n, f})
	}
	sort.Slice(fees, func(i, j int) bool {
		return strings.Compare(norm(fees[i].Name), norm(fees[j].Name)) < 0
	})
	writeJSON(w, 200, map[string]any{
		"restaurant":   c.EtteremNeve,
		"phone":        c.EtteremTelefon,
		"open":         !c.OnlineSzunetel && !a.closedToday(),
		"closed_today": a.closedToday(),
		"notice":       c.OnlineUzenet,
		"sections":     publicMenu,
		"fees":         fees,
	})
}

// ---------------------------------------------------------------- rendelés leadása

type publicLine struct {
	ID      string `json:"id"`
	Variant string `json:"variant"`
	Qty     int    `json:"qty"`
}

type publicOrderReq struct {
	Items   []publicLine `json:"items"`
	Text    string       `json:"text"` // szabadon leírt rendelés (gyors rendelés)
	Mode    string       `json:"mode"` // pickup | delivery
	City    string       `json:"city"`
	Street  string       `json:"street"`
	Name    string       `json:"name"`
	Phone   string       `json:"phone"`
	Email   string       `json:"email"`
	Note    string       `json:"note"`
	Payment string       `json:"payment"`
	Website string       `json:"website"` // csapda a robotoknak, embernek üres marad
}

var (
	emailRe  = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	rlMu     sync.Mutex
	rlTimes  []time.Time
	rlRecent = map[string]time.Time{} // telefon+tételek → mikor (dupla küldés ellen)
)

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func publicErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// quickPrefix: a szabadon leírt rendelések első sora (így a felületek felismerik).
const quickPrefix = "Szabadon leírt rendelés:"

// POST /api/public/quick – „írd le, mit kérsz”: a régi Wix-űrlaphoz hasonló, egyszerű rendelés.
// Az árat a személyzet számolja ki, és visszaigazoláskor vagy átvételkor közli.
func (a *App) handlePublicQuick(w http.ResponseWriter, r *http.Request) {
	var in publicOrderReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "Hibás kérés.")
		return
	}
	if msg := a.orderingBlocked(); msg != "" {
		publicErr(w, 503, msg)
		return
	}
	if in.Website != "" {
		writeJSON(w, 200, map[string]any{"ok": true, "id": 0, "token": "x"})
		return
	}
	in.Text = clip(in.Text, 1500)
	if len([]rune(in.Text)) < 3 {
		publicErr(w, 400, "Írd le, mit szeretnél rendelni.")
		return
	}
	if msg := dailyOfferCheck(in.Text, time.Now()); msg != "" {
		publicErr(w, 400, msg)
		return
	}
	mode, payment, ok := a.checkContact(w, &in)
	if !ok {
		return
	}
	itemsText := quickPrefix + "\n" + in.Text
	if a.rateGate(w, norm(in.Phone)+"|"+itemsText) {
		return
	}
	o := &Order{
		Source: "web", Visitor: visitorKey(r), Name: in.Name, Phone: in.Phone, Email: in.Email, Mode: mode,
		Items: itemsText, Note: in.Note, Payment: payment,
		Token: randStr(22, "abcdefghjkmnpqrstuvwxyz23456789"),
		Extra: map[string]string{"Fizetés módja": payment, "Végösszeg": "az árat egyeztetni kell (szabadon leírt rendelés)"},
	}
	if a.dailyFreeDelivery(in.Text, mode, in.City, time.Now()) {
		o.FreeDelivery = true
		o.Extra["Szállítás"] = "ingyenes – napi ajánlat előrendelése " + hhmm(dailyFreeTil) + " előtt"
	}
	if mode == modeDelivery {
		o.City, o.Street = in.City, in.Street
	}
	o.ExtID = "web:" + o.Token
	a.ingest(o)
	a.st.mu.Lock()
	if x := a.find(o.ID); x != nil && x.Total > 0 {
		x.Extra["Végösszeg"] = "kb. " + formatHUF(x.Total) + " Ft (becslés a leírás alapján – egyeztetni)"
		a.st.saveLocked()
	}
	a.st.mu.Unlock()
	log.Printf("Weboldali gyors rendelés #%d", o.ID)
	writeJSON(w, 200, map[string]any{"ok": true, "id": o.ID, "token": o.Token})
}

// checkContact: név, telefon, e-mail, cím és fizetés ellenőrzése (mindkét rendeléstípushoz).
func (a *App) checkContact(w http.ResponseWriter, in *publicOrderReq) (mode, payment string, ok bool) {
	in.Name, in.Phone, in.Email = clip(in.Name, 80), clip(in.Phone, 30), clip(in.Email, 120)
	in.City, in.Street, in.Note = clip(in.City, 80), clip(in.Street, 160), clip(in.Note, 600)
	if in.Name == "" {
		publicErr(w, 400, "Kérjük, add meg a neved.")
		return
	}
	if len(digitsRe.FindAllString(in.Phone, -1)) < 8 {
		publicErr(w, 400, "Kérjük, adj meg egy telefonszámot, amin elérünk.")
		return
	}
	if in.Email != "" && !emailRe.MatchString(in.Email) {
		publicErr(w, 400, "Az e-mail cím nem tűnik helyesnek.")
		return
	}
	mode = modePickup
	if in.Mode == "delivery" {
		mode = modeDelivery
		if in.City == "" || in.Street == "" {
			publicErr(w, 400, "Kiszállításhoz add meg a települést és a címet.")
			return
		}
		if a.fee(modeDelivery, in.City) < 0 {
			publicErr(w, 400, "Erre a településre sajnos nem szállítunk ki. Válaszd az elvitelt, vagy hívj minket: "+a.conf().EtteremTelefon)
			return
		}
	}
	payment = "Készpénz"
	if strings.HasPrefix(norm(in.Payment), "kart") {
		payment = "Kártya"
	}
	return mode, payment, true
}

// POST /api/public/order
func (a *App) handlePublicOrder(w http.ResponseWriter, r *http.Request) {
	var in publicOrderReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "Hibás kérés.")
		return
	}
	if msg := a.orderingBlocked(); msg != "" {
		publicErr(w, 503, msg)
		return
	}
	if in.Website != "" { // robot
		writeJSON(w, 200, map[string]any{"ok": true, "id": 0, "token": "x"})
		return
	}
	mode, payment, ok := a.checkContact(w, &in)
	if !ok {
		return
	}
	if len(in.Items) == 0 || len(in.Items) > 60 {
		publicErr(w, 400, "A kosár üres.")
		return
	}

	// árak a szerver saját étlapjából
	var lines []string
	sub := 0
	var packs []packLine
	for _, l := range in.Items {
		it := menuIdx[l.ID]
		if it == nil || l.Qty < 1 || l.Qty > 50 {
			publicErr(w, 400, "Az étlap közben változott – frissítsd az oldalt, és próbáld újra.")
			return
		}
		if menuOff[l.ID] {
			publicErr(w, 400, "Elfogyott: "+it.Name+". Vedd ki a kosárból, vagy frissítsd az oldalt.")
			return
		}
		price, label, pk := it.Price, it.Name, it.Pack
		if len(it.Variants) > 0 {
			price = -1
			for _, v := range it.Variants {
				if v.Label == l.Variant {
					price, label, pk = v.Price, it.Name+" "+v.Label, v.Pack
				}
			}
			if price < 0 {
				publicErr(w, 400, "Válassz méretet: "+it.Name)
				return
			}
		}
		sub += price * l.Qty
		packs = append(packs, packLine{it.Role, pk, l.Qty})
		lines = append(lines, fmt.Sprintf("%d× %s – %s Ft", l.Qty, label, formatHUF(price*l.Qty)))
	}
	pack := packTotal(packs)
	if pack > 0 {
		lines = append(lines, fmt.Sprintf("Csomagolás (elviteles dobozok) – %s Ft", formatHUF(pack)))
	}
	itemsText := strings.Join(lines, "\n")

	// védelem: túl sok rendelés rövid idő alatt, illetve dupla küldés
	if a.rateGate(w, norm(in.Phone)+"|"+itemsText) {
		return
	}

	o := &Order{
		Source: "web", Visitor: visitorKey(r), Name: in.Name, Phone: in.Phone, Email: in.Email, Mode: mode,
		Items: itemsText, Note: in.Note, Payment: payment,
		Token: randStr(22, "abcdefghjkmnpqrstuvwxyz23456789"),
		Extra: map[string]string{"Fizetés módja": payment},
	}
	if mode == modeDelivery {
		o.City, o.Street = in.City, in.Street
	}
	o.ExtID = "web:" + o.Token
	a.ingest(o) // kiszámolja a szállítási díjat is
	total := sub + pack
	parts := "étel " + formatHUF(sub)
	if pack > 0 {
		parts += " + csomagolás " + formatHUF(pack)
	}
	totalTxt := formatHUF(total) + " Ft"
	if pack > 0 {
		totalTxt += " (" + parts + ")"
	}
	if o.DeliveryFee > 0 {
		total += o.DeliveryFee
		totalTxt = fmt.Sprintf("%s Ft (%s + szállítás %s)", formatHUF(total), parts, formatHUF(o.DeliveryFee))
	} else if o.DeliveryFee < 0 {
		totalTxt = formatHUF(total) + " Ft + szállítás (egyeztetni)"
	}
	a.st.mu.Lock()
	if x := a.find(o.ID); x != nil {
		x.Total = total
		x.Extra["Végösszeg"] = totalTxt
		a.st.saveLocked()
	}
	a.st.mu.Unlock()
	log.Printf("Weboldali rendelés #%d: %s Ft", o.ID, formatHUF(total))
	writeJSON(w, 200, map[string]any{"ok": true, "id": o.ID, "token": o.Token})
}

// rateGate: dupla küldés és túl sok rendelés elleni védelem. true = a választ már megírta, ne folytasd.
func (a *App) rateGate(w http.ResponseWriter, key string) bool {
	rlMu.Lock()
	now := time.Now()
	keep := rlTimes[:0]
	for _, t := range rlTimes {
		if now.Sub(t) < 10*time.Minute {
			keep = append(keep, t)
		}
	}
	rlTimes = keep
	if t, ok := rlRecent[key]; ok && now.Sub(t) < 3*time.Minute {
		rlMu.Unlock()
		if o := a.findByKey(key); o != nil {
			writeJSON(w, 200, map[string]any{"ok": true, "id": o.ID, "token": o.Token})
			return true
		}
		publicErr(w, 429, "Ezt a rendelést már megkaptuk.")
		return true
	}
	if len(rlTimes) >= 30 {
		rlMu.Unlock()
		publicErr(w, 429, "Most nagyon sok rendelés érkezik – kérjük, hívj minket telefonon!")
		return true
	}
	rlTimes = append(rlTimes, now)
	rlRecent[key] = now
	for k, t := range rlRecent {
		if now.Sub(t) > 10*time.Minute {
			delete(rlRecent, k)
		}
	}
	rlMu.Unlock()
	return false
}

func (a *App) findByKey(key string) *Order {
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	for i := len(a.st.Orders) - 1; i >= 0 && i >= len(a.st.Orders)-50; i-- {
		o := a.st.Orders[i]
		if o.Token != "" && norm(o.Phone)+"|"+o.Items == key {
			cp := *o
			return &cp
		}
	}
	return nil
}

// GET /api/public/order/{token} – a vendég ezzel látja élőben, hol tart a rendelése.
func (a *App) handlePublicStatus(w http.ResponseWriter, r *http.Request) {
	tok := r.PathValue("token")
	if len(tok) < 16 {
		publicErr(w, 404, "nincs ilyen rendelés")
		return
	}
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	for i := len(a.st.Orders) - 1; i >= 0; i-- {
		o := a.st.Orders[i]
		if o.Token == tok {
			// A vendég csak azt látja, hogy várakozik, elfogadtuk vagy elutasítottuk.
			// A „Kész / átadva” állapot belső ügy, kifelé „elfogadva” marad.
			st, at, eta := o.Status, o.StatusAt, o.ETA
			if st == StDone {
				st = StPreparing
				if o.AcceptedAt.IsZero() { // régi rendelés: nem tudjuk, mikor fogadtuk el
					eta = 0
				} else {
					at = o.AcceptedAt
				}
			}
			writeJSON(w, 200, map[string]any{
				"id": o.ID, "status": st, "eta": eta, "reason": o.Reason,
				"status_at": at, "created": o.Created, "mode": o.Mode,
				"total": o.Total, "delivery_fee": o.DeliveryFee, "items": o.Items, "payment": o.Payment,
				"quick": strings.HasPrefix(o.Items, quickPrefix),
			})
			return
		}
	}
	publicErr(w, 404, "nincs ilyen rendelés")
}

// ---------------------------------------------------------------- szüneteltetés

// POST /api/settings/online – online rendelés fogadásának be/kikapcsolása.
func (a *App) handleSettingsOnline(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Paused bool   `json:"paused"`
		Note   string `json:"note"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "hibás kérés")
		return
	}
	in.Note = clip(in.Note, 200)
	if err := a.updateConf(func(c *Config) { c.OnlineSzunetel, c.OnlineUzenet = in.Paused, in.Note }); err != nil {
		publicErr(w, 500, "nem sikerült menteni")
		return
	}
	log.Printf("Online rendelés: szünetel=%v", in.Paused)
	writeJSON(w, 200, map[string]any{"ok": true, "paused": in.Paused})
}

// closedToday: ma korábban bezártunk (a személyzet jelezte; éjfélkor magától lejár).
func (a *App) closedToday() bool {
	z := a.conf().KorabbanZarva
	return z != "" && z == todayBudapest(time.Now())
}

// orderingBlocked: üres, ha most lehet online rendelni; különben a vendégnek szóló üzenet.
func (a *App) orderingBlocked() string {
	c := a.conf()
	if a.closedToday() {
		return "Ma már bezártunk. Holnap 11:00-tól újra várjuk a rendelésed! Előrendelést későbbi napra most is leadhatsz."
	}
	if c.OnlineSzunetel {
		msg := "Az online rendelés most szünetel."
		if c.OnlineUzenet != "" {
			msg += " " + c.OnlineUzenet
		}
		return msg + " Telefonon szívesen felvesszük: " + c.EtteremTelefon
	}
	return ""
}

// POST /api/settings/closed {"closed":true} – ma korábban bezártunk / mégis nyitva.
func (a *App) handleSettingsClosed(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Closed bool `json:"closed"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "hibás kérés")
		return
	}
	day := ""
	if in.Closed {
		day = todayBudapest(time.Now())
	}
	if err := a.updateConf(func(c *Config) { c.KorabbanZarva = day }); err != nil {
		publicErr(w, 500, "nem sikerült menteni")
		return
	}
	log.Printf("Korábbi zárás ma: %v", in.Closed)
	writeJSON(w, 200, map[string]any{"ok": true, "closed": in.Closed})
}

// ---------------------------------------------------------------- biztonság

// Az internet felől (Funnel) érkező belépési próbálkozások korlátozása:
// 15 percen belül 8 hibás jelszó után 15 percre lezár. Otthonról (helyi hálózat,
// Tailscale) a belépés ettől függetlenül működik.
var (
	lockMu    sync.Mutex
	lockFails []time.Time
	lockUntil time.Time
)

func loginLocked() bool {
	lockMu.Lock()
	defer lockMu.Unlock()
	return time.Now().Before(lockUntil)
}

func loginFailed() {
	lockMu.Lock()
	defer lockMu.Unlock()
	now := time.Now()
	keep := lockFails[:0]
	for _, t := range lockFails {
		if now.Sub(t) < 15*time.Minute {
			keep = append(keep, t)
		}
	}
	lockFails = append(keep, now)
	if len(lockFails) >= 8 {
		lockUntil = now.Add(15 * time.Minute)
		lockFails = nil
		log.Printf("Figyelem: túl sok hibás belépés az internet felől – a távoli belépés 15 percre zárolva.")
	}
}

// Alap biztonsági fejlécek. A rendelőoldal beágyazható (pl. a Wix oldalba), a kezelőfelület nem.
func secHeaders(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		p := r.URL.Path
		if p != "/rendeles" && !strings.HasPrefix(p, "/api/public/") && p != "/logo.png" {
			hd.Set("X-Frame-Options", "DENY")
		}
		h.ServeHTTP(w, r)
	})
}

// visitorKey: a weboldal "vv_id" sütijének hash-e – ugyanúgy képezve, mint a weboldal statisztikájában.
// Csak akkor van, ha a vendég elfogadta a sütit; magát a kódot nem tároljuk.
func visitorKey(r *http.Request) string {
	c, err := r.Cookie("vv_id")
	if err != nil || len(c.Value) != 32 {
		return ""
	}
	for _, ch := range c.Value {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return ""
		}
	}
	h := sha256.Sum256([]byte("vadvirag-visitor|" + c.Value))
	return hex.EncodeToString(h[:10])
}

var huDays = []string{"vasárnap", "hétfő", "kedd", "szerda", "csütörtök", "péntek", "szombat"}

// Napi ajánlat (a heti menü napjai): hétköznap 9:30 és 14:00 között rendelhető, mindig az aznapi.
// 9:30 és 11:00 között előrendelés: Zalaszentgrótra, Zalaudvarnokra és Zalaszentlászlóra ilyenkor
// nincs szállítási díj. A weboldal gombja „Napi ajánlat – <nap> (…)” kezdetű szöveget küld
// (a régebbi oldal „Heti menü – …”-t); ebből ismerjük fel.
const (
	dailyFrom    = 9*60 + 30 // 9:30
	dailyFreeTil = 11 * 60   // 11:00-ig ingyenes a kiszállítás a lenti településekre
	dailyTo      = 14 * 60   // 14:00
)

var dailyFreeTowns = []string{"Zalaszentgrót", "Zalaudvarnok", "Zalaszentlászló"}

func isDailyOffer(text string) bool {
	t := strings.TrimSpace(text)
	return strings.HasPrefix(t, "Napi ajánlat") || strings.HasPrefix(t, "Heti menü")
}

func budapestNow(now time.Time) time.Time {
	if loc, err := time.LoadLocation("Europe/Budapest"); err == nil {
		return now.In(loc)
	}
	return now
}

func hhmm(m int) string { return fmt.Sprintf("%d:%02d", m/60, m%60) }

// dailyOfferCheck: üres, ha rendelhető; különben a vendégnek szóló magyarázat.
func dailyOfferCheck(text string, now time.Time) string {
	if !isDailyOffer(text) {
		return ""
	}
	now = budapestNow(now)
	today := huDays[now.Weekday()]
	if now.Weekday() == time.Saturday || now.Weekday() == time.Sunday {
		return "A napi ajánlatot csak hétköznap lehet rendelni. Az étlapról vagy a hétvégi ajánlatból most is rendelhetsz!"
	}
	if m := now.Hour()*60 + now.Minute(); m < dailyFrom || m >= dailyTo {
		return "A napi ajánlat " + hhmm(dailyFrom) + " és " + hhmm(dailyTo) + " között rendelhető. Az étlapról most is rendelhetsz!"
	}
	first := strings.SplitN(strings.TrimSpace(text), "\n", 2)[0]
	for _, d := range huDays {
		if strings.Contains(first, "– "+d) && d != today {
			return "Mindig csak az aznapi napi ajánlat rendelhető (ma: " + today + ")."
		}
	}
	return ""
}

// dailyFreeDelivery: napi ajánlat előrendelése 11:00 előtt a kedvezményes településekre.
func (a *App) dailyFreeDelivery(text, mode, city string, now time.Time) bool {
	if !isDailyOffer(text) || mode != modeDelivery {
		return false
	}
	now = budapestNow(now)
	if m := now.Hour()*60 + now.Minute(); m < dailyFrom || m >= dailyFreeTil {
		return false
	}
	e := a.match(city)
	if e == nil {
		return false
	}
	for _, t := range dailyFreeTowns {
		if norm(t) == e.norm {
			return true
		}
	}
	return false
}
