package main

// Első beállítás, beállítások menü, mentés és költöztetés (régi gépről) – mind a böngészőből,
// hogy Umbrelen ne kelljen fájlokat szerkeszteni.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const appVersion = "2.0"

// viaFunnel: a kérés a nyilvános interneten (Tailscale Funnel) keresztül érkezett.
func viaFunnel(r *http.Request) bool { return r.Header.Get("Tailscale-Funnel-Request") != "" }

type importFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

type importReq struct {
	Password string       `json:"password"`
	Files    []importFile `json:"files"`
}

// parseImport: a feltöltött fájlok (beallitasok.json, rendelesek.json vagy egy mentés)
// felismerése. cfg/st nil, ha az adott fajta nem volt a fájlok között.
func parseImport(files []importFile) (cfg *Config, st *Store, err error) {
	for _, f := range files {
		name := f.Name
		if name == "" {
			name = "fájl"
		}
		var probe map[string]json.RawMessage
		if err := json.Unmarshal([]byte(f.Content), &probe); err != nil {
			// ertesitesek.json egy tömb – azt szándékosan kihagyjuk (új címen újra kell kapcsolni)
			var arr []any
			if json.Unmarshal([]byte(f.Content), &arr) == nil {
				continue
			}
			return nil, nil, fmt.Errorf("%s: nem olvasható (%v)", name, err)
		}
		var cfgRaw, ordRaw json.RawMessage
		switch {
		case probe["beallitasok"] != nil || probe["rendelesek"] != nil:
			cfgRaw, ordRaw = probe["beallitasok"], probe["rendelesek"]
		case probe["orders"] != nil || probe["next_id"] != nil:
			ordRaw = f2raw(f.Content)
		case probe["webhook_kulcs"] != nil || probe["belso_titok"] != nil || probe["szallitasi_dijak"] != nil || probe["jelszo"] != nil:
			cfgRaw = f2raw(f.Content)
		default:
			return nil, nil, fmt.Errorf("%s: ez nem a rendeléskezelő fájlja", name)
		}
		if cfgRaw != nil {
			c := defaultConfig()
			if err := json.Unmarshal(cfgRaw, &c); err != nil {
				return nil, nil, fmt.Errorf("%s: hibás beállítások (%v)", name, err)
			}
			normalizeConfig(&c)
			cfg = &c
		}
		if ordRaw != nil {
			s := &Store{NextID: 1}
			if err := json.Unmarshal(ordRaw, s); err != nil {
				return nil, nil, fmt.Errorf("%s: hibás rendelésfájl (%v)", name, err)
			}
			for _, o := range s.Orders {
				if o.ID >= s.NextID {
					s.NextID = o.ID + 1
				}
			}
			st = s
		}
	}
	return cfg, st, nil
}

func f2raw(s string) json.RawMessage { return json.RawMessage(s) }

