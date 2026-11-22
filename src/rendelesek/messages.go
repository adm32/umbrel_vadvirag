package main

// Üzenetek a weboldalról: asztalfoglalás, rendezvény-ajánlatkérés, egyéb kérdés.
// A weboldal a /api/public/message végpontra küldi; a kezelőprogramban a boríték ikon alatt látszanak.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Message struct {
	ID     int       `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"` // asztal | rendezveny | egyeb
	Name   string    `json:"name"`
	Phone  string    `json:"phone,omitempty"`
	Email  string    `json:"email,omitempty"`
	Date   string    `json:"date,omitempty"`
	Time   string    `json:"time,omitempty"`
	Guests int       `json:"guests,omitempty"`
	Text   string    `json:"text,omitempty"`
	Lang   string    `json:"lang,omitempty"`
	Done   bool      `json:"done,omitempty"`
}

var (
	msgMu    sync.Mutex
	msgTimes []time.Time
	msgLast  = map[string]time.Time{}
)

var msgKinds = map[string]string{"asztal": "Asztalfoglalás", "rendezveny": "Rendezvény", "egyeb": "Üzenet"}

func (a *App) msgPath() string { return filepath.Join(a.dataDir, "uzenetek.json") }

func (a *App) loadMessages() []Message {
	var ms []Message
	if b, err := os.ReadFile(a.msgPath()); err == nil {
		json.Unmarshal(b, &ms)
	}
	return ms
}

func (a *App) saveMessages(ms []Message) error {
	if len(ms) > 500 {
		ms = ms[len(ms)-500:]
	}
	b, _ := json.MarshalIndent(ms, "", " ")
	tmp := a.msgPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, a.msgPath())
}

func (a *App) unreadMessages() int {
	msgMu.Lock()
	defer msgMu.Unlock()
	n := 0
	for _, m := range a.loadMessages() {
		if !m.Done {
			n++
		}
	}
	return n
}

// POST /api/public/message
func (a *App) handlePublicMessage(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Kind, Name, Phone, Email, Date, Time, Text, Lang, Website string
		Guests                                                    int
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 32<<10)).Decode(&in); err != nil {
		publicErr(w, 400, "Hibás kérés.")
		return
	}
	if in.Website != "" { // robot
		writeJSON(w, 200, map[string]any{"ok": true})
		return
	}
	if _, ok := msgKinds[in.Kind]; !ok {
		in.Kind = "egyeb"
	}
	in.Name, in.Phone, in.Email = clip(strings.TrimSpace(in.Name), 80), clip(strings.TrimSpace(in.Phone), 30), clip(strings.TrimSpace(in.Email), 120)
	in.Date, in.Time, in.Text, in.Lang = clip(in.Date, 20), clip(in.Time, 10), clip(strings.TrimSpace(in.Text), 2000), clip(in.Lang, 5)
	if in.Guests < 0 || in.Guests > 1000 {
		in.Guests = 0
	}
	if in.Name == "" {
		publicErr(w, 400, "Kérjük, add meg a neved.")
		return
	}
	if len([]rune(in.Phone)) < 6 && !strings.Contains(in.Email, "@") {
		publicErr(w, 400, "Adj meg telefonszámot vagy e-mail címet, hogy válaszolni tudjunk.")
		return
	}
	if in.Text == "" && in.Date == "" {
		publicErr(w, 400, "Írd meg röviden, miben segíthetünk.")
		return
	}

	now := time.Now()
	key := norm(in.Name + "|" + in.Phone + "|" + in.Email + "|" + in.Text)
	msgMu.Lock()
	keep := msgTimes[:0]
	for _, t := range msgTimes {
		if now.Sub(t) < 10*time.Minute {
			keep = append(keep, t)
		}
	}
	msgTimes = keep
	if t, ok := msgLast[key]; ok && now.Sub(t) < 10*time.Minute {
		msgMu.Unlock()
		writeJSON(w, 200, map[string]any{"ok": true, "dup": true})
		return
	}
	if len(msgTimes) >= 20 {
		msgMu.Unlock()
		publicErr(w, 429, "Most nem tudjuk fogadni az üzenetet – kérjük, hívj minket telefonon!")
		return
	}
	msgTimes = append(msgTimes, now)
	msgLast[key] = now
	for k, t := range msgLast {
		if now.Sub(t) > 10*time.Minute {
			delete(msgLast, k)
		}
	}
	ms := a.loadMessages()
	id := 1
	if len(ms) > 0 {
		id = ms[len(ms)-1].ID + 1
	}
	m := Message{ID: id, At: now, Kind: in.Kind, Name: in.Name, Phone: in.Phone, Email: in.Email,
		Date: in.Date, Time: in.Time, Guests: in.Guests, Text: in.Text, Lang: in.Lang}
	ms = append(ms, m)
	err := a.saveMessages(ms)
	msgMu.Unlock()
	if err != nil {
		publicErr(w, 500, "Nem sikerült elmenteni az üzenetet. Kérjük, hívj minket telefonon!")
		return
	}
	go a.notifyMessage(m)
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (a *App) notifyMessage(m Message) {
	body := m.Text
	if m.Date != "" {
		d := m.Date
		if m.Time != "" {
			d += " " + m.Time
		}
		if m.Guests > 0 {
			d += fmt.Sprintf(", %d fő", m.Guests)
		}
		body = d + "\n" + body
	}
	if len([]rune(body)) > 140 {
		body = string([]rune(body)[:140]) + "…"
	}
	a.ntfyNotify("message", msgKinds[m.Kind]+" – "+m.Name, strings.TrimSpace(body), 3, "envelope")
	if push != nil && push.count() > 0 {
		push.broadcast(map[string]any{"title": msgKinds[m.Kind] + " – " + m.Name, "body": strings.TrimSpace(body), "tag": fmt.Sprintf("msg-%d", m.ID), "url": "/#uzenetek"})
	}
}

// GET /api/messages – legújabb elöl
func (a *App) handleMessages(w http.ResponseWriter, r *http.Request) {
	msgMu.Lock()
	ms := a.loadMessages()
	msgMu.Unlock()
	out := make([]Message, 0, len(ms))
	for i := len(ms) - 1; i >= 0; i-- {
		out = append(out, ms[i])
	}
	writeJSON(w, 200, out)
}

// POST /api/messages/{id}/done  {"done":true|false}
func (a *App) handleMessageDone(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	var in struct{ Done bool }
	json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&in)
	msgMu.Lock()
	defer msgMu.Unlock()
	ms := a.loadMessages()
	for i := range ms {
		if ms[i].ID == id {
			ms[i].Done = in.Done
			if err := a.saveMessages(ms); err != nil {
				writeJSON(w, 500, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
	}
	writeJSON(w, 404, map[string]string{"error": "nincs ilyen üzenet"})
}
