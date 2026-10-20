package main

// Elviteles dobozok (csomagolás) díja – az étterem árlistája szerint, darabonként.
//
//   leveses doboz ................................ 150 Ft  (csontleves, krémlevesek)
//   gulyásleves, halászlé doboz .................. 200 Ft
//   pizzás doboz (32 cm) ......................... 200 Ft
//   50 cm-es pizza doboz ......................... 450 Ft  (a pizzakoszorú is ebbe fér)
//   fedeles habtálca ............................. 200 Ft  (főételek, burgerek, saláták, köretek, desszertek)
//   2 személyes tálakhoz habtálca ................ 450 Ft
//   4 személyes habtálca ......................... 550 Ft
//
// A szószok (saját kis tégelyben), az üdítő és a burger-extrák (a burger dobozába kerülnek)
// nem kerülnek pénzbe.

import "strings"

const (
	packSoup     = 150
	packBigSoup  = 200
	packPizza32  = 200
	packPizza50  = 450
	packTray     = 200
	packPlatter2 = 450
	packPlatter4 = 550
)

// packFor: egy darab étel csomagolási díja.
func packFor(sectionID string, it *MenuItem, variant string) int {
	n := norm(it.Name)
	switch sectionID {
	case "pizzak":
		if strings.Contains(norm(variant), "50") || strings.Contains(n, "koszoru") {
			return packPizza50
		}
		return packPizza32
	case "levesek":
		if strings.Contains(n, "gulyas") || strings.Contains(n, "halaszle") {
			return packBigSoup
		}
		return packSoup
	case "talak":
		switch {
		case strings.Contains(n, "4 fo"):
			return packPlatter4
		case strings.Contains(n, "2 fo"):
			return packPlatter2
		}
		return packTray // pl. Erdélyi fatányéros 1 főre
	case "szoszok-es-martogatosok", "burger-extrak-es-udito":
		return 0
	}
	return packTray
}
