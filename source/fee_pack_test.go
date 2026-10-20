package main

import (
	"os"
	"strings"
	"testing"
)

func TestFeeMatching(t *testing.T) {
	a := newApp(Config{Dijak: defaultFees()}, &Store{})
	cases := map[string]int{
		"Zalaudvarnok": 300, "zalaszentgrot": 800, "Sümegcsehi": 2600, "Csehi": 2700,
		"Csehimindszent": 2800, "Nagytilaj": 2300, "Tilaj": 1000, "Kehidakustány": 1100,
		"Zalaszentgrót-Csáford": 1000, "Kiscsáford": 1000, "Felsőaranyod": 900, "Budapest": -1,
		"8790 Zalaszentgrót": 800, "Hévíz": 2400,
	}
	for in, want := range cases {
		if got := a.fee(modeDelivery, in); got != want {
			t.Errorf("fee(%q)=%d, want %d", in, got, want)
		}
	}
	if n := len(defaultFees()); n != 82 {
		t.Errorf("want 82 towns, got %d", n)
	}
}

func TestPack(t *testing.T) {
	want := map[string]int{
		"pizzak-pizza-margherita|32 cm": 200, "pizzak-pizza-margherita|50 cm": 450, "pizzak-pizza-koszoru-sonkas|": 450,
		"levese-gulyasleves-csipetkevel|": 200, "levese-halaszle-harcsafilevel|": 200, "levese-fokhagymakremleves-krutonnal|": 150,
		"talak-udvarnok-tal-2-fos|": 450, "talak-vadvirag-tal-4-fos|": 550, "talak-parasztos-fatal-2-fore|": 450,
		"talak-erdelyi-fatanyeros-1-fore|": 200, "sertes-rantott-sertesszelet|": 200, "burger-smash-burger|": 200,
		"szoszo-ketchup|": 0, "burger-dobozos-coca-cola|": 0, "korete-hasabburgonya|": 200,
	}
	for k, w := range want {
		id, v, _ := strings.Cut(k, "|")
		it := menuIdx[id]
		if it == nil {
			t.Fatalf("no item %s", id)
		}
		got := it.Pack
		for _, x := range it.Variants {
			if x.Label == v {
				got = x.Pack
			}
		}
		if got != w {
			t.Errorf("%s: %d want %d", k, got, w)
		}
	}
}

func TestFeeMigration(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/beallitasok.json"
	old := `{"port":4747,"szallitasi_dijak":{"Zalaudvarnok":200,"Pókaszepetk":3000}}`
	if err := os.WriteFile(p, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DijakVerzio != feeTableVersion || len(cfg.Dijak) != 82 || cfg.Dijak["Pókaszepetk"] != 2300 {
		t.Fatalf("not migrated: v%d %d towns", cfg.DijakVerzio, len(cfg.Dijak))
	}
	again, _ := loadConfig(p) // a mentett v2 fájl változatlan marad
	if len(again.Dijak) != 82 || again.DijakVerzio != feeTableVersion {
		t.Fatal("saved file not v2")
	}
}
