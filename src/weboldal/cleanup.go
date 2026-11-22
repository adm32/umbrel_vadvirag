package main

// A látogatottsági statisztika tisztítása.
//
//  1. Robotok utólag: a Facebook/Instagram ellenőrző gépei amerikai (külföldi) IP-ről, „Android 10”
//     telefonnak álcázva nyitják meg a megosztott linket. Ezeket az 1.3.5 óta nem számoljuk; a
//     korábbiakat induláskor egyszer kivesszük (a látogatások részletei 7 napig vannak meg).
//  2. Saját eszközök: az irányítópultból kapott „sajat” linkkel megnyitva a böngésző egy sütit kap,
//     onnantól nem számít bele, és a korábbi (7 napon belüli) látogatásai is kikerülnek.
//  3. Kézzel: az irányítópult „Egyenként” lapján bármelyik látogatás kivehető.
//
// Egy látogatás kivételekor a napi összesítőkből is levonjuk, amit hozzáadott.

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const cleanVersion = 1

const staffCookie = "vv_staff"

// looksLikeMetaBot: a régi (szűrés előtti) Facebook-ellenőrző látogatások mintája.
func looksLikeMetaBot(s *Session) bool {
	if s.Country == "" || s.Country == "HU" {
		return false
	}
	if s.OS != "Android 10" || (s.Model != "" && s.Model != "K") {
		return false
	}
	ref := strings.ToLower(s.Ref)
	if ref != "" && !strings.Contains(ref, "facebook") && !strings.Contains(ref, "instagram") && !strings.Contains(ref, "fb") {
		return false
	}
	for _, e := range s.Events {
		if e.N == "add" || e.N == "order_sent" || e.N == "order_page" || e.N == "town" || e.N == "form" {
			return false // valódi érdeklődés
		}
	}
	return !s.Ordered && s.Added == 0
}

func dec(m map[string]int, k string, n int) {
	if k == "" || m == nil || n <= 0 {
		return
	}
	m[k] -= n
	if m[k] <= 0 {
		delete(m, k)
	}
}

func clampDay(d *Day) {
	for _, p := range []*int{&d.Views, &d.Visitors, &d.Dark, &d.Consented, &d.Returning, &d.Errors} {
		if *p < 0 {
			*p = 0
		}
	}
	for i := range d.HourViews {
		d.HourViews[i] = max(d.HourViews[i], 0)
		d.HourVis[i] = max(d.HourVis[i], 0)
	}
}

// removeSessionLocked: egy látogatás törlése és a napi összesítők visszaszámolása (lock alatt).
func removeSessionLocked(day, key string) bool {
	ss := st.f.Sessions[day]
	s := ss[key]
	if s == nil {
		return false
	}
	if d := st.f.Days[day]; d != nil {
		d.Visitors--
		d.Views -= s.Views
		h := s.First.In(budapest).Hour()
		d.HourVis[h]--
		d.HourViews[h] -= s.Views
		dec(d.Devices, s.Device, 1)
		dec(d.Countries, s.Country, 1)
		dec(d.OS, s.OS, 1)
		dec(d.Browsers, s.Browser, 1)
		dec(d.Models, s.Model, 1)
		dec(d.Screens, s.Screen, 1)
		dec(d.Conns, s.Conn, 1)
		dec(d.BLangs, strings.Split(s.BLang, "-")[0], 1)
		dec(d.Campaigns, s.Campaign, 1)
		dec(d.Cities, s.City, 1)
		dec(d.Refs, clip(s.Ref, 60), 1)
		if s.Dark {
			d.Dark--
		}
		for _, p := range s.Pages {
			dec(d.Pages, p, 1)
		}
		dec(d.Pages, s.Entry, s.Views-len(s.Pages))
		// nyelv oldalmegnyitásonként: az oldal címéből (/en, /de), különben magyar
		langOf := func(p string) string {
			switch {
			case strings.HasPrefix(p, "/en"):
				return "en"
			case strings.HasPrefix(p, "/de"):
				return "de"
			}
			return "hu"
		}
		for _, p := range s.Pages {
			dec(d.Langs, langOf(p), 1)
		}
		dec(d.Langs, langOf(s.Entry), s.Views-len(s.Pages))
		for _, x := range s.Sections {
			dec(d.Sections, x, 1)
		}
		for _, e := range s.Events {
			switch e.N {
			case "page", "lang":
			case "error":
				d.Errors--
			default:
				dec(d.Events, e.N, 1)
			}
			switch e.N {
			case "add", "wheel_order":
				dec(d.Adds, e.V, 1)
			case "town":
				if t, ok := strings.CutSuffix(e.V, " (nem szállítunk oda)"); ok {
					dec(d.TownsNo, strings.ToLower(t), 1)
				} else {
					dec(d.Towns, e.V, 1)
				}
			}
		}
		if s.Cookie {
			d.Consented--
			if s.VisitNo > 1 {
				d.Returning--
			}
		}
		delete(d.seen, key)
		for i := range d.hourSeen {
			delete(d.hourSeen[i], key)
		}
		clampDay(d)
	}
	delete(ss, key)
	st.dirty = true
	return true
}

