package main

import (
	"testing"
	"time"
)

func TestOffersVisibility(t *testing.T) {
	at := func(s string) map[string]any {
		tm, _ := time.ParseInLocation("2006-01-02 15:04", s, budapestTZ)
		return activeOffers(tm)
	}
	cases := []struct {
		when            string
		weekend, weekly bool
		upcoming        bool
	}{
		{"2026-10-22 12:00", false, false, false},
		{"2026-10-23 08:00", true, true, true},
		{"2026-10-24 18:00", true, true, true},
		{"2026-10-25 23:59", true, true, true},
		{"2026-10-26 00:01", false, true, false}, // hétfőtől nincs hétvégi ajánlat
		{"2026-10-30 14:59", false, true, false},
		{"2026-10-30 15:00", false, true, true}, // péntek 15:00-tól a jövő heti menü
		{"2026-10-31 09:00", false, true, true},
		{"2026-11-02 10:00", false, true, false},
		{"2026-11-06 14:59", false, true, false},
		{"2026-11-06 15:00", false, true, true}, // péntek 15:00-tól a nov. 9–13. heti menü
		{"2026-11-07 09:00", false, true, true},
		{"2026-11-10 10:00", false, true, false},
		{"2026-11-14 09:00", false, false, false},
	}
	for _, c := range cases {
		o := at(c.when)
		_, we := o["weekend"]
		wk, wl := o["weekly"]
		if we != c.weekend || wl != c.weekly {
			t.Errorf("%s: weekend=%v weekly=%v", c.when, we, wl)
		}
		if wl && wk.(weeklyMenu).Upcoming != c.upcoming {
			t.Errorf("%s: upcoming wrong", c.when)
		}
	}
	if o := at("2026-10-24 18:00"); o["weekend"].(weekendOffer).DaysLeft != 1 {
		t.Error("days left on Saturday should be 1")
	}
	if len(offers.Weeks) < 2 || len(offers.Weeks[1].Days) != 5 || offers.Weekend.Price != 3990 {
		t.Error("data not loaded")
	}
	if w := at("2026-10-30 15:00")["weekly"].(weeklyMenu); w.From != "2026-11-02" || w.Days[0].Dessert["hu"] == "" {
		t.Errorf("wrong week after the switch: %s", w.From)
	}
	if w := at("2026-10-30 14:00")["weekly"].(weeklyMenu); w.From != "2026-10-26" {
		t.Errorf("wrong week before the switch: %s", w.From)
	}
	for _, w := range offers.Weeks {
		for _, d := range w.Days {
			for _, x := range append(append(d.Soups, d.Mains...), d.Dessert) {
				if x == nil && d.Dessert == nil {
					continue
				}
				if x["hu"] == "" || x["en"] == "" || x["de"] == "" {
					t.Errorf("%s: missing translation %v", d.Date, x)
				}
			}
		}
	}
}

func TestSeasonsAndSwitches(t *testing.T) {
	at := func(s string) time.Time { tm, _ := time.ParseInLocation("2006-01-02 15:04", s, budapestTZ); return tm }
	for when, want := range map[string]string{
		"2026-08-31 23:59": "base", "2026-09-01 00:00": "autumn", "2026-11-30 22:00": "autumn", "2026-12-01 00:01": "winter",
		"2026-12-15 23:00": "winter", "2026-12-16 08:00": "christmas", "2026-12-23 00:00": "christmas2", "2026-12-25 23:59": "christmas2",
		"2026-12-26 10:00": "christmas", "2026-12-30 23:00": "christmas", "2026-12-31 00:00": "newyear", "2027-01-02 23:59": "newyear",
		"2027-01-03 00:00": "winter", "2027-02-28 12:00": "winter", "2027-03-01 00:00": "base",
	} {
		if got := autoSeason(at(when)); got != want {
			t.Errorf("%s: %s, want %s", when, got, want)
		}
	}
	ch := seasonChanges(at("2026-11-29 12:00"), 6)
	if len(ch) != 6 || ch[0].At != "2026-12-01" || ch[1].Key != "christmas" || ch[2].At != "2026-12-23" || ch[4].Key != "newyear" || ch[5].At != "2027-01-03" {
		t.Errorf("changes: %+v", ch)
	}
	// kézi téma: csak az adott automatikus szakaszban érvényes
	sw = Switches{Season: "winter", SeasonFor: "autumn"}
	if s, _, m := currentSeason(at("2026-11-02 12:00")); s != "winter" || !m {
		t.Error("manual season not applied")
	}
	if s, _, m := currentSeason(at("2026-12-02 12:00")); s != "winter" || m {
		t.Error("manual season should expire at the next change")
	}
	// jövő heti menü előrehozása, hétvégi ajánlat elrejtése
	sw = Switches{WeekNow: "2026-11-02"}
	if w := activeOffers(at("2026-10-27 10:00"))["weekly"].(weeklyMenu); w.From != "2026-11-02" || !w.Upcoming {
		t.Errorf("week_now: %s", w.From)
	}
	sw = Switches{WeeklyHidden: "2026-10-26"}
	if _, ok := activeOffers(at("2026-10-27 10:00"))["weekly"]; ok {
		t.Error("hidden week still shown")
	}
	sw = Switches{WeekendHidden: offers.Weekend.From}
	if _, ok := activeOffers(at("2026-10-24 10:00"))["weekend"]; ok {
		t.Error("hidden weekend still shown")
	}
	sw = Switches{}
}
