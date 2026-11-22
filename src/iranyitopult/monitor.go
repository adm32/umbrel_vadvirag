// Vadvirág Irányítópult – állapotfigyelés: a mérések állapota, riasztások és az ntfy értesítés küldése.

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

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
