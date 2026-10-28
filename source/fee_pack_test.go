package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestFeeMatching(t *testing.T) {
	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, &Store{})
	cases := map[string]int{
		"Zalaudvarnok": 200, "zalaszentgrot": 600, "Sümegcsehi": 2500, "Kehida": -1,
		"Kehidakustány": 1400, "Zalaszentgrót-Csáford": 1200, "Kiscsáford": 1200, "Felsőaranyod": 900,
		"Budapest": -1, "Hévíz": -1, "Dabronc": -1, "Gógánfa": -1, "8790 Zalaszentgrót": 600, "Kisvásárhely": 2600,
		"Pókaszepetk": 3000, "Bérbaltavár": 3000, "Zalabér": 1600, "Sénye": 600,
	}
	for in, want := range cases {
		if got := a.fee(modeDelivery, in); got != want {
			t.Errorf("fee(%q)=%d, want %d", in, got, want)
		}
	}
	for in, want := range map[string]int{"Csáford": 10, "Dötk": 22, "Zalaudvarnok": 1, "Hévíz": 0, "Pókaszepetk": 25} {
		if got := a.driveMins(modeDelivery, in); got != want {
			t.Errorf("driveMins(%q)=%d, want %d", in, got, want)
		}
	}
	if a.driveMins(modePickup, "Csáford") != 0 {
		t.Error("pickup should have no drive time")
	}
	if len(defaultDriveTimes()) != len(defaultFees()) {
		t.Error("every village needs a drive time")
	}
	if n := len(defaultFees()); n != 42 {
		t.Errorf("want 42 towns, got %d", n)
	}
}

