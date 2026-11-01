package main

// Automatikus változások a weboldalon, és a kézi felülbírálásuk.
//
//   - Évszakos téma: ősz (szept.–nov.), tél (dec. 1–15., jan. 3.–febr.), karácsony (dec. 16–30.,
//     ezen belül dec. 23–25. a „nagy” karácsony), szilveszter–újév (dec. 31.–jan. 2.), tavasszal és
//     nyáron az alap, pipacsos megjelenés. A /season.js minden oldalbetöltéskor megmondja az aktuálisat.
//   - A napi ajánlat (heti menü) és a hétvégi ajánlat a saját időpontjában jelenik meg és tűnik el
//     (ajanlat.go).
//
// Az irányítópultról (belső port: GET/POST /schedule) mindez látható és felülbírálható. A kézi
// beállítások a DATA_DIR/kapcsolok.json-ban vannak, és maguktól lejárnak: a téma a következő
// automatikus váltásig, a heti menü és a hétvégi ajánlat kapcsolója addig, amíg az az ajánlat tart.

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

type Switches struct {
	Season        string `json:"season,omitempty"`         // kézzel választott téma ("" = automatikus)
	SeasonFor     string `json:"season_for,omitempty"`     // melyik automatikus szakaszra szól (utána lejár)
	WeekNow       string `json:"week_now,omitempty"`       // ennek a hétnek a napi ajánlata látszik most (a "from" dátuma)
	WeeklyHidden  string `json:"weekly_hidden,omitempty"`  // ez a hét el van rejtve
	WeekendHidden string `json:"weekend_hidden,omitempty"` // ez a hétvégi ajánlat el van rejtve
}

var (
	swMu   sync.Mutex
	sw     Switches
	swPath string
)

var seasonOrder = []string{"base", "autumn", "winter", "christmas", "christmas2", "newyear"}

var seasonNames = map[string]string{
	"base":       "Alap (pipacsos)",
	"autumn":     "Ősz",
	"winter":     "Tél",
	"christmas":  "Karácsony",
	"christmas2": "Karácsony – ünnepnapok",
	"newyear":    "Szilveszter és újév",
}

var seasonPeriods = map[string]string{
	"base":       "márc. 1. – aug. 31.",
	"autumn":     "szept. 1. – nov. 30.",
	"winter":     "dec. 1–15. és jan. 3. – febr. vége",
	"christmas":  "dec. 16–22. és dec. 26–30.",
	"christmas2": "dec. 23–25.",
	"newyear":    "dec. 31. – jan. 2.",
}

func switchesInit() {
	if dir := env("DATA_DIR", ""); dir != "" {
		swPath = filepath.Join(dir, "kapcsolok.json")
		if b, err := os.ReadFile(swPath); err == nil {
			json.Unmarshal(b, &sw)
		}
	}
}

func getSwitches() Switches {
	swMu.Lock()
	defer swMu.Unlock()
	return sw
}

