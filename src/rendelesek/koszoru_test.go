package main

import "testing"

func TestKoszoruSpelling(t *testing.T) {
	for s, want := range map[string]int{"1x pizza koszorú sonkás": 4390, "1x pizzakoszorú sonkás": 4390, "1 Pizzakoszorú – Csülkös": 4790, "2 db csirkés koszorú": 9180, "1 pizza-koszorú csirkés": 4590} {
		if f, _, _, _ := priceText(s); f != want {
			t.Errorf("%q → %d, want %d", s, f, want)
		}
	}
}