func TestPack(t *testing.T) {
	want := map[string]int{
		"pizzak-pizza-margherita|32 cm": 200, "pizzak-pizza-margherita|50 cm": 450, "pizzak-pizza-koszoru-sonkas|": 450,
		"levese-gulyasleves-csipetkevel|": 200, "levese-halaszle-harcsafilevel|": 200, "levese-fokhagymakremleves-krutonnal|": 150,
		"talak-udvarnok-tal-2-fos|": 450, "talak-vadvirag-tal-4-fos|": 550, "talak-parasztos-fatal-2-fore|": 450,
		"talak-erdelyi-fatanyeros-1-fore|": 200, "sertes-rantott-sertesszelet|": 200, "burger-smash-burger|": 200,
		"szoszo-ketchup|": 0, "burger-dobozos-coca-cola|": 0, "korete-hasabburgonya|": 200, "kocsonya-hazi-kocsonya|kis doboz (0,5 l)": 150, "kocsonya-hazi-kocsonya|nagy doboz (0,75 l)": 200,
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
	if cfg.DijakVerzio != feeTableVersion || len(cfg.Dijak) != 42 || cfg.Dijak["Türje"] != 1600 || cfg.Menetidok["Türje"] != 13 {
		t.Fatalf("not migrated: v%d %d towns", cfg.DijakVerzio, len(cfg.Dijak))
	}
	again, _ := loadConfig(p) // a mentett v2 fájl változatlan marad
	if len(again.Dijak) != 42 || again.DijakVerzio != feeTableVersion {
		t.Fatal("saved file not v2")
	}
}

func TestSideSharesBox(t *testing.T) {
	m, s, soup := packLine{roleMain, 200, 1}, packLine{roleSide, 200, 1}, packLine{"", 150, 1}
	cases := []struct {
		in   []packLine
		want int
	}{
		{[]packLine{m, s}, 200},                                   // rántott hús + hasáb: egy doboz
		{[]packLine{{roleMain, 200, 2}, {roleSide, 200, 3}}, 600}, // 2 főétel, 3 köret: 2 köret ingyen
		{[]packLine{s}, 200},                                      // csak köret: saját doboz
		{[]packLine{m, soup}, 350},
		{[]packLine{m, {roleSide, 200, 1}, {roleSide, 200, 1}}, 400},
	}
	for i, c := range cases {
		if got := packTotal(c.in); got != c.want {
			t.Errorf("case %d: %d want %d", i, got, c.want)
		}
	}
	if menuIdx["sertes-rantott-sertesszelet"].Role != roleMain || menuIdx["korete-hasabburgonya"].Role != roleSide ||
		menuIdx["teszta-bolognai-spagetti"].Role != "" || menuIdx["teszta-rantott-trappista-sajt-tartarral"].Role != roleMain {
		t.Error("roles wrong")
	}
}

func TestDailyOffer(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Budapest")
	at := func(d, h, m int) time.Time { return time.Date(2026, 10, d, h, m, 0, 0, loc) }
	wedTxt := "Napi ajánlat – szerda (okt. 28.), kétfogásos (2 400 Ft)"
	cases := []struct {
		text string
		now  time.Time
		ok   bool
	}{
		{wedTxt, at(24, 12, 0), false}, // szombat
		{wedTxt, at(28, 9, 29), false}, // még korán
		{wedTxt, at(28, 9, 30), true},  // előrendelés kezdete
		{wedTxt, at(28, 13, 59), true}, // utolsó perc
		{wedTxt, at(28, 14, 0), false}, // lejárt
		{"Napi ajánlat – csütörtök (okt. 29.), kétfogásos", at(28, 10, 0), false}, // más nap
		{"Heti menü – szerda (okt. 28.), kétfogásos", at(28, 12, 0), true},        // régi oldalról
		{"2 gulyásleves", at(24, 20, 0), true},                                    // más rendelést nem érint
		{"Hétvégi ajánlat: x", at(24, 20, 0), true},
	}
	for _, c := range cases {
		if got := dailyOfferCheck(c.text, c.now) == ""; got != c.ok {
			t.Errorf("%q @ %s: rendelhető=%v, várt %v", c.text, c.now.Format("Mon 15:04"), got, c.ok)
		}
	}

	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, &Store{})
	free := []struct {
		text, mode, city string
		now              time.Time
		want             bool
	}{
		{wedTxt, modeDelivery, "Zalaszentgrót", at(28, 10, 15), true},
		{wedTxt, modeDelivery, "8790 Zalaszentgrót, Csáford", at(28, 10, 15), false}, // Csáford drágább, az számít
		{wedTxt, modeDelivery, "zalaudvarnok", at(28, 9, 30), true},
		{wedTxt, modeDelivery, "Zalaszentlászló", at(28, 10, 59), true},
		{wedTxt, modeDelivery, "Zalaszentgrót", at(28, 11, 0), false}, // 11-től már díjas
		{wedTxt, modeDelivery, "Kehidakustány", at(28, 10, 0), false},
		{wedTxt, modePickup, "Zalaszentgrót", at(28, 10, 0), false},
		{"2 Pizza Margherita", modeDelivery, "Zalaszentgrót", at(28, 10, 0), false},
	}
	for _, c := range free {
		if got := a.dailyFreeDelivery(c.text, c.mode, c.city, c.now); got != c.want {
			t.Errorf("ingyenes? %q %s %s: %v, várt %v", c.text[:12], c.city, c.now.Format("15:04"), got, c.want)
		}
	}
	o := &Order{Mode: modeDelivery, City: "Zalaszentgrót", Items: wedTxt, FreeDelivery: true}
	a.ingest(o)
	if o.DeliveryFee != 0 || o.Food != 2400 || o.Total != o.Food+o.Pack {
		t.Errorf("ingyenes kiszállítás: díj %d, végösszeg %d", o.DeliveryFee, o.Total)
	}
}

func TestMenuOff(t *testing.T) {
	if !menuOff["kocsonya-hazi-kocsonya"] {
		t.Fatal("a kocsonya ki van kapcsolva")
	}
	for _, s := range publicMenu {
		if s.Off || s.ID == "kocsonya" {
			t.Error("kikapcsolt rész a vendég étlapján:", s.ID)
		}
	}
	if menuIdx["kocsonya-hazi-kocsonya"] == nil {
		t.Error("a régi rendelések árához meg kell maradnia")
	}
}

