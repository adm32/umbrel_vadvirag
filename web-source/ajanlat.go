package main

// Ajánlatok: hétvégi ajánlat és heti menü. Az adatok az ajanlatok.json fájlban vannak;
// hetente csak azt kell frissíteni. Hogy mi látszik, azt a szerver dönti el a budapesti
// dátum alapján, így a hétvégi ajánlat hétfőtől magától eltűnik, a heti menü pedig csak a
// saját hetén (és az azt megelőző hétvégén, „jövő heti menüként”) látszik.

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
	Soups []L10n `json:"soups"`
	Mains []L10n `json:"mains"`
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
	Weekly  *weeklyMenu   `json:"weekly"`
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
}

func day(s string) (time.Time, bool) {
	t, err := time.ParseInLocation("2006-01-02", s, budapestTZ)
	return t, err == nil
}

// activeOffers: ami az adott napon látszik.
func activeOffers(now time.Time) map[string]any {
	now = now.In(budapestTZ)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, budapestTZ)
	out := map[string]any{"today": today.Format("2006-01-02")}
	if w := offers.Weekend; w != nil {
		from, ok1 := day(w.From)
		to, ok2 := day(w.To)
		if ok1 && ok2 && !today.Before(from) && !today.After(to) {
			cp := *w
			cp.DaysLeft = int(to.Sub(today).Hours()/24 + 0.5)
			out["weekend"] = cp
		}
	}
	if m := offers.Weekly; m != nil {
		from, ok1 := day(m.From)
		to, ok2 := day(m.To)
		show := from.AddDate(0, 0, -3)
		if s, ok := day(m.ShowFrom); ok {
			show = s
		}
		if ok1 && ok2 && !today.Before(show) && !today.After(to) {
			cp := *m
			cp.Upcoming = today.Before(from)
			out["weekly"] = cp
		}
	}
	return out
}

func handleOffers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	json.NewEncoder(w).Encode(activeOffers(time.Now()))
}
