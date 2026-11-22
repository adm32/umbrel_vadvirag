package main

import "testing"

func TestPriceTextMore(t *testing.T) {
	for _, c := range []struct {
		text string
		food int
	}{
		{"Pizza Margherita (50 cm) - 1 db", 4500},
		{"Rántott sertésszelet + hasábburgonya", 3290 + 950},
		{"Cordon bleu rizzsel", 3390 + 950},
		{"2x Smash Burger", 2 * 3590},
		{"1 gyrostál", 3790},
		{"3 db palacsinta kakaós", 3 * 1250},
		{"Brassói aprópecsenye", 3990},
		{"1 nagy kocsonya, 1 kicsi kocsonya", 1800 + 1500},
	} {
		if f, _, _, _ := priceText(c.text); f != c.food {
			t.Errorf("%q: %d want %d", c.text, f, c.food)
		}
	}
}