func TestPreorder(t *testing.T) {
	if preOff == nil || len(preOff.Mains) != 4 || len(preOff.Desserts) != 2 || len(preOff.Platters) != 1 || preOff.MainPrice != 6500 || preOff.DessertPrice != 1590 || preOff.PlatterPrice != 12990 {
		t.Fatal("az elorendeles.json adatai hibásak")
	}
	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, &Store{})
	loc, _ := time.LoadLocation("Europe/Budapest")
	now := time.Date(2026, 10, 28, 13, 0, 0, 0, loc)
	okEarly := preorderReq{Items: map[string]int{"m0": 1}, Mode: preTakeaway, Date: "2026-11-11", Time: "09:30", Name: "A", Phone: "06301234567"}
	if _, msg := a.buildPreorder(okEarly, now); msg != "" {
		t.Error("9:30-ra lehet előrendelni:", msg)
	}
	base := func() preorderReq {
		return preorderReq{Items: map[string]int{"m0": 2, "d1": 1}, Mode: preDineIn, Date: "2026-11-11", Time: "18:00", Guests: 2, Name: "Kiss Anna", Phone: "06 30 123 4567"}
	}
	// helyben: csak az ételek ára
	p, msg := a.buildPreorder(base(), now)
	if msg != "" || p.Food != 2*6500+1590 || p.Pack != 0 || p.Fee != 0 || p.Total != p.Food || len(p.Lines) != 2 {
		t.Fatalf("helyben: %q %+v", msg, p)
	}
	// kiszállítás: doboz adagonként + a település díja
	in := base()
	in.Mode, in.City, in.Street = preDelivery, "Zalaszentgrót", "Fő u. 1."
	p, msg = a.buildPreorder(in, now)
	if msg != "" || p.Pack != 3*packTray || p.Fee != 600 || p.Total != 2*6500+1590+3*packTray+600 || p.DriveMin == 0 {
		t.Fatalf("kiszállítás: %q %+v", msg, p)
	}
	// tál elvitelre: 2 személyes habtálca
	in = base()
	in.Items, in.Mode = map[string]int{"t0": 1}, preTakeaway
	if p, msg = a.buildPreorder(in, now); msg != "" || p.Food != 12990 || p.Pack != packPlatter2 || p.Total != 12990+packPlatter2 {
		t.Fatalf("tál: %q %+v", msg, p)
	}
	// elvitel: doboz, szállítás nélkül
	in = base()
	in.Mode = preTakeaway
	if p, msg = a.buildPreorder(in, now); msg != "" || p.Fee != 0 || p.Pack != 3*packTray {
		t.Fatalf("elvitel: %q %+v", msg, p)
	}
	bad := []func(*preorderReq){
		func(r *preorderReq) { r.Date = "2026-11-09" },              // a kezdés előtt
		func(r *preorderReq) { r.Items = map[string]int{} },         // semmi
		func(r *preorderReq) { r.Items = map[string]int{"m0": 51} }, // túl sok
		func(r *preorderReq) { r.Time = "22:30" },
		func(r *preorderReq) { r.Time = "09:15" },                                        // zárás után
		func(r *preorderReq) { r.Phone = "" },                                            // nincs telefon
		func(r *preorderReq) { r.Guests = 0 },                                            // helyben létszám kell
		func(r *preorderReq) { r.Mode = "x" },                                            // ismeretlen mód
		func(r *preorderReq) { r.Mode, r.City, r.Street = preDelivery, "Budapest", "x" }, // nem szállítunk oda
		func(r *preorderReq) { r.Mode, r.City = preDelivery, "Zalaszentgrót" },           // nincs cím
	}
	for i, f := range bad {
		in := base()
		f(&in)
		if _, msg := a.buildPreorder(in, now); msg == "" {
			t.Errorf("%d. hibás kérést elfogadott", i)
		}
	}
	// elfogyott: semmit nem fogad el
	a.cfg.Elfogyott = map[string]bool{preOff.ID: true}
	if _, msg := a.buildPreorder(base(), now); msg == "" {
		t.Error("elfogyott ajánlatra nem szabad előrendelést fogadni")
	}
}
