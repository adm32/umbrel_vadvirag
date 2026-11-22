// Vadvirág Irányítópult – HTTP: az irányítópult API-ja (áttekintés, beállítások, teszt értesítés).

package main

import (
	"encoding/json"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

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
	wg.Add(6)
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
	go func() {
		defer wg.Done()
		var p json.RawMessage
		if _, err := getJSON(ordersDash, "/dash/preorders", &p); err == nil {
			set("preorders", p)
		}
	}()
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
