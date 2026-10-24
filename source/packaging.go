package main

// Elviteles dobozok (csomagolás) díja – az étterem árlistája szerint, darabonként.
//
//   leveses doboz (0,5 l) ........................ 150 Ft  (csontleves, krémlevesek, kis kocsonya)
//   gulyásleves, halászlé doboz (0,75 l) ......... 200 Ft  (nagy kocsonya is)
//   pizzás doboz (32 cm) ......................... 200 Ft
//   50 cm-es pizza doboz ......................... 450 Ft  (a pizzakoszorú is ebbe fér)
//   fedeles habtálca ............................. 200 Ft  (főételek, burgerek, saláták, köretek, desszertek)
//   2 személyes tálakhoz habtálca ................ 450 Ft
//   4 személyes habtálca ......................... 550 Ft
//
// A szószok (saját kis tégelyben), az üdítő és a burger-extrák (a burger dobozába kerülnek)
// nem kerülnek pénzbe.
//
// Köretek: a köretre szoruló főételekkel (rántott hús, sült csirke, hal stb.) a köret ugyanabba
// a dobozba kerül, ezért főételenként egy köretnek nincs külön dobozdíja.

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
	case "kocsonya": // kis doboz = leveses doboz (0,5 l), nagy doboz = a nagyobb leveses doboz (0,75 l)
		if strings.Contains(norm(variant), "0,75") || strings.Contains(norm(variant), "nagy") {
			return packBigSoup
		}
		return packSoup
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

// Szerepek a dobozoláshoz: "main" = köretre szoruló főétel, "side" = köret.
const (
	roleMain = "main"
	roleSide = "side"
)

func roleFor(sectionID string, it *MenuItem) string {
	n := norm(it.Name)
	switch sectionID {
	case "koretek":
		return roleSide
	case "szarnyasbol-keszult-etelek", "serteshusbol-keszult-etelek", "marhahusbol-keszult-etelek", "halak":
		return roleMain
	case "tesztak-es-vega-etelek":
		if strings.Contains(n, "rantott") || strings.Contains(n, "camembert") {
			return roleMain
		}
	}
	return ""
}

// packLine: egy kosársor a dobozdíj számolásához.
type packLine struct {
	role string
	pack int // darabonkénti dobozdíj
	qty  int
}

// packTotal: a teljes dobozdíj. Minden köretre szoruló főétel dobozába egy köret belefér,
// így ennyi köretnek nem számolunk külön dobozt.
func packTotal(lines []packLine) int {
	total, mains := 0, 0
	for _, l := range lines {
		total += l.pack * l.qty
		if l.role == roleMain {
			mains += l.qty
		}
	}
	for _, l := range lines {
		if l.role != roleSide || mains == 0 {
			continue
		}
		free := min(mains, l.qty)
		total -= free * l.pack
		mains -= free
	}
	return total
}
