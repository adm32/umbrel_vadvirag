package main

// Nyereményjáték a weboldalas rendelők között (nyeremenyjatek.json). Aki a játék ideje alatt a
// weboldalon rendel, és a rendelése a szállítási díj nélkül több mint a megadott összeg,
// automatikusan bekerül – telefonszámonként egyszer. A sorsolás a meglévő sorsolóval megy:
// a Rendelések appban a „Résztvevők betöltése” gomb ebből tölti be a listát.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

//go:embed nyeremenyjatek.json
var giveawayJSON []byte

type Giveaway struct {
	ID        string            `json:"id"`
	Title     map[string]string `json:"title"`
	Prize     map[string]string `json:"prize"`
	PrizeNote string            `json:"prize_note,omitempty"`
	Winners   int               `json:"winners"`
	Above     int               `json:"above"`
	From      string            `json:"from"`
	Until     string            `json:"until"`
	Draw      string            `json:"draw"`
	ShowUntil string            `json:"show_until"`
}

var giveaway *Giveaway

func init() {
	var g Giveaway
	if err := json.Unmarshal(giveawayJSON, &g); err != nil {
		log.Printf("nyeremenyjatek.json hibás: %v", err)
		return
	}
	if g.ID != "" {
		giveaway = &g
	}
}

func bpTime(s string) time.Time {
	loc, err := time.LoadLocation("Europe/Budapest")
	if err != nil {
		loc = time.FixedZone("CET", 3600)
	}
	for _, f := range []string{"2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(f, strings.TrimSpace(s), loc); err == nil {
			return t
		}
	}
	return time.Time{}
}

// az "until" időpont már nem számít bele (21:00 → 20:59:59-ig lehet rendelni)
func (g *Giveaway) end() time.Time   { return bpTime(g.Until) }
func (g *Giveaway) start() time.Time { return bpTime(g.From) }
func (g *Giveaway) showEnd() time.Time {
	if t := bpTime(g.ShowUntil); !t.IsZero() {
		return t.Add(time.Minute)
	}
	return g.end()
}

// phoneKey: a telefonszám utolsó 9 számjegye (06 30… és +36 30… ugyanaz).
func phoneKey(p string) string {
	var b strings.Builder
	for _, c := range p {
		if c >= '0' && c <= '9' {
			b.WriteRune(c)
		}
	}
	s := b.String()
	if len(s) > 9 {
		s = s[len(s)-9:]
	}
	return s
}

// orderValue: a rendelés értéke szállítási díj nélkül (ételek + dobozok).
func orderValue(o *Order) int {
	if o.Food > 0 {
		return o.Food + o.Pack
	}
	v := o.Total
	if o.DeliveryFee > 0 {
		v -= o.DeliveryFee
	}
	return v
}

type giveawayEntry struct {
	Name  string    `json:"name"`
	Phone string    `json:"phone"`
	Email string    `json:"email,omitempty"`
	Ref   string    `json:"ref"` // #12 vagy E7 (előrendelés)
	Value int       `json:"value"`
	At    time.Time `json:"at"`
	Line  string    `json:"line"` // a sorsolóba kerülő sor
}

// giveawayEntries: a jogosult résztvevők, telefonszámonként az első jogosult rendelés.
func (a *App) giveawayEntries(g *Giveaway) []giveawayEntry {
	from, end := g.start(), g.end()
	in := func(t time.Time) bool { return !t.Before(from) && t.Before(end) }
	var all []giveawayEntry
	if a.st != nil {
		a.st.mu.Lock()
		for _, o := range a.st.Orders {
			if o.Source != "web" || o.Test || o.Status == StRejected || !in(o.Created) {
				continue
			}
			if v := orderValue(o); v > g.Above {
				all = append(all, giveawayEntry{Name: strings.TrimSpace(o.Name), Phone: strings.TrimSpace(o.Phone), Email: strings.TrimSpace(o.Email), Ref: fmt.Sprintf("#%d", o.ID), Value: v, At: o.Created})
			}
		}
		a.st.mu.Unlock()
	}
	for _, p := range a.loadPreorders() {
		if p.Status == "lemondva" || !in(p.At) {
			continue
		}
		if v := p.Food + p.Pack; v > g.Above {
			all = append(all, giveawayEntry{Name: strings.TrimSpace(p.Name), Phone: strings.TrimSpace(p.Phone), Email: strings.TrimSpace(p.Email), Ref: fmt.Sprintf("E%d", p.ID), Value: v, At: p.At})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })
	seen := map[string]bool{}
	var out []giveawayEntry
	for _, e := range all {
		k := phoneKey(e.Phone)
		if k == "" {
			k = "név:" + norm(e.Name)
		}
		if seen[k] {
			continue
		}
		seen[k] = true
		if e.Email == "" {
			// ha ugyanez a vendég egy későbbi rendelésnél adott meg e-mail-címet, azt használjuk
			for _, x := range all {
				if x.Email != "" && phoneKey(x.Phone) == phoneKey(e.Phone) && phoneKey(e.Phone) != "" {
					e.Email = x.Email
					break
				}
			}
		}
		name := e.Name
		if name == "" {
			name = "Névtelen"
		}
		e.Line = clip(name, 40) + " – " + e.Phone + " – " + e.Ref
		out = append(out, e)
	}
	return out
}