// foreignErr: olyan böngészőhiba, amely nem a weboldal kódjából jön. A Facebook, Instagram,
// Messenger és más alkalmazások beépített böngészője, illetve a böngészőbővítmények saját
// kódot futtatnak az oldalon; ha az hibázik, az nálunk is megjelenne, pedig nem a mi hibánk.
func foreignErr(msg, src string) bool {
	m := strings.ToLower(msg)
	for _, k := range []string{"_meta", "iabinnerframe", "fbinapp", "instagram", "__gcrweb", "webkit.messagehandlers",
		"_autofillcallbackhandler", "instantsearchsdkjsbridge", "zaloJSV2", "chrome-extension://", "moz-extension://",
		"safari-web-extension://", "resizeobserver loop"} {
		if strings.Contains(m, strings.ToLower(k)) {
			return true
		}
	}
	if strings.TrimSpace(m) == "script error." || strings.TrimSpace(m) == "script error" {
		return true // más domainről betöltött kód hibája, részletek nélkül
	}
	s := strings.ToLower(src)
	return strings.Contains(s, "-extension://")
}

// cleanupOnStart: induláskor – a már elmentett, nem a weboldalból jövő hibák törlése, és egyszer a régi robotlátogatások eltávolítása.
func cleanupOnStart() {
	st.Lock()
	defer st.Unlock()
	kept := st.f.Errors[:0]
	for _, e := range st.f.Errors {
		if e.Kind == "js" && foreignErr(e.Msg, e.Src) {
			st.dirty = true
			continue
		}
		kept = append(kept, e)
	}
	st.f.Errors = kept
	if st.f.CleanVer >= cleanVersion {
		return
	}
	n := 0
	for day, ss := range st.f.Sessions {
		for key, s := range ss {
			if looksLikeMetaBot(s) && removeSessionLocked(day, key) {
				n++
			}
		}
	}
	recompute()
	st.f.CleanVer = cleanVersion
	st.dirty = true
	log.Printf("statisztika tisztítás: %d régi robotlátogatás (Facebook-ellenőrző) eltávolítva", n)
}

// isStaff: saját eszköz (a „sajat” linkkel megjelölt böngésző).
func isStaff(r *http.Request) bool {
	c, err := r.Cookie(staffCookie)
	if err != nil || c.Value == "" {
		return false
	}
	st.Lock()
	defer st.Unlock()
	return st.f.StaffToken != "" && c.Value == st.f.StaffToken
}

// GET /sajat?t=<token> – ezt az eszközt ne számoljuk (a régi látogatásait is kivesszük).
func handleStaffLink(w http.ResponseWriter, r *http.Request) {
	tok := r.URL.Query().Get("t")
	st.Lock()
	valid := tok != "" && st.f.StaffToken != "" && tok == st.f.StaffToken
	removed := 0
	if valid {
		ck := cookieKey(r)
		id := visitorID(r)
		for day, ss := range st.f.Sessions {
			for key, s := range ss {
				if (ck != "" && s.CK == ck) || (day == dayKey(time.Now()) && key == id) {
					if removeSessionLocked(day, key) {
						removed++
					}
				}
			}
		}
		if ck != "" {
			delete(st.f.Known, ck)
		}
		recompute()
	}
	st.Unlock()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if !valid {
		w.WriteHeader(404)
		io.WriteString(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Vadvirág</title><body style="font:17px system-ui;padding:40px;max-width:520px;margin:auto"><h1>Érvénytelen link</h1><p>Ezt a linket az Irányítópult Látogatók lapjáról tudod megnyitni.</p><p><a href="/">Vissza a főoldalra</a></p>`)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: staffCookie, Value: tok, Path: "/", MaxAge: 2 * 365 * 24 * 3600, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"})
	log.Printf("statisztika: saját eszköz megjelölve, %d korábbi látogatása kivéve", removed)
	io.WriteString(w, `<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Vadvirág – saját eszköz</title><body style="font:17px/1.5 system-ui,sans-serif;padding:40px 24px;max-width:520px;margin:auto;background:#f4efe4;color:#2a2823"><h1 style="font-size:26px">✓ Ez az eszköz nem számít bele a statisztikába</h1><p>Mostantól az innen indított látogatások és rendelési kattintások nem kerülnek a látogatottsági számokba. A korábbi (7 napon belüli) látogatásaid is kikerültek.</p><p>Minden olyan telefonon és gépen nyisd meg ezt a linket, amelyről te is nézed az oldalt (Safari, Chrome stb. külön számít).</p><p><a href="/" style="color:#c4140a;font-weight:600">Tovább a weboldalra ›</a></p>`)
}

// POST /exclude {"date":"2026-10-24","id":"a1b2c3"} – egy látogatás kivétele (belső port, irányítópult).
func handleExclude(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date string `json:"date"`
		ID   string `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&in); err != nil || in.ID == "" {
		writeJSONs(w, map[string]any{"ok": false, "error": "hibás kérés"})
		return
	}
	st.Lock()
	defer st.Unlock()
	ok := false
	for key, s := range st.f.Sessions[in.Date] {
		if s.ID == in.ID {
			// ha sütis látogató, a többi napját is kivesszük, és többé nem számít visszatérőnek
			if s.CK != "" {
				delete(st.f.Known, s.CK)
			}
			ok = removeSessionLocked(in.Date, key)
			break
		}
	}
	if ok {
		recompute()
	}
	writeJSONs(w, map[string]any{"ok": ok})
}

// GET /staff-token – a „saját eszköz” link kódja (belső port, irányítópult).
func handleStaffToken(w http.ResponseWriter, r *http.Request) {
	st.Lock()
	if st.f.StaffToken == "" {
		st.f.StaffToken = randHex(12)
		st.dirty = true
	}
	t := st.f.StaffToken
	st.Unlock()
	writeJSONs(w, map[string]any{"token": t, "url": siteURL + "/sajat?t=" + t})
}
