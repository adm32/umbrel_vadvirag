package main

// Ajánlatok: hétvégi ajánlat és heti menü. Az adatok az ajanlatok.json fájlban vannak;
// hetente csak azt kell frissíteni. Hogy mi látszik, azt a szerver dönti el a budapesti
// dátum alapján, így a hétvégi ajánlat hétfőtől magától eltűnik. A napi ajánlatokból (heti
// menük, "weeks") mindig az látszik, amelyiknek a megjelenési ideje ("show_from") már elmúlt és
// még nem járt le; ha több is, a legkésőbb megjelenő. Így a jövő heti menü előre beírható, és
// pl. péntek 15:00-kor magától átvált („jövő heti menü”, majd hétfőtől az aktuális).

import (
	_ "embed"
	"encoding/json"
	"log"
	"net/http"
	"time"
	_ "time/tzdata" // a FROM scratch konténerben nincs időzóna-adatbázis
)

//go:embed ajanlatok.json
var offersJSON []byte

type L10n map[string]string

type offerCourse struct {
	Kind string `json:"kind"`
	Name L10n   `json:"name"`
	Desc L10n   `json:"desc"`
}

type weekendOffer struct {
	From    string        `json:"from"`
	To      string        `json:"to"`
	Title   L10n          `json:"title"`
	Courses []offerCourse `json:"courses"`
	Price   int           `json:"price"`
	Dessert *struct {
		Name  L10n `json:"name"`
		Desc  L10n `json:"desc"`
		Price int  `json:"price"`
	} `json:"dessert,omitempty"`
	DaysLeft int `json:"days_left"` // 0 = ma az utolsó nap
}

type weeklyDay struct {
	Date  string `json:"date"`
	Soups   []L10n `json:"soups"`
	Mains   []L10n `json:"mains"`
	Dessert L10n   `json:"dessert,omitempty"` // a háromfogásos menü desszertje
}

type weeklyMenu struct {
	From     string      `json:"from"`
	To       string      `json:"to"`
	ShowFrom string      `json:"show_from,omitempty"`
	Two      int         `json:"two"`
	Three    int         `json:"three"`
	Days     []weeklyDay `json:"days"`
	Upcoming bool        `json:"upcoming"` // még nem kezdődött el (jövő heti menü)
}

type offersFile struct {
	Weekend *weekendOffer `json:"weekend"`
	Weeks   []weeklyMenu  `json:"weeks"`
	Weekly  *weeklyMenu   `json:"weekly,omitempty"` // régi formátum (egy hét)
}

var (
	budapestTZ = mustLoc("Europe/Budapest")
	offers     offersFile
)

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("CET", 3600)
	}
	return l
}

func init() {
	if err := json.Unmarshal(offersJSON, &offers); err != nil {
		log.Printf("ajanlatok.json hibás, az ajánlatok nem jelennek meg: %v", err)
	}
	if offers.Weekly != nil {
		offers.Weeks = append(offers.Weeks, *offers.Weekly)
		offers.Weekly = nil
	}
}

// showTime: a "show_from" értéke ("2026-10-30" vagy "2026-10-30 15:00"); alapból 3 nappal a hét előtt.
func showTime(m weeklyMenu, from time.Time) time.Time {
	for _, f := range []string{"2006-01-02 15:04", "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(f, m.ShowFrom, budapestTZ); err == nil {
			return t
		}
	}
	return from.AddDate(0, 0, -3)
}

func day(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, budapestTZ)
	return t, err == nil
}

// activeOffers: ami az adott napon látszik.
func activeOffers(now time.Time) map[string]any {
	now = now.In(budapestTZ)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, budapestTZ)
	out := map[string]any{"today": today.Format("2006-01-02"), "now_min": now.Hour()*60 + now.Minute()}
	if w := offers.Weekend; w != nil {
		from, ok1 := day(w.From)
		to, ok2 := day(w.To)
		if ok1 && ok2 && !today.Before(from) && !today.After(to) {
			cp := *w
			cp.DaysLeft = int(to.Sub(today).Hours()/24 + 0.5)
			out["weekend"] = cp
		}
	}
	var best *weeklyMenu
	var bestShow time.Time
	for i := range offers.Weeks {
		m := &offers.Weeks[i]
		from, ok1 := day(m.From)
		to, ok2 := day(m.To)
		if !ok1 || !ok2 {
			continue
		}
		show := showTime(*m, from)
		if !now.Before(show) && !today.After(to) && (best == nil || show.After(bestShow)) {
			best, bestShow = m, show
		}
	}
	if best != nil {
		from, _ := day(best.From)
		cp := *best
		cp.Upcoming = today.Before(from)
		out["weekly"] = cp
	}
	return out
}

func handleOffers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(activeOffers(time.Now()))
}