// GET /api/giveaway – a személyzetnek: a játék adatai és a résztvevők.
func (a *App) handleGiveaway(w http.ResponseWriter, r *http.Request) {
	g := giveaway
	if g == nil {
		writeJSON(w, 200, map[string]any{"active": false})
		return
	}
	now := time.Now()
	es := a.giveawayEntries(g)
	if es == nil {
		es = []giveawayEntry{}
	}
	writeJSON(w, 200, map[string]any{"active": true, "giveaway": g, "running": now.Before(g.end()), "ended": !now.Before(g.end()), "entries": es})
}

// POST /api/giveaway/notify {"winners":["Név – telefon – #12", …]} – e-mail a nyerteseknek.
// A sorokból a rendelésszám alapján keressük meg a résztvevőt (és az e-mail-címét).
func (a *App) handleGiveawayNotify(w http.ResponseWriter, r *http.Request) {
	g := giveaway
	if g == nil {
		writeJSON(w, 400, map[string]string{"error": "nincs nyereményjáték"})
		return
	}
	var in struct {
		Winners []string `json:"winners"`
	}
	if err := readJSON(r, &in, 64<<10); err != nil || len(in.Winners) == 0 || len(in.Winners) > 10 {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	if !a.smtpEnabled() {
		writeJSON(w, 400, map[string]string{"error": "Az e-mail-küldés nincs beállítva (Továbbiak → Beállítások → E-mail)."})
		return
	}
	byRef := map[string]giveawayEntry{}
	for _, e := range a.giveawayEntries(g) {
		byRef[e.Ref] = e
	}
	c := a.conf()
	type res struct {
		Line  string `json:"line"`
		Name  string `json:"name"`
		Email string `json:"email,omitempty"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	var out []res
	for _, line := range in.Winners {
		parts := strings.Split(line, " – ")
		ref := strings.TrimSpace(parts[len(parts)-1])
		e, ok := byRef[ref]
		x := res{Line: line, Name: strings.TrimSpace(parts[0])}
		switch {
		case !ok:
			x.Error = "nem található a résztvevők között"
		case e.Email == "":
			x.Error = "nem adott meg e-mail-címet – hívd fel: " + e.Phone
		default:
			x.Email = e.Email
			if err := sendMail(c.SMTP, e.Email, giveawayMailSubject(), giveawayMailBody(g, e.Name, c.EtteremTelefon)); err != nil {
				x.Error = "nem sikerült elküldeni: " + err.Error()
			} else {
				x.OK = true
			}
		}
		out = append(out, x)
	}
	writeJSON(w, 200, map[string]any{"results": out})
}

func giveawayMailSubject() string { return "Gratulálunk, nyertél! – Vadvirág nyereményjáték" }

func giveawayMailBody(g *Giveaway, name, phone string) string {
	if name == "" {
		name = "Vendégünk"
	}
	if phone == "" {
		phone = "06 20 339 5099"
	}
	note := ""
	if g.PrizeNote != "" {
		note = " (" + g.PrizeNote + ")"
	}
	return "Kedves " + name + "!\n\n" +
		"Gratulálunk! Kisorsoltunk a Vadvirág Étterem nyereményjátékán: nyertél " + g.Prize["hu"] + note + ".\n\n" +
		"A nyeremény átvételéhez hívj minket a " + phone + " számon, vagy válaszolj erre az e-mailre, és megbeszéljük, mikor és hogyan kéred – helyben, elvitelre vagy kiszállítással.\n\n" +
		"Köszönjük, hogy a weboldalunkon rendeltél!\n\n" +
		"Vadvirág Étterem, Zalaudvarnok\nvadviragetterem.com\n"
}

// GET /api/public/giveaway – a weboldalnak és a rendelőoldalnak (személyes adat nélkül).
func (a *App) handlePublicGiveaway(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, 200, publicGiveaway(time.Now()))
}

func publicGiveaway(now time.Time) map[string]any {
	g := giveaway
	if g == nil || now.Before(g.start()) || !now.Before(g.showEnd()) {
		return map[string]any{"active": false}
	}
	return map[string]any{"active": true, "id": g.ID, "title": g.Title, "prize": g.Prize, "winners": g.Winners, "above": g.Above,
		"from": g.From, "until": g.Until, "draw": g.Draw, "running": now.Before(g.end())}
}
