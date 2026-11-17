package main

import "testing"

// Az új burgerek: hús (sertés/marha/csirke/vega) × kiszerelés (box/csak burger).
func TestBurgerMenu(t *testing.T) {
	it := menuIdx["burger-smash-burger"]
	if it == nil || len(it.Variants) != 4 {
		t.Fatalf("smash burger variants: %+v", it)
	}
	want := map[string]int{"Sertés · Box": 3190, "Marha · Box": 3590, "Sertés · Csak burger": 2490, "Marha · Csak burger": 2890}
	for _, v := range it.Variants {
		if want[v.Label] != v.Price || v.Pack != packTray {
			t.Errorf("%s: %d Ft, doboz %d", v.Label, v.Price, v.Pack)
		}
	}
	if c := menuIdx["burger-vadvirag-burger"]; c == nil || len(c.Variants) != 2 || c.Badge != "new" {
		t.Errorf("crispy: %+v", c)
	}
	if v := menuIdx["burger-camemburger"]; v == nil || len(v.Variants) != 6 || v.Badge != "recipe" {
		t.Errorf("camemburger: %+v", v)
	}
	if f := menuIdx["burger-fustos-sajtburger"]; f == nil || len(f.Variants) != 4 {
		t.Errorf("füstös: csak sertés/marha, vega nélkül: %+v", f)
	}
	if menuIdx["burger-vega-burger"] != nil {
		t.Error("a Vega Burger már nincs az étlapon")
	}
}

func TestBurgerFreeText(t *testing.T) {
	cases := []struct {
		text string
		food int
	}{
		{"1 Smash burger", 3590},                   // nincs megadva: marhás box, mint eddig
		{"1 smash burger sertéssel", 3190},         // sertés box
		{"smash burger sertés, csak burger", 2490}, // sertés, hasáb nélkül
		{"2 smash burger 1 farmer burger", 2*3590 + 3790},
		{"2 Konyhafőnök Dupla marha magában", 2 * 3890}, // marha, csak burger
		{"Vadvirág crispy csirkeburger", 3790},
		{"camemburger vega csak burger", 2890},
	}
	for _, c := range cases {
		f, _, _, fd := priceText(c.text)
		if f != c.food || !fd {
			t.Errorf("%q: %d Ft (found=%v), want %d", c.text, f, fd, c.food)
		}
	}
}
