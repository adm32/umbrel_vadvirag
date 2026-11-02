package main

// Belső adatcsatorna a Vadvirág Irányítópult számára.
//
// Csak akkor indul el, ha a DASH_ADDR környezeti változó meg van adva (Umbrelen ":9091").
// Ez a port nincs kivezetve se a gépre, se az internetre: csak az Umbrel belső
// Docker-hálózatán, a többi app konténeréből érhető el. Ezért itt nincs jelszó,
// de a vendégek telefonszámát, e-mail címét és utcáját nem is adjuk ki.

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type dashOrder struct {
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
	AcceptedAt  time.Time `json:"accepted_at,omitempty"`
	ETA         int       `json:"eta,omitempty"`
	Reason      string    `json:"reason,omitempty"`
	EmailState  string    `json:"email_state,omitempty"`
	EmailErr    string    `json:"email_err,omitempty"`
	Food        int       `json:"food"`
	Pack        int       `json:"pack"`
	TotalEst    bool      `json:"total_est,omitempty"`
	TotalManual bool      `json:"total_manual,omitempty"`
	Test        bool      `json:"test,omitempty"`
	TestAuto    bool      `json:"test_auto,omitempty"`
	Own         bool      `json:"own,omitempty"`
	Customer    string    `json:"cust,omitempty"`
	Free        bool      `json:"free_delivery,omitempty"`
	Visitor     string    `json:"visitor,omitempty"` // a telefonszámból képzett névtelen kód: így látszik, ki rendel újra
}

