package main

import "testing"

func TestPriceText(t *testing.T) {
	cases := []struct {
		text     string
		food, pk int
		exact    bool
		found    bool
	}{
		// weboldali kosár: pontos
		{"2× Pizza Margherita 50 cm – 9 000 Ft\n1× Gulyásleves csipetkével – 2 190 Ft\nCsomagolás (elviteles dobozok) – 1 100 Ft", 11190, 1100, true, true},
		// szabadon leírt
		{"Szabadon leírt rendelés:\n2 gulyásleves, 1 nagy Vadvirág pizza, 2 rántott szelet hasábbal", 2*2190 + 5000 + 2*3290 + 950, 2*200 + 450 + 2*200, false, true},
		{"1 db Bolognai spagetti", 3890, 200, false, true},
		{"Pizza Hawaii 32 cm x 2", 2 * 2650, 400, false, true},
		{"két hasábburgonya", 1900, 400, false, true},
		{"Udvarnok tál 4 fős", 23200, 550, false, true},
		{"1 Rántott csirkemáj, tartármártással", 3190, 200, false, true},
		{"Hétvégi ajánlat: Sajtkrémleves + Dijoni töltött karaj bundában (3 990 Ft)\n+ desszert: Almás kelt palacsinta (1 290 Ft)", 5280, 550, false, true},
		{"Heti menü – szerda (okt. 28.), háromfogásos (3 100 Ft)\nLeves: Burgonyakrémleves levesgyönggyel\nFőétel: B) Bolognai spagetti, reszelt sajt\nDesszert: a háromfogásos menü desszertje", 3100, 550, false, true},
		{"valami amit nem ismerünk", 0, 0, false, false},
		{"Házi kocsonya nagy", 1800, 200, false, true},
		{"2 Coca-Cola", 900, 0, false, true},
	}
	for i, c := range cases {
		f, p, ex, fd := priceText(c.text)
		if f != c.food || p != c.pk || ex != c.exact || fd != c.found {
			t.Errorf("case %d %q: food=%d pack=%d exact=%v found=%v; want %d %d %v %v", i, c.text, f, p, ex, fd, c.food, c.pk, c.exact, c.found)
		}
	}
}

func TestApplyPrice(t *testing.T) {
	o := &Order{Items: "1 db Bolognai spagetti", DeliveryFee: 1400}
	applyPrice(o)
	if o.Total != 3890+200+1400 || !o.TotalEst {
		t.Fatalf("got %+v", o)
	}
	o.Total, o.TotalManual = 5000, true
	applyPrice(o)
	if o.Total != 5000 {
		t.Fatal("manual total must stay")
	}
	u := &Order{Items: "érthetetlen", DeliveryFee: 1400}
	applyPrice(u)
	if u.Total != 0 || u.TotalEst {
		t.Fatalf("unpriced order must stay 0, got %+v", u)
	}
}
