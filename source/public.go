package main

// Nyilvános rendelőoldal (/rendeles): a vendég kattintással állítja össze a rendelést,
// a szerver a saját étlapja alapján számolja az árat, majd a vendég élőben látja az állapotát.

import (
	_ "embed"
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
}

type MenuItem struct {
	ID       string        `json:"id"`
	Name     string        `json:"name"`
	Desc     string        `json:"desc,omitempty"`
	Alg      string        `json:"alg,omitempty"`
	Price    int           `json:"price,omitempty"`
	Variants []MenuVariant `json:"variants,omitempty"`
}

type MenuSection struct {
	ID    string     `json:"id"`
	Name  string     `json:"name"`
	Short string     `json:"short"`
	Items []MenuItem `json:"items"`
}

var (
	menuSections []MenuSection
	menuIdx      = map[string]*MenuItem{}
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
			menuIdx[it.ID] = it
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
		"restaurant": c.EtteremNeve,
		"phone":      c.EtteremTelefon,
		"sections":   menuSections,
		"fees":       fees,
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

// POST /api/public/order
func (a *App) handlePublicOrder(w http.ResponseWriter, r *http.Request) {
	var in publicOrderReq
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "Hibás kérés.")
		return
	}
	if in.Website != "" { // robot
		writeJSON(w, 200, map[string]any{"ok": true, "id": 0, "token": "x"})
		return
	}
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
	mode := modePickup
	if in.Mode == "delivery" {
		mode = modeDelivery
		if in.City == "" || in.Street == "" {
			publicErr(w, 400, "Kiszállításhoz add meg a települést és a címet.")
			return
		}
	}
	payment := "Készpénz"
	if strings.HasPrefix(norm(in.Payment), "kart") {
		payment = "Kártya"
	}
	if len(in.Items) == 0 || len(in.Items) > 60 {
		publicErr(w, 400, "A kosár üres.")
		return
	}

	// árak a szerver saját étlapjából
	var lines []string
	sub := 0
	for _, l := range in.Items {
		it := menuIdx[l.ID]
		if it == nil || l.Qty < 1 || l.Qty > 50 {
			publicErr(w, 400, "Az étlap közben változott – frissítsd az oldalt, és próbáld újra.")
			return
		}
		price, label := it.Price, it.Name
		if len(it.Variants) > 0 {
			price = -1
			for _, v := range it.Variants {
				if v.Label == l.Variant {
					price, label = v.Price, it.Name+" "+v.Label
				}
			}
			if price < 0 {
				publicErr(w, 400, "Válassz méretet: "+it.Name)
				return
			}
		}
		sub += price * l.Qty
		lines = append(lines, fmt.Sprintf("%d× %s – %s Ft", l.Qty, label, formatHUF(price*l.Qty)))
	}
	itemsText := strings.Join(lines, "\n")

	// védelem: túl sok rendelés rövid idő alatt, illetve dupla küldés
	key := norm(in.Phone) + "|" + itemsText
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
			return
		}
		publicErr(w, 429, "Ezt a rendelést már megkaptuk.")
		return
	}
	if len(rlTimes) >= 30 {
		rlMu.Unlock()
		publicErr(w, 429, "Most nagyon sok rendelés érkezik – kérjük, hívj minket telefonon!")
		return
	}
	rlTimes = append(rlTimes, now)
	rlRecent[key] = now
	for k, t := range rlRecent {
		if now.Sub(t) > 10*time.Minute {
			delete(rlRecent, k)
		}
	}
	rlMu.Unlock()

	o := &Order{
		Source: "web", Name: in.Name, Phone: in.Phone, Email: in.Email, Mode: mode,
		Items: itemsText, Note: in.Note, Payment: payment,
		Token: randStr(22, "abcdefghjkmnpqrstuvwxyz23456789"),
		Extra: map[string]string{"Fizetés módja": payment},
	}
	if mode == modeDelivery {
		o.City, o.Street = in.City, in.Street
	}
	o.ExtID = "web:" + o.Token
	a.ingest(o) // kiszámolja a szállítási díjat is
	total := sub
	totalTxt := formatHUF(sub) + " Ft"
	if o.DeliveryFee > 0 {
		total += o.DeliveryFee
		totalTxt = fmt.Sprintf("%s Ft (étel %s + szállítás %s)", formatHUF(total), formatHUF(sub), formatHUF(o.DeliveryFee))
	} else if o.DeliveryFee < 0 {
		totalTxt = formatHUF(sub) + " Ft + szállítás (egyeztetni)"
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
			writeJSON(w, 200, map[string]any{
				"id": o.ID, "status": o.Status, "eta": o.ETA, "reason": o.Reason,
				"status_at": o.StatusAt, "created": o.Created, "mode": o.Mode,
				"total": o.Total, "delivery_fee": o.DeliveryFee, "items": o.Items,
			})
			return
		}
	}
	publicErr(w, 404, "nincs ilyen rendelés")
}