func setSwitches(f func(s *Switches)) error {
	swMu.Lock()
	defer swMu.Unlock()
	f(&sw)
	if swPath == "" {
		return nil
	}
	b, _ := json.MarshalIndent(sw, "", "  ")
	if err := os.WriteFile(swPath+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(swPath+".tmp", swPath)
}

// autoSeason: a naptár szerinti téma (budapesti idő).
func autoSeason(t time.Time) string {
	t = t.In(budapestTZ)
	m, d := t.Month(), t.Day()
	switch {
	case m >= time.September && m <= time.November:
		return "autumn"
	case m == time.December && d == 31, m == time.January && d <= 2:
		return "newyear"
	case m == time.December && d >= 23 && d <= 25:
		return "christmas2"
	case m == time.December && d >= 16:
		return "christmas"
	case m == time.December, m <= time.February:
		return "winter"
	}
	return "base"
}

// currentSeason: a weboldalon most látszó téma (kézi beállítás, ha még érvényes).
func currentSeason(now time.Time) (season, auto string, manual bool) {
	auto = autoSeason(now)
	s := getSwitches()
	if s.Season != "" && s.SeasonFor == auto {
		return s.Season, auto, true
	}
	return auto, auto, false
}

type seasonChange struct {
	At   string `json:"at"` // YYYY-MM-DD (éjfél, budapesti idő)
	Key  string `json:"key"`
	Name string `json:"name"`
}

// seasonChanges: a következő n automatikus témaváltás.
func seasonChanges(now time.Time, n int) []seasonChange {
	now = now.In(budapestTZ)
	d := time.Date(now.Year(), now.Month(), now.Day(), 12, 0, 0, 0, budapestTZ)
	cur := autoSeason(d)
	var out []seasonChange
	for i := 1; i <= 400 && len(out) < n; i++ {
		x := d.AddDate(0, 0, i)
		if s := autoSeason(x); s != cur {
			out = append(out, seasonChange{At: x.Format("2006-01-02"), Key: s, Name: seasonNames[s]})
			cur = s
		}
	}
	return out
}

// GET /season.js – minden oldalbetöltéskor: beállítja a <html data-season> értékét, mielőtt az oldal
// megjelenik. Előnézet: ?tema=osz|tel|karacsony|karacsony2|szilveszter|alap
func handleSeasonJS(w http.ResponseWriter, r *http.Request) {
	s, _, _ := currentSeason(time.Now())
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, max-age=0")
	fmt.Fprintf(w, `(function(){var s=%q,k={alap:"base",tavasz:"base",nyar:"base",osz:"autumn",tel:"winter",karacsony:"christmas",karacsony2:"christmas2",szilveszter:"newyear",ujev:"newyear",base:"base",autumn:"autumn",winter:"winter",christmas:"christmas",christmas2:"christmas2",newyear:"newyear"},m=/[?&]tema=([a-z0-9]+)/.exec(location.search);if(m&&k[m[1]])s=k[m[1]];document.documentElement.setAttribute("data-season",s)})();`, s)
}

// ---------------------------------------------------------------- irányítópult (belső port)

type weekInfo struct {
	From     string `json:"from"`
	To       string `json:"to"`
	ShowFrom string `json:"show_from"` // mikor jelenik meg ("2026-10-30 15:00")
	State    string `json:"state"`     // aktiv | elore | kozelgo | lejart
	Manual   bool   `json:"manual,omitempty"`
	Hidden   bool   `json:"hidden,omitempty"`
}

// GET /schedule – minden, ami a weboldalon magától változik, és mikor.
func handleSchedule(w http.ResponseWriter, r *http.Request) {
	now := time.Now().In(budapestTZ)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, budapestTZ)
	s := getSwitches()
	cur, auto, manual := currentSeason(now)
	themes := []map[string]string{}
	for _, k := range seasonOrder {
		themes = append(themes, map[string]string{"key": k, "name": seasonNames[k], "period": seasonPeriods[k]})
	}
	out := map[string]any{
		"now": now,
		"season": map[string]any{"current": cur, "name": seasonNames[cur], "auto": auto, "auto_name": seasonNames[auto],
			"manual": manual, "next": seasonChanges(now, 6), "themes": themes},
	}
	act := activeOffers(now)
	var weeks []weekInfo
	for _, m := range offers.Weeks {
		from, ok1 := day(m.From)
		to, ok2 := day(m.To)
		if !ok1 || !ok2 {
			continue
		}
		wi := weekInfo{From: m.From, To: m.To, ShowFrom: showTime(m, from).Format("2006-01-02 15:04"), Manual: s.WeekNow == m.From, Hidden: s.WeeklyHidden == m.From}
		switch shown, _ := act["weekly"].(weeklyMenu); {
		case today.After(to):
			wi.State = "lejart"
		case shown.From == m.From && today.Before(from):
			wi.State = "elore"
		case shown.From == m.From:
			wi.State = "aktiv"
		default:
			wi.State = "kozelgo"
		}
		weeks = append(weeks, wi)
	}
	sort.Slice(weeks, func(i, j int) bool { return weeks[i].From < weeks[j].From })
	out["weeks"] = weeks
	if wk := offers.Weekend; wk != nil {
		_, visible := act["weekend"]
		out["weekend"] = map[string]any{"from": wk.From, "to": wk.To, "title": wk.Title["hu"], "price": wk.Price,
			"visible": visible, "hidden": s.WeekendHidden == wk.From}
	}
	writeJSONs(w, out)
}

// POST /schedule {"season":"auto"|"winter"…} | {"week_now":"2026-11-02"|""} | {"weekly_hidden":"2026-11-02"|""} | {"weekend_hidden":true|false}
func handleScheduleSet(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Season        *string `json:"season"`
		WeekNow       *string `json:"week_now"`
		WeeklyHidden  *string `json:"weekly_hidden"`
		WeekendHidden *bool   `json:"weekend_hidden"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		http.Error(w, `{"error":"hibás kérés"}`, 400)
		return
	}
	validWeek := func(f string) bool {
		if f == "" {
			return true
		}
		for _, m := range offers.Weeks {
			if m.From == f {
				return true
			}
		}
		return false
	}
	if in.Season != nil && *in.Season != "auto" && seasonNames[*in.Season] == "" ||
		in.WeekNow != nil && !validWeek(*in.WeekNow) || in.WeeklyHidden != nil && !validWeek(*in.WeeklyHidden) {
		http.Error(w, `{"error":"ismeretlen érték"}`, 400)
		return
	}
	err := setSwitches(func(s *Switches) {
		if in.Season != nil {
			if *in.Season == "auto" {
				s.Season, s.SeasonFor = "", ""
			} else {
				s.Season, s.SeasonFor = *in.Season, autoSeason(time.Now())
			}
		}
		if in.WeekNow != nil {
			s.WeekNow = *in.WeekNow
			if s.WeekNow != "" && s.WeeklyHidden == s.WeekNow {
				s.WeeklyHidden = ""
			}
		}
		if in.WeeklyHidden != nil {
			s.WeeklyHidden = *in.WeeklyHidden
		}
		if in.WeekendHidden != nil {
			s.WeekendHidden = ""
			if *in.WeekendHidden && offers.Weekend != nil {
				s.WeekendHidden = offers.Weekend.From
			}
		}
	})
	if err != nil {
		http.Error(w, `{"error":"nem sikerült menteni"}`, 500)
		return
	}
	log.Printf("kapcsolók: %+v", getSwitches())
	writeJSONs(w, map[string]bool{"ok": true})
}
