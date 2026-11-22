package main

import (
	"strings"
	"testing"
)

func TestPromo(t *testing.T) {
	old := promo
	t.Cleanup(func() { promo = old })
	promo = &Promo{ID: "teszt", From: "2026-11-02", To: "2026-11-06", Section: "pizzak", Variant: "50 cm", Gift: "1 l",
		Choices: []string{"Coca-Cola", "Fanta"}, Title: map[string]string{"hu": "Családi pizza + ajándék üdítő"}}

	for s, want := range map[string]bool{"2026-11-01 23:59": false, "2026-11-02 00:00": true, "2026-11-06 23:59": true, "2026-11-07 00:00": false} {
		if got := promo.on(bpTime(s)); got != want {
			t.Errorf("on(%s) = %v", s, got)
		}
		if got := publicPromo(bpTime(s))["active"]; got != want {
			t.Errorf("publicPromo(%s) = %v", s, got)
		}
	}

	now := bpTime("2026-11-03 18:00")
	items := []publicLine{
		{ID: "pizzak-pizza-margherita", Variant: "50 cm", Qty: 2},
		{ID: "pizzak-pizza-salami", Variant: "32 cm", Qty: 1}, // kis pizza: nem jár
		{ID: "pizzak-pizza-koszoru-sonkas", Qty: 1},           // pizzakoszorú: nem jár
		{ID: "pizzak-pizza-hawaii", Variant: "50 cm", Qty: 1},
	}
	got := promoLines(items, []string{"Fanta", "hack", "Fanta"}, now)
	want := []string{"1× Ajándék 1 l Coca-Cola – 0 Ft", "2× Ajándék 1 l Fanta – 0 Ft"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines: %q", got)
	}
	if l := promoLines(items, nil, now); len(l) != 1 || l[0] != "3× Ajándék 1 l Coca-Cola – 0 Ft" {
		t.Errorf("default pick: %q", l)
	}
	if l := promoLines(items[1:3], nil, now); l != nil {
		t.Errorf("no family pizza, still got %q", l)
	}
	if l := promoLines(items, nil, bpTime("2026-11-07 12:00")); l != nil {
		t.Errorf("after the promo, still got %q", l)
	}

	// a 0 Ft-os ajándék sor nem rontja el az árat, és pontos marad
	food, _, exact, _ := priceText("2× Pizza Margherita 50 cm – 9 000 Ft\n1× Ajándék 1 l Fanta – 0 Ft\n1× Ajándék 1 l Coca-Cola – 0 Ft")
	if food != 9000 || !exact {
		t.Errorf("price: %d exact=%v", food, exact)
	}

	st := &Store{}
	st.Orders = []*Order{
		{ID: 1, Created: now, Items: "2× Pizza Margherita 50 cm – 9 000 Ft\n1× Ajándék 1 l Coca-Cola – 0 Ft\n1× Ajándék 1 l Fanta – 0 Ft", Status: StDone},
		{ID: 2, Created: now, Items: "3× Ajándék 1 l Fanta – 0 Ft", Status: StRejected},
		{ID: 3, Created: bpTime("2026-11-08 12:00"), Items: "1× Ajándék 1 l Fanta – 0 Ft", Status: StDone},
	}
	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, st)
	a.dataDir = t.TempDir()
	if g := a.promoGiven(promo); g["Coca-Cola"] != 1 || g["Fanta"] != 1 {
		t.Errorf("given: %v", g)
	}
}