type dashMessage struct {
	ID     int       `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	Date   string    `json:"date,omitempty"`
	Time   string    `json:"time,omitempty"`
	Guests int       `json:"guests,omitempty"`
	Lang   string    `json:"lang,omitempty"`
	Done   bool      `json:"done"`
}

func (a *App) startDash() {
	addr := os.Getenv("DASH_ADDR")
	if addr == "" {
		return
	}
	started := time.Now()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /dash/info", func(w http.ResponseWriter, r *http.Request) {
		c := a.conf()
		a.st.mu.Lock()
		n := len(a.st.Orders)
		a.st.mu.Unlock()
		writeJSON(w, 200, map[string]any{
			"version":       appVersion,
			"started":       started,
			"online_paused": c.OnlineSzunetel,
			"closed_today":  a.closedToday(),
			"paused_note":   c.OnlineUzenet,
			"email_ready":   a.smtpEnabled(),
			"orders_total":  n,
			"unread":        a.unreadMessages(),
			"needs_setup":   a.needsSetup(),
			"preorder":      a.preInfo(),
		})
	})
	// GET /dash/preorders – az előrendelések (elérhetőségek nélkül) az irányítópult statisztikájához
	mux.HandleFunc("GET /dash/preorders", func(w http.ResponseWriter, r *http.Request) {
		preMu.Lock()
		ps := a.loadPreorders()
		preMu.Unlock()
		for i := range ps {
			ps[i].Phone, ps[i].Email, ps[i].Street, ps[i].Note = "", "", "", ""
		}
		writeJSON(w, 200, ps)
	})
	// GET /dash/orders?days=90 – az utolsó N nap rendelései (személyes elérhetőségek nélkül)
	mux.HandleFunc("GET /dash/orders", func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days <= 0 || days > 400 {
			days = 90
		}
		since := time.Now().AddDate(0, 0, -days)
		salt := a.conf().Titok
		own := map[string]bool{}
		for _, k := range a.conf().SajatTelefonok {
			own[k] = true
		}
		a.st.mu.Lock()
		out := make([]dashOrder, 0, 256)
		for _, o := range a.st.Orders {
			if o.Created.Before(since) {
				continue
			}
			out = append(out, dashOrder{ID: o.ID, Source: o.Source, Created: o.Created, Name: o.Name, Mode: o.Mode, City: o.City,
				Items: o.Items, Total: o.Total, DeliveryFee: o.DeliveryFee, Payment: o.Payment, Status: o.Status,
				StatusAt: o.StatusAt, AcceptedAt: o.AcceptedAt, ETA: o.ETA, Reason: o.Reason, EmailState: o.EmailState, EmailErr: o.EmailErr,
				Food: o.Food, Pack: o.Pack, TotalEst: o.TotalEst, TotalManual: o.TotalManual, Test: o.Test, TestAuto: o.TestAuto,
				Own: own[ownKey(o.Phone)], Customer: custID(salt, o.Phone), Visitor: o.Visitor, Free: o.FreeDelivery})
		}
		a.st.mu.Unlock()
		writeJSON(w, 200, out)
	})
	// POST /dash/orders/{id}/test {"test": true|false} – teszt rendelésnek jelölés (kimarad a statisztikából)
	mux.HandleFunc("POST /dash/orders/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Test bool `json:"test"`
		}
		id, ok := a.orderID(r)
		if !ok || readJSON(r, &in, 1<<10) != nil {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		a.st.mu.Lock()
		defer a.st.mu.Unlock()
		o := a.find(id)
		if o == nil {
			writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
			return
		}
		o.Test, o.TestAuto, o.TestChecked = in.Test, false, true
		a.st.saveLocked()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	// POST /dash/orders/{id}/own {"own": true|false} – „ez az én telefonszámom”: minden innen jövő
	// (régi és új) rendelés tesztnek számít, illetve a jelölés visszavonása.
	mux.HandleFunc("POST /dash/orders/{id}/own", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Own bool `json:"own"`
		}
		id, ok := a.orderID(r)
		if !ok || readJSON(r, &in, 1<<10) != nil {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		a.st.mu.Lock()
		o := a.find(id)
		var key string
		if o != nil {
			key = ownKey(o.Phone)
		}
		a.st.mu.Unlock()
		if key == "" {
			writeJSON(w, 400, map[string]string{"error": "ennél a rendelésnél nincs telefonszám"})
			return
		}
		err := a.updateConf(func(c *Config) {
			var keep []string
			for _, k := range c.SajatTelefonok {
				if k != key {
					keep = append(keep, k)
				}
			}
			if in.Own {
				keep = append(keep, key)
			}
			c.SajatTelefonok = keep
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		a.st.mu.Lock()
		n := 0
		for _, x := range a.st.Orders {
			if ownKey(x.Phone) == key {
				x.Test, x.TestAuto, x.TestChecked = in.Own, false, true
				n++
			}
		}
		a.st.saveLocked()
		a.st.mu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "orders": n})
	})
	mux.HandleFunc("GET /dash/messages", func(w http.ResponseWriter, r *http.Request) {
		ms := a.loadMessages()
		out := make([]dashMessage, 0, len(ms))
		for _, m := range ms {
			out = append(out, dashMessage{ID: m.ID, At: m.At, Kind: m.Kind, Name: m.Name, Date: m.Date, Time: m.Time, Guests: m.Guests, Lang: m.Lang, Done: m.Done})
		}
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /dash/schedule", a.handleDashSchedule)
	mux.HandleFunc("POST /dash/action", a.handleDashAction)
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("irányítópult adatcsatorna: %s", addr)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("irányítópult adatcsatorna nem indult: %v", err)
		}
	}()
}

// custID: a telefonszám számjegyeiből (utolsó 9) képzett rövid, visszafejthetetlen kód.
func custID(salt, phone string) string {
	var digits []rune
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			digits = append(digits, c)
		}
	}
	if len(digits) < 6 {
		return ""
	}
	if len(digits) > 9 {
		digits = digits[len(digits)-9:]
	}
	h := sha256.Sum256([]byte(salt + "|" + string(digits)))
	return hex.EncodeToString(h[:6])
}

// GET /dash/schedule – ami a rendeléskezelőben magától változik (és mikor), az irányítópult
// „Automatikus” nézetéhez: napi ajánlat idősávjai, korábbi zárás (éjfélkor lejár), szüneteltetés,
// előrendelés, nyereményjáték. Személyes adat nincs benne (a nyertesekből csak a név).
func (a *App) handleDashSchedule(w http.ResponseWriter, r *http.Request) {
	now := budapestNow(time.Now())
	c := a.conf()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, 1)
	out := map[string]any{
		"now":           now,
		"online_paused": c.OnlineSzunetel,
		"paused_note":   c.OnlineUzenet,
		"closed_today":  a.closedToday(),
		"closed_until":  midnight,
		"daily": map[string]any{"from": hhmm(dailyFrom), "free_til": hhmm(dailyFreeTil), "to": hhmm(dailyTo),
			"towns": dailyFreeTowns, "weekdays": true},
	}
	if p := a.preInfo(); p != nil {
		p["first"], p["last"] = preFirst, preLast
		out["preorder"] = p
	}
	if g := giveaway; g != nil {
		n := len(a.giveawayEntries(g))
		out["giveaway"] = map[string]any{"title": g.Title["hu"], "from": g.From, "until": g.Until, "draw": g.Draw,
			"show_until": g.ShowUntil, "min": g.Min, "entries": n, "running": !now.Before(g.start()) && now.Before(g.end()),
			"ended": !now.Before(g.end()), "shown": publicGiveaway(time.Now())["active"]}
	}
	if p := promo; p != nil {
		out["promo"] = map[string]any{"title": p.Title["hu"], "from": p.From, "to": p.To, "variant": p.Variant, "gift": p.Gift,
			"choices": p.Choices, "running": p.on(now), "ended": !now.Before(bpTime(p.To).AddDate(0, 0, 1)), "given": a.promoGiven(p)}
	}
	if rs := a.loadRaffles(); len(rs) > 0 {
		last := rs[len(rs)-1]
		names := func(ls []string) []string {
			out := make([]string, 0, len(ls))
			for _, l := range ls {
				out = append(out, strings.TrimSpace(strings.Split(l, " – ")[0]))
			}
			return out
		}
		out["last_raffle"] = map[string]any{"at": last.At, "title": last.Title, "count": last.Count, "winners": names(last.Winners), "reserves": names(last.Reserves)}
	}
	var off []string
	for _, s := range menuSections {
		if s.Off {
			off = append(off, s.Name)
		}
	}
	out["menu_off"] = off
	writeJSON(w, 200, out)
}

// POST /dash/action {"do":"pause"|"resume"|"close_today"|"open_today"|"preorder_soldout"|"preorder_available","note":"…"}
// Ugyanazok a kapcsolók, mint a Rendelések appban – az irányítópultról is elérhetők.
func (a *App) handleDashAction(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Do   string `json:"do"`
		Note string `json:"note"`
	}
	if readJSON(r, &in, 4<<10) != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	var f func(c *Config)
	switch in.Do {
	case "pause":
		f = func(c *Config) { c.OnlineSzunetel, c.OnlineUzenet = true, clip(strings.TrimSpace(in.Note), 200) }
	case "resume":
		f = func(c *Config) { c.OnlineSzunetel, c.OnlineUzenet = false, "" }
	case "close_today":
		f = func(c *Config) { c.KorabbanZarva = todayBudapest(time.Now()) }
	case "open_today":
		f = func(c *Config) { c.KorabbanZarva = "" }
	case "preorder_soldout", "preorder_available":
		if preOff == nil {
			writeJSON(w, 400, map[string]string{"error": "nincs előrendelhető ajánlat"})
			return
		}
		sold := in.Do == "preorder_soldout"
		f = func(c *Config) {
			m := map[string]bool{}
			for k, v := range c.Elfogyott {
				m[k] = v
			}
			if sold {
				m[preOff.ID] = true
			} else {
				delete(m, preOff.ID)
			}
			c.Elfogyott = m
		}
	default:
		writeJSON(w, 400, map[string]string{"error": "ismeretlen művelet"})
		return
	}
	if err := a.updateConf(f); err != nil {
		writeJSON(w, 500, map[string]string{"error": "nem sikerült menteni"})
		return
	}
	log.Printf("irányítópult: %s", in.Do)
	writeJSON(w, 200, map[string]bool{"ok": true})
}
