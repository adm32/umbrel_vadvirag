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
	Customer    string    `json:"cust,omitempty"` // a telefonszámból képzett névtelen kód: így látszik, ki rendel újra
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
			"paused_note":   c.OnlineUzenet,
			"email_ready":   a.smtpEnabled(),
			"orders_total":  n,
			"unread":        a.unreadMessages(),
			"needs_setup":   a.needsSetup(),
		})
	})
	// GET /dash/orders?days=90 – az utolsó N nap rendelései (személyes elérhetőségek nélkül)
	mux.HandleFunc("GET /dash/orders", func(w http.ResponseWriter, r *http.Request) {
		days, _ := strconv.Atoi(r.URL.Query().Get("days"))
		if days <= 0 || days > 400 {
			days = 90
		}
		since := time.Now().AddDate(0, 0, -days)
		salt := a.conf().Titok
		a.st.mu.Lock()
		out := make([]dashOrder, 0, 256)
		for _, o := range a.st.Orders {
			if o.Created.Before(since) {
				continue
			}
			out = append(out, dashOrder{ID: o.ID, Source: o.Source, Created: o.Created, Name: o.Name, Mode: o.Mode, City: o.City,
				Items: o.Items, Total: o.Total, DeliveryFee: o.DeliveryFee, Payment: o.Payment, Status: o.Status,
				StatusAt: o.StatusAt, AcceptedAt: o.AcceptedAt, ETA: o.ETA, Reason: o.Reason, EmailState: o.EmailState, EmailErr: o.EmailErr,
				Customer: custID(salt, o.Phone)})
		}
		a.st.mu.Unlock()
		writeJSON(w, 200, out)
	})
	mux.HandleFunc("GET /dash/messages", func(w http.ResponseWriter, r *http.Request) {
		ms := a.loadMessages()
		out := make([]dashMessage, 0, len(ms))
		for _, m := range ms {
			out = append(out, dashMessage{ID: m.ID, At: m.At, Kind: m.Kind, Name: m.Name, Date: m.Date, Time: m.Time, Guests: m.Guests, Lang: m.Lang, Done: m.Done})
		}
		writeJSON(w, 200, out)
	})
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
