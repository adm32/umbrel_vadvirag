package main

// Szállítási díjak, menetidők és a cím → település párosítás.

import "strings"

// Szállítási díjak és menetidők: az étterem saját táblázata (2026. október 22., kézzel javított
// díjakkal, 42 település). A km és a perc valódi autós adat Zalaudvarnokról; Pókaszepetk és
// Bérbaltavár menetideje becsült. Csak ezekre a településekre szállítunk.
// Ha a táblát módosítod, emeld a feeTableVersion értékét: induláskor a régi tábla lecserélődik.
const feeTableVersion = 8

func defaultFees() map[string]int {
	return map[string]int{
		"Zalaudvarnok":    200,  // 1.1 km
		"Zalaszentlászló": 400,  // 3.2 km
		"Sénye":           600,  // 3.6 km
		"Zalakoppány":     800,  // 4.8 km
		"Gyülevész":       600,  // 5 km
		"Zalaszentgrót":   600,  // 5.2 km
		"Kisszentgrót":    600,  // 5.5 km
		"Aranyod":         900,  // 6.5 km
		"Barátsziget":     1300, // 6.8 km
		"Tüskeszentpéter": 1000, // 8.3 km
		"Zalaköveskút":    1400, // 8.3 km
		"Csáford":         1200, // 8.5 km
		"Kallósd":         1400, // 8.5 km
		"Kisgörbő":        1400, // 9.5 km
		"Kehidakustány":   1400, // 9.8 km
		"Tekenye":         1400, // 9.9 km
		"Türje":           1600, // 11.3 km
		"Vindornyafok":    1800, // 11.4 km
		"Nemesbük":        1800, // 11.5 km
		"Nagygörbő":       1600, // 11.7 km
		"Zalabér":         1600, // 11.9 km
		"Óhíd":            1800, // 11.9 km
		"Döbröce":         1800, // 12.8 km
		"Karmacs":         1900, // 12.9 km
		"Vindornyaszőlős": 1800, // 13.2 km
		"Zalacsány":       2000, // 13.7 km
		"Batyk":           1900, // 14.1 km
		"Szalapa":         2300, // 14.5 km
		"Mihályfa":        2400, // 15.1 km
		"Sümegcsehi":      2500, // 15.2 km
		"Felsőpáhok":      2500, // 16.2 km
		"Bókaháza":        2500, // 16.3 km
		"Pakod":           2600, // 16.4 km
		"Zalavég":         2400, // 16.6 km
		"Szentgyörgyvár":  2500, // 16.7 km
		"Ligetfalva":      2500, // 17 km
		"Vindornyalak":    2500, // 17 km
		"Tilaj":           2600, // 18 km
		"Dötk":            2600, // 18.1 km
		"Kisvásárhely":    2600, // 18.1 km
		"Bérbaltavár":     3000, // 19 km (becsült)
		"Pókaszepetk":     3000, // 21 km (becsült)
	}
}

// defaultDriveTimes: autós menetidő percben az étteremtől (a futárnak).
func defaultDriveTimes() map[string]int {
	return map[string]int{
		"Zalaudvarnok":    1,
		"Zalaszentlászló": 4,
		"Sénye":           5,
		"Zalakoppány":     9,
		"Gyülevész":       6,
		"Zalaszentgrót":   6,
		"Kisszentgrót":    7,
		"Aranyod":         8,
		"Barátsziget":     11,
		"Tüskeszentpéter": 12,
		"Zalaköveskút":    9,
		"Csáford":         10,
		"Kallósd":         14,
		"Kisgörbő":        10,
		"Kehidakustány":   11,
		"Tekenye":         13,
		"Türje":           13,
		"Vindornyafok":    12,
		"Nemesbük":        13,
		"Nagygörbő":       12,
		"Zalabér":         14,
		"Óhíd":            12,
		"Döbröce":         14,
		"Karmacs":         13,
		"Vindornyaszőlős": 13,
		"Zalacsány":       14,
		"Batyk":           17,
		"Szalapa":         17,
		"Mihályfa":        18,
		"Sümegcsehi":      16,
		"Felsőpáhok":      17,
		"Bókaháza":        18,
		"Pakod":           19,
		"Zalavég":         20,
		"Szentgyörgyvár":  17,
		"Ligetfalva":      18,
		"Vindornyalak":    17,
		"Tilaj":           20,
		"Dötk":            22,
		"Kisvásárhely":    21,
		"Bérbaltavár":     23, // becsült
		"Pókaszepetk":     25, // becsült
	}
}

type feeEntry struct {
	norm string
	name string
	fee  int
	mins int // autós menetidő percben (0 = nem ismert)
}

// match: melyik szállítási településre vonatkozik a megadott cím (nil = nincs a listán).
// A leghosszabb nevek jönnek először, és a megtalált nevet kitöröljük, hogy egy rövidebb
// név ne találjon bele (Sümegcsehi ≠ Csehi). Ha több település is szerepel
// („Zalaszentgrót-Csáford”), a messzebbi (drágább) számít.
func (a *App) match(city string) *feeEntry {
	c := norm(city)
	var best *feeEntry
	for i := range a.feeIx {
		f := &a.feeIx[i]
		if strings.Contains(c, f.norm) {
			c = strings.ReplaceAll(c, f.norm, "|")
			if best == nil || f.fee > best.fee {
				best = f
			}
		}
	}
	return best
}

func (a *App) fee(mode, city string) int {
	if mode != modeDelivery {
		return 0
	}
	if f := a.match(city); f != nil {
		return f.fee
	}
	return -1
}

// driveMins: hány perc az út autóval a településig (0 = nem tudjuk).
func (a *App) driveMins(mode, city string) int {
	if mode != modeDelivery {
		return 0
	}
	if f := a.match(city); f != nil {
		return f.mins
	}
	return 0
}
