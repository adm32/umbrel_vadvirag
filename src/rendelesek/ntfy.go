package main

// Értesítés az ntfy appba (iPhone / Android): a böngészős értesítések helyett vagy mellett.
// Beállítás: Rendelések app → Továbbiak → Beállítások → „Értesítés az ntfy appba”.
// A telefonon az ntfy appban fel kell iratkozni ugyanarra a témára.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

type NtfyConf struct {
	Server    string `json:"server,omitempty"`
	Topic     string `json:"topic,omitempty"`
	Token     string `json:"token,omitempty"`
	Orders    bool   `json:"orders"`    // új rendelés
	Messages  bool   `json:"messages"`  // üzenet a weboldalról (asztalfoglalás, rendezvény, kérdés)
	Preorders bool   `json:"preorders"` // előrendelés
}

func (n NtfyConf) server() string {
	s := strings.TrimRight(strings.TrimSpace(n.Server), "/")
	if s == "" {
		return "https://ntfy.sh"
	}
	return s
}

var ntfyClient = &http.Client{Timeout: 10 * time.Second}

// ntfySend: egy értesítés az ntfy szerverre (JSON-ben, így az ékezetes cím is rendben megy át).
func ntfySend(n NtfyConf, title, msg string, prio int, tags ...string) error {
	if strings.TrimSpace(n.Topic) == "" {
		return fmt.Errorf("nincs megadva téma")
	}
	body, _ := json.Marshal(map[string]any{"topic": strings.TrimSpace(n.Topic), "title": title, "message": msg, "priority": prio, "tags": tags})
	req, err := http.NewRequest("POST", n.server(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if t := strings.TrimSpace(n.Token); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := ntfyClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("ntfy: %s %s", resp.Status, strings.TrimSpace(string(b)))
	}
	return nil
}

// ntfyNotify: küldés a háttérben, ha az adott fajta értesítés be van kapcsolva.
func (a *App) ntfyNotify(kind, title, msg string, prio int, tags ...string) {
	n := a.conf().Ntfy
	if strings.TrimSpace(n.Topic) == "" {
		return
	}
	on := map[string]bool{"order": n.Orders, "message": n.Messages, "preorder": n.Preorders}[kind]
	if !on {
		return
	}
	go func() {
		if err := ntfySend(n, title, msg, prio, tags...); err != nil {
			log.Printf("ntfy értesítés nem ment ki: %v", err)
		}
	}()
}

// POST /api/settings/ntfy {"topic":"…","server":"…","token":"…","orders":true,…}
// A token csak akkor változik, ha új érték jön (üres = marad a régi; "-" = törlés).
func (a *App) handleSettingsNtfy(w http.ResponseWriter, r *http.Request) {
	var in NtfyConf
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	in.Topic = strings.TrimSpace(in.Topic)
	if in.Topic != "" && (len(in.Topic) > 64 || strings.ContainsAny(in.Topic, " /?#&")) {
		writeJSON(w, 400, map[string]string{"error": "A téma neve csak betűt, számot, kötőjelet és aláhúzást tartalmazhat."})
		return
	}
	if s := strings.TrimSpace(in.Server); s != "" && !strings.HasPrefix(s, "http://") && !strings.HasPrefix(s, "https://") {
		writeJSON(w, 400, map[string]string{"error": "A szerver címe http:// vagy https:// kezdetű legyen."})
		return
	}
	err := a.updateConf(func(c *Config) {
		tok := c.Ntfy.Token
		switch strings.TrimSpace(in.Token) {
		case "":
		case "-":
			tok = ""
		default:
			tok = strings.TrimSpace(in.Token)
		}
		in.Token = tok
		c.Ntfy = in
	})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": "nem sikerült menteni"})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/settings/test-ntfy
func (a *App) handleTestNtfy(w http.ResponseWriter, r *http.Request) {
	if err := ntfySend(a.conf().Ntfy, "Vadvirág – próba értesítés", "Ha ezt látod a telefonodon, az ntfy értesítések működnek. ✓", 3, "white_check_mark"); err != nil {
		writeJSON(w, 502, map[string]string{"error": "Nem sikerült elküldeni: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}
