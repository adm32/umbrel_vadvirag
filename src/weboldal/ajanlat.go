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
	"sort"
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
	Date    string `json:"date"`
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

// Rendezvény (pl. bál): a "show_from" naptól a rendezvény napjáig ("date") látszik.
type event struct {
	ID       string `json:"id"`
	Date     string `json:"date"`
	ShowFrom string `json:"show_from,omitempty"`
	Time     string `json:"time,omitempty"`
	Title    L10n   `json:"title"`
	Lead     L10n   `json:"lead,omitempty"`
	Facts    []struct {
		Icon string `json:"icon"`
		Text L10n   `json:"text"`
	} `json:"facts,omitempty"`
	EntryTitle L10n `json:"entry_title,omitempty"`
	Entry      []struct {
		Head  L10n `json:"head,omitempty"` // csoport címe (pl. „Belépő vacsora nélkül”)
		Note  L10n `json:"note,omitempty"`
		Label L10n `json:"label"`
		Desc  L10n `json:"desc"`
		Price int  `json:"price"`
		Unit  L10n `json:"unit,omitempty"` // pl. „/ fő”
	} `json:"entry,omitempty"`
	Platter *struct {
		Title  L10n `json:"title"`
		Sub    L10n `json:"sub,omitempty"`
		Groups []struct {
			Name  L10n   `json:"name"`
			Items []L10n `json:"items"`
		} `json:"groups"`
	} `json:"platter,omitempty"`
	Art      string `json:"art,omitempty"`
	ArtW     int    `json:"art_w,omitempty"` // a kép mérete (alapból 1000×620)
	ArtH     int    `json:"art_h,omitempty"`
	NoWheel  bool   `json:"no_wheel,omitempty"` // a bálos Katalin-kerék dísz nélkül
	Icon     string `json:"icon,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Book     L10n   `json:"book,omitempty"`     // a gomb felirata (alapból „Asztalt foglalok”)
	HeroSub  L10n   `json:"hero_sub,omitempty"` // a nyitókép linkjének alsó sora, {date}
	Msg      L10n   `json:"msg,omitempty"`      // az üzenet előre kitöltött szövege, {title} {date}
	DaysLeft int    `json:"days_left"`          // 0 = ma van
}

// Ünnepi ajánlat (pl. karácsony): a "show_from" naptól az "until" napig látszik. Csak tájékoztat,
// online nem rendelhető (telefonon vagy személyesen).
type holidayPlatter struct {
	Name      L10n   `json:"name"`
	P2        int    `json:"p2"`
	P4        int    `json:"p4"`
	Items     []L10n `json:"items"`
	SideLabel L10n   `json:"side_label"`
	Side      L10n   `json:"side"`
}

type holidayOffer struct {
	ID        string           `json:"id"`
	ShowFrom  string           `json:"show_from"`
	Until     string           `json:"until"`
	Title     L10n             `json:"title"`
	When      L10n             `json:"when"`
	OrderNote L10n             `json:"order_note"`
	Platters  []holidayPlatter `json:"platters"`
	Special   *struct {
		Tag   L10n `json:"tag"`
		Name  L10n `json:"name"`
		Desc  L10n `json:"desc"`
		Price int  `json:"price"`
		Unit  L10n `json:"unit"`
	} `json:"special,omitempty"`
	ClassicsTitle L10n             `json:"classics_title"`
	Classics      []holidayPlatter `json:"classics"`
	PreorderTitle L10n             `json:"preorder_title"`
	Preorder      []struct {
		Name  L10n `json:"name"`
		Price int  `json:"price"`
		Unit  L10n `json:"unit,omitempty"`
	} `json:"preorder"`
}

// Vadvirág Disco: a "show_from" naptól az "until" napig látszik; minden buli a saját napja után eltűnik.
type discoParty struct {
	ID         string `json:"id"`
	Date       string `json:"date"`
	Star       string `json:"star"`
	N          int    `json:"n"`
	Title      L10n   `json:"title"`
	Poster     string `json:"poster"`
	PosterFull string `json:"poster_full"`
	DaysLeft   int    `json:"days_left"`
}

type discoOffer struct {
	ID          string       `json:"id"`
	ShowFrom    string       `json:"show_from"`
	Until       string       `json:"until"`
	Title       L10n         `json:"title"`
	Tagline     L10n         `json:"tagline"`
	Video       string       `json:"video,omitempty"`
	VideoPoster string       `json:"video_poster,omitempty"`
	Parties     []discoParty `json:"parties"`
}

type offersFile struct {
	Holiday *holidayOffer `json:"holiday,omitempty"`
	Disco   *discoOffer   `json:"disco,omitempty"`
	Weekend *weekendOffer `json:"weekend"`
	Weeks   []weeklyMenu  `json:"weeks"`
	Weekly  *weeklyMenu   `json:"weekly,omitempty"` // régi formátum (egy hét)
	Events  []event       `json:"events,omitempty"`
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

// activeOffers: ami az adott pillanatban látszik (a kézi kapcsolókkal együtt, lásd schedule.go).
func activeOffers(now time.Time) map[string]any {
	now = now.In(budapestTZ)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, budapestTZ)
	out := map[string]any{"today": today.Format("2006-01-02"), "now_min": now.Hour()*60 + now.Minute()}
	sw := getSwitches()
	if w := offers.Weekend; w != nil && sw.WeekendHidden != w.From {
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
		if today.After(to) {
			continue
		}
		if sw.WeekNow == m.From { // kézzel előrehozva: ez látszik, amíg tart
			best = m
			break
		}
		show := showTime(*m, from)
		if !now.Before(show) && (best == nil || show.After(bestShow)) {
			best, bestShow = m, show
		}
	}
	var evs []event
	for _, e := range offers.Events {
		d, ok := day(e.Date)
		if !ok || today.After(d) {
			continue
		}
		if f, ok := day(e.ShowFrom); ok && today.Before(f) {
			continue
		}
		e.DaysLeft = int(d.Sub(today).Hours()/24 + 0.5)
		evs = append(evs, e)
	}
	sort.SliceStable(evs, func(i, j int) bool { return evs[i].Date < evs[j].Date })
	if len(evs) > 0 {
		out["events"] = evs
	}
	if h := offers.Holiday; h != nil {
		from, ok1 := day(h.ShowFrom)
		until, ok2 := day(h.Until)
		if ok1 && ok2 && !today.Before(from) && !today.After(until) {
			out["holiday"] = h
		}
	}
	if dc := offers.Disco; dc != nil {
		from, ok1 := day(dc.ShowFrom)
		until, ok2 := day(dc.Until)
		if ok1 && ok2 && !today.Before(from) && !today.After(until) {
			cp := *dc
			cp.Parties = nil
			for _, p := range dc.Parties {
				if d, ok := day(p.Date); ok && !today.After(d) {
					p.DaysLeft = int(d.Sub(today).Hours()/24 + 0.5)
					cp.Parties = append(cp.Parties, p)
				}
			}
			if len(cp.Parties) > 0 {
				out["disco"] = cp
			}
		}
	}
	if best != nil && sw.WeeklyHidden != best.From {
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
