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
		{"2026-11-06 21:00", false, true, false},
		{"2026-11-07 09:00", false, false, false},
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
