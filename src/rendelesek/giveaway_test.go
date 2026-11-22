package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGiveawayEntries(t *testing.T) {
	useTestGiveaway(t)
	at := func(s string) time.Time { return bpTime(s) }
	st := &Store{}
	st.Orders = []*Order{
		{ID: 1, Source: "web", Name: "Kiss Anna", Phone: "06 30 123 4567", Created: at("2026-10-30 12:00"), Food: 5700, Pack: 300, DeliveryFee: 900, Total: 6900, Status: StDone}, // pont 6000 → igen
		{ID: 2, Source: "web", Name: "Kiss Anna", Phone: "+36301234567", Created: at("2026-10-31 12:00"), Food: 9000, Status: StDone},                                             // ugyanaz a szám → egyszer
		{ID: 3, Source: "web", Name: "Nagy Béla", Phone: "06201112222", Created: at("2026-10-30 12:00"), Food: 5500, Pack: 499, DeliveryFee: 1500, Total: 7499, Status: StDone},   // 5999 → nem
		{ID: 4, Source: "web", Name: "Tóth Cili", Phone: "06701113333", Created: at("2026-11-01 20:59"), Total: 7500, DeliveryFee: 1000, TotalEst: true, Status: StNew},           // 6500 → igen
		{ID: 5, Source: "web", Name: "Késő Kata", Phone: "06701114444", Created: at("2026-11-01 21:00"), Food: 9000, Status: StDone},                                              // lejárt
		{ID: 6, Source: "manual", Name: "Telefonos", Phone: "06701115555", Created: at("2026-10-30 12:00"), Food: 9000, Status: StDone},                                           // nem weboldalas
		{ID: 7, Source: "web", Name: "Elutasított", Phone: "06701116666", Created: at("2026-10-30 12:00"), Food: 9000, Status: StRejected},                                        // elutasítva
		{ID: 8, Source: "web", Name: "Teszt", Phone: "06701117777", Created: at("2026-10-30 12:00"), Food: 9000, Status: StDone, Test: true},                                      // teszt
		{ID: 9, Source: "web", Name: "Korai", Phone: "06701118888", Created: at("2026-10-28 23:59"), Food: 9000, Status: StDone},                                                  // még nem indult
	}
	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, st)
	a.dataDir = t.TempDir()
	es := a.giveawayEntries(giveaway)
	got := map[string]bool{}
	for _, e := range es {
		got[e.Ref] = true
	}
	if len(es) != 3 || !got["#1"] || !got["#3"] || !got["#4"] { // bármekkora összeg számít
		t.Fatalf("entries: %+v", es)
	}
	if es[0].Line != "Kiss Anna – 06 30 123 4567 – #1" {
		t.Errorf("line: %q", es[0].Line)
	}
	if n := len(cleanNames(es[0].Line + "\n" + es[1].Line)); n != 2 {
		t.Errorf("raffle cleaner kept %d", n)
	}
	if p := publicGiveaway(at("2026-10-30 12:00")); p["active"] != true || p["running"] != true {
		t.Error("should be running")
	}
	if p := publicGiveaway(at("2026-10-28 22:00")); p["active"] != false {
		t.Error("should not show before Oct 30")
	}
	if p := publicGiveaway(at("2026-11-01 21:00")); p["active"] != true || p["running"] != false {
		t.Error("should be shown as ended after the draw")
	}
	if p := publicGiveaway(at("2026-11-03 10:00")); p["active"] != false {
		t.Error("should be hidden after draw day")
	}
}

func TestGiveawayMail(t *testing.T) {
	useTestGiveaway(t)
	b := giveawayMailBody(giveaway, "Kiss Anna", "06 20 339 5099")
	for _, want := range []string{"Kedves Kiss Anna!", "nyertél egy szabadon választott pizzakoszorút (sonkás, csirkés vagy csülkös).", "06 20 339 5099"} {
		if !strings.Contains(b, want) {
			t.Errorf("mail body missing %q:\n%s", want, b)
		}
	}
}

// useTestGiveaway: a kiszállított nyeremenyjatek.json lehet kikapcsolva (üres id), ezért a tesztek
// a benne lévő beállításokat egy teszt-azonosítóval töltik be.
func useTestGiveaway(t *testing.T) {
	t.Helper()
	var g Giveaway
	if err := json.Unmarshal(giveawayJSON, &g); err != nil {
		t.Fatal(err)
	}
	g.ID = "pizzakoszoru-2026-11"
	g.Title = map[string]string{"hu": "Teszt"}
	g.Prize = map[string]string{"hu": "egy szabadon választott pizzakoszorút"}
	g.PrizeNote = "sonkás, csirkés vagy csülkös"
	g.Winners, g.Min = 2, 0
	g.From, g.Until, g.Draw, g.ShowUntil = "2026-10-29 00:00", "2026-11-01 21:00", "2026-11-01 21:00", "2026-11-02 23:59"
	old := giveaway
	giveaway = &g
	t.Cleanup(func() { giveaway = old })
}