func normalizeConfig(c *Config) {
	if c.Port == 0 {
		c.Port = 8080
	}
	if c.Wix.LekerdezesSec < 5 {
		c.Wix.LekerdezesSec = 20
	}
	if c.Wix.APICim == "" {
		c.Wix.APICim = "https://www.wixapis.com"
	}
	if c.Titok == "" {
		c.Titok = randStr(40, "abcdef0123456789")
	}
	if c.WebhookKulcs == "" {
		c.WebhookKulcs = randStr(24, "abcdefghjkmnpqrstuvwxyz23456789")
	}
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// commitAndRestart: új beállítások / rendelések kiírása, majd újraindulás, hogy minden
// (értesítések, díjtáblázat, Wix) az új adatokkal induljon. A zárakat szándékosan nem
// engedjük el: így a régi példány már nem írhatja felül a fájlokat.
func (a *App) commitAndRestart(w http.ResponseWriter, cfg Config, st *Store) {
	a.st.mu.Lock()
	a.cfgMu.Lock()
	unlock := func() { a.cfgMu.Unlock(); a.st.mu.Unlock() }
	if err := saveConfig(a.cfgPath, cfg); err != nil {
		unlock()
		writeJSON(w, 500, map[string]string{"error": "nem sikerült menteni: " + err.Error()})
		return
	}
	if st != nil {
		data, _ := json.MarshalIndent(st, "", " ")
		if err := writeFileAtomic(filepath.Join(a.dataDir, "rendelesek.json"), data); err != nil {
			unlock()
			writeJSON(w, 500, map[string]string{"error": "nem sikerült menteni a rendeléseket: " + err.Error()})
			return
		}
	}
	a.setSession(w, sessionToken(cfg.Titok))
	writeJSON(w, 200, map[string]any{"ok": true, "restart": true})
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	go func() {
		time.Sleep(900 * time.Millisecond)
		log.Printf("Újraindítás az új beállításokkal…")
		if err := restartSelf(); err != nil {
			log.Printf("Nem sikerült újraindulni (%v) – kilépek, indítsd újra a programot.", err)
			os.Exit(3)
		}
	}()
}

func readJSON(r *http.Request, v any, limit int64) error {
	return json.NewDecoder(io.LimitReader(r.Body, limit)).Decode(v)
}

// POST /api/setup – első indítás: jelszó megadása, opcionálisan a régi gép fájljaival.
func (a *App) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !a.needsSetup() {
		writeJSON(w, 409, map[string]string{"error": "a program már be van állítva"})
		return
	}
	if viaFunnel(r) {
		writeJSON(w, 403, map[string]string{"error": "Az első beállítást a helyi hálózatról végezd el (pl. http://umbrel.local)."})
		return
	}
	var in importReq
	if err := readJSON(r, &in, 32<<20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	in.Password = strings.TrimSpace(in.Password)
	if len([]rune(in.Password)) < 4 {
		writeJSON(w, 400, map[string]string{"error": "A jelszó legalább 4 karakter legyen."})
		return
	}
	imp, st, err := parseImport(in.Files)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	cfg := a.conf()
	if imp != nil {
		imp.Port = cfg.Port
		imp.Dijak = cfg.Dijak // költözéskor az új, beépített szállítási díjak érvényesek
		cfg = *imp
	}
	cfg.Jelszo = in.Password
	cfg.Titok = randStr(40, "abcdef0123456789")
	ensureVapid(&cfg)
	log.Printf("Első beállítás kész (régi beállítások: %v, régi rendelések: %v)", imp != nil, st != nil)
	a.commitAndRestart(w, cfg, st)
}

// POST /api/import – visszaállítás mentésből / régi gépről (bejelentkezve). A jelenlegi jelszó marad.
func (a *App) handleImport(w http.ResponseWriter, r *http.Request) {
	var in importReq
	if err := readJSON(r, &in, 32<<20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	imp, st, err := parseImport(in.Files)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	if imp == nil && st == nil {
		writeJSON(w, 400, map[string]string{"error": "Nem választottál ki fájlt."})
		return
	}
	cur := a.conf()
	cfg := cur
	if imp != nil {
		cfg = *imp
		cfg.Port, cfg.Jelszo, cfg.Titok = cur.Port, cur.Jelszo, cur.Titok
		ensureVapid(&cfg)
	}
	log.Printf("Visszaállítás (beállítások: %v, rendelések: %v)", imp != nil, st != nil)
	a.commitAndRestart(w, cfg, st)
}

// GET /api/backup – minden adat egy fájlban (beállítások + rendelések).
func (a *App) handleBackup(w http.ResponseWriter, r *http.Request) {
	cfg := a.conf()
	a.st.mu.Lock()
	ord, _ := json.Marshal(a.st)
	a.st.mu.Unlock()
	out, _ := json.MarshalIndent(map[string]any{
		"tipus":       "vadvirag-rendeleskezelo-mentes",
		"verzio":      appVersion,
		"keszult":     time.Now(),
		"beallitasok": cfg,
		"rendelesek":  json.RawMessage(ord),
	}, "", " ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vadvirag-mentes-%s.json"`, time.Now().Format("2006-01-02")))
	w.Header().Set("Cache-Control", "no-store")
	w.Write(out)
}

// GET /api/settings
func (a *App) handleSettings(w http.ResponseWriter, r *http.Request) {
	c := a.conf()
	addr := c.SMTP.FeladoEmail
	if addr == "" {
		addr = c.SMTP.Felhasznalo
	}
	writeJSON(w, 200, map[string]any{
		"version":      appVersion,
		"restaurant":   c.EtteremNeve,
		"webhook_path": "/webhook/" + c.WebhookKulcs,
		"container":    a.container,
		"email": map[string]any{
			"enabled":      a.smtpEnabled(),
			"address":      addr,
			"has_password": c.SMTP.Jelszo != "",
			"server":       c.SMTP.Szerver,
			"port":         c.SMTP.Port,
		},
	})
}

// POST /api/settings/email – Gmail (vagy más SMTP) beállítása. Üres jelszó = a régi marad.
func (a *App) handleSettingsEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Address  string `json:"address"`
		Password string `json:"password"`
		Server   string `json:"server"`
		Port     int    `json:"port"`
		Off      bool   `json:"off"`
	}
	if err := readJSON(r, &in, 1<<16); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	in.Address = strings.TrimSpace(in.Address)
	pass := strings.ReplaceAll(strings.TrimSpace(in.Password), " ", "") // a Gmail szóközökkel mutatja
	if !in.Off && !strings.Contains(in.Address, "@") {
		writeJSON(w, 400, map[string]string{"error": "Adj meg egy érvényes e-mail címet."})
		return
	}
	err := a.updateConf(func(c *Config) {
		if in.Off {
			c.SMTP.Szerver = ""
			return
		}
		server := strings.TrimSpace(in.Server)
		if server == "" {
			server = "smtp.gmail.com"
		}
		port := in.Port
		if port == 0 {
			port = 587
		}
		c.SMTP.Szerver, c.SMTP.Port = server, port
		c.SMTP.Felhasznalo, c.SMTP.FeladoEmail = in.Address, in.Address
		if c.SMTP.FeladoNev == "" {
			c.SMTP.FeladoNev = c.EtteremNeve
		}
		if pass != "" {
			c.SMTP.Jelszo = pass
		}
	})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	if !in.Off && a.conf().SMTP.Jelszo == "" {
		writeJSON(w, 200, map[string]any{"ok": true, "warning": "Mentve, de jelszó (alkalmazásjelszó) nélkül nem fog tudni küldeni."})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// POST /api/settings/test-email – próbalevél a beállított címre (vagy a megadottra).
func (a *App) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	readJSON(r, &in, 1<<12)
	c := a.conf()
	if !a.smtpEnabled() {
		writeJSON(w, 400, map[string]string{"error": "Az e-mail küldés nincs beállítva."})
		return
	}
	to := strings.TrimSpace(in.To)
	if to == "" {
		to = c.SMTP.FeladoEmail
	}
	err := sendMail(c.SMTP, to, c.EtteremNeve+" – próbalevél",
		"Ez egy próbalevél a rendeléskezelőből.\n\nHa megkaptad, a vendégek is meg fogják kapni a visszaigazolásokat.\n")
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": "Nem ment el: " + err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "to": to})
}

// POST /api/settings/password – jelszócsere. Minden más eszköz kijelentkezik.
func (a *App) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := readJSON(r, &in, 1<<12); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	if in.Current != a.conf().Jelszo {
		time.Sleep(1200 * time.Millisecond)
		writeJSON(w, 403, map[string]string{"error": "A jelenlegi jelszó nem jó."})
		return
	}
	in.New = strings.TrimSpace(in.New)
	if len([]rune(in.New)) < 4 {
		writeJSON(w, 400, map[string]string{"error": "Az új jelszó legalább 4 karakter legyen."})
		return
	}
	var tok string
	err := a.updateConf(func(c *Config) {
		c.Jelszo = in.New
		c.Titok = randStr(40, "abcdef0123456789")
		tok = sessionToken(c.Titok)
	})
	if err != nil {
		writeJSON(w, 500, map[string]string{"error": err.Error()})
		return
	}
	a.setSession(w, tok)
	writeJSON(w, 200, map[string]any{"ok": true})
}
