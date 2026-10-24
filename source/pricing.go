package main

// Rendelések ára.
//
// A weboldal kosarából érkező rendelésnél a szerver pontosan tudja az árat. A régi Wix-űrlapos,
// a kézzel felvett és a „szabadon leírt” rendeléseknél viszont csak szöveg van – ezeknél az
// étlap alapján megbecsüljük az ételek és a dobozok árát (TotalEst = true). A személyzet a
// pontos összeget bármikor megadhatja (TotalManual = true), azt már nem írjuk felül.
//
// Minden rendelésnél külön tároljuk: Food (ételek), Pack (dobozok), DeliveryFee (szállítás),
// és Total = Food + Pack + szállítás. Így az irányítópult minden számot ugyanabból számol.

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode"
)

const priceVersion = 1 // ha a becslés javul, emeld: induláskor minden régi rendelés újraszámolódik

var (
	explicitPriceRe = regexp.MustCompile(`(\d{1,3}(?:[ .\x{00a0}\x{202f}]?\d{3})*)\s*Ft`)
	cartLineRe      = regexp.MustCompile(`^\s*\d+\s*×\s.+–\s*[\d\s\x{00a0}\x{202f}]+Ft\s*$`)
	leadQtyRe       = regexp.MustCompile(`^\s*(\d{1,2})\s*(?:x|×|db|darab|adag|porcio|porció|-?szor)?\.?\s+`)
	tailQtyRe       = regexp.MustCompile(`(?:\s|^)(?:x\s*(\d{1,2})|(\d{1,2})\s*(?:x|×|db|darab|adag))\s*$`)
	numWords        = map[string]int{"egy": 1, "ket": 2, "ketto": 2, "harom": 3, "negy": 4, "ot": 5, "hat": 6, "het": 7, "nyolc": 8}
	stopWords       = map[string]bool{"a": true, "az": true, "es": true, "is": true, "meg": true, "db": true, "darab": true, "adag": true,
		"kerek": true, "kerem": true, "kernek": true, "szeretnek": true, "plusz": true, "sok": true, "nagy": true, "kis": true,
		"kicsi": true, "cm": true, "x": true, "ft": true, "val": true, "vel": true}
	personsRe = regexp.MustCompile(`^\s*\d{1,2}\s*(?:fo|fő|fos|fős|fore|főre|szem)`)
)

type menuRef struct {
	sec, id, name string
	it            *MenuItem
	toks          []string
}

var (
	menuRefs     []menuRef
	menuRefsOnce sync.Once
)

// refs: az étlap kereshető változata (az étlap betöltése után, első használatkor épül fel).
func refs() []menuRef {
	menuRefsOnce.Do(func() {
		for si := range menuSections {
			for ii := range menuSections[si].Items {
				it := &menuSections[si].Items[ii]
				menuRefs = append(menuRefs, menuRef{menuSections[si].ID, it.ID, it.Name, it, sigTokens(it.Name)})
			}
		}
	})
	return menuRefs
}

// gyakori köznyelvi alakok → az étlapon szereplő szó
var synonyms = map[string]string{"rizzsel": "rizs", "rizzsal": "rizs", "rizses": "rizs", "krumpli": "hasabburgonya",
	"krumplival": "hasabburgonya", "sultkrumpli": "hasabburgonya", "kola": "cola", "kolat": "cola", "coca": "coca"}

func tokens(s string) []string {
	s = norm(s)
	t := strings.FieldsFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for i, x := range t {
		if y, ok := synonyms[x]; ok {
			t[i] = y
		}
	}
	return t
}

func sigTokens(s string) []string {
	var out []string
	for _, t := range tokens(s) {
		if len(t) >= 3 && !stopWords[t] {
			out = append(out, t)
		}
	}
	return out
}

// tokMatch: a szövegben lévő szó (seg) megfelel-e az étel nevében lévő szónak (item).
// Ragok és összetett szavak miatt elég a közös 5 betűs eleje, vagy ha az egyik tartalmazza a másikat.
func tokMatch(seg, item string) bool {
	if seg == item {
		return true
	}
	if len(seg) >= 5 && len(item) >= 5 && seg[:5] == item[:5] {
		return true
	}
	if len(seg) >= 5 && strings.Contains(item, seg) {
		return true
	}
	if len(item) >= 5 && strings.Contains(seg, item) {
		return true
	}
	return false
}

type estLine struct {
	ref     *menuRef
	variant string
	qty     int
	price   int // darabár
	pack    int
}

// bestMatch: a legjobban illő étel a szövegrészhez; a felhasznált szavakat visszaadja.
func bestMatch(seg []string, raw string) (*menuRef, []bool) {
	var best *menuRef
	var bestUsed []bool
	bestScore := 0.0
	rs := refs()
	for i := range rs {
		m := &rs[i]
		if len(m.toks) == 0 {
			continue
		}
		used := make([]bool, len(seg))
		hit, head := 0, false
		for k, it := range m.toks {
			for j, st := range seg {
				if !used[j] && tokMatch(st, it) {
					used[j] = true
					hit++
					head = head || (k == 0 && len(it) >= 6)
					break
				}
			}
		}
		if hit == 0 {
			continue
		}
		frac := float64(hit) / float64(len(m.toks))
		if frac < 0.5 && hit < 2 && !head { // a fő szó (pl. „gyrostál”) egyezése is elég
			continue
		}
		score := float64(hit) + frac           // több egyező szó, majd nagyobb arány nyer
		for _, d := range []string{"2", "4"} { // „4 fős tál” ↔ „Udvarnok tál (4 fős)”
			if strings.Contains(m.name, d+" f") && strings.Contains(raw, d) {
				score += 0.5
			}
		}
		if score > bestScore {
			best, bestScore, bestUsed = m, score, used
		}
	}
	return best, bestUsed
}

func pickVariant(it *MenuItem, segText string) (string, int) {
	if len(it.Variants) == 0 {
		return "", it.Price
	}
	n := norm(segText)
	big := strings.Contains(n, "50") || strings.Contains(n, "nagy") || strings.Contains(n, "csaladi") || strings.Contains(n, "0,75") || strings.Contains(n, "0.75")
	for _, v := range it.Variants {
		vl := norm(v.Label)
		if big && (strings.Contains(vl, "50") || strings.Contains(vl, "nagy")) {
			return v.Label, v.Price
		}
	}
	return it.Variants[0].Label, it.Variants[0].Price
}

func parseQty(seg string) (int, string) {
	if !personsRe.MatchString(seg) { // „4 fős tál”: ez nem mennyiség
		if m := leadQtyRe.FindStringSubmatch(seg); m != nil {
			q, _ := strconv.Atoi(m[1])
			return max(q, 1), seg[len(m[0]):]
		}
	}
	if m := tailQtyRe.FindStringSubmatchIndex(seg); m != nil {
		var g string
		if m[2] >= 0 {
			g = seg[m[2]:m[3]]
		} else {
			g = seg[m[4]:m[5]]
		}
		q, _ := strconv.Atoi(g)
		return max(q, 1), seg[:m[0]]
	}
	if t := strings.Fields(strings.TrimSpace(seg)); len(t) > 1 {
		if q, ok := numWords[norm(t[0])]; ok {
			return q, strings.Join(t[1:], " ")
		}
	}
	return 1, seg
}

// splitSegments: egy sor több ételre bontása. Vesszőnél / „és”-nél / „+”-nál csak akkor
// vágunk, ha utána mennyiség jön – így a „Rántott csirkemáj, tartármártással” egyben marad.
func splitSegments(line string) []string {
	parts := regexp.MustCompile(`\s*(?:,|;|\+|\bés\b|\bes\b)\s*`).Split(line, -1)
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		q, _ := parseQty(p)
		_, isNum := numWords[firstTok(p)]
		if len(out) > 0 && q == 1 && !leadQtyRe.MatchString(p) && !isNum {
			// nincs mennyiség: ugyanannak az ételnek a leírása (vagy egy második étel – azt a
			// bestMatch ismétlése úgyis megtalálja)
			out[len(out)-1] += ", " + p
			continue
		}
		out = append(out, p)
	}
	return out
}

func firstTok(s string) string {
	t := tokens(s)
	if len(t) == 0 {
		return ""
	}
	return t[0]
}

func parsePrice(s string) int {
	d := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
	n, _ := strconv.Atoi(d)
	return n
}

// priceText: az ételek és a dobozok ára a rendelés szövegéből.
// exact = minden sor pontos árat tartalmazott (weboldali kosár); found = talált-e valamit.
func priceText(items string) (food, pack int, exact, found bool) {
	exact = true
	var packs []packLine
	skipMenuLines := false
	for _, raw := range strings.Split(items, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, quickPrefix) {
			continue
		}
		n := norm(line)
		// weboldali kosár: „2× Pizza – 4 600 Ft” és „Csomagolás (elviteles dobozok) – 600 Ft”
		if strings.HasPrefix(n, "csomagolas") && strings.Contains(line, "Ft") {
			if m := explicitPriceRe.FindAllStringSubmatch(line, -1); m != nil {
				pack += parsePrice(m[len(m)-1][1])
				found = true
			}
			continue
		}
		if cartLineRe.MatchString(line) {
			m := explicitPriceRe.FindAllStringSubmatch(line, -1)
			food += parsePrice(m[len(m)-1][1])
			found = true
			continue
		}
		exact = false
		// heti menü / hétvégi ajánlat: az ár zárójelben áll; a Leves/Főétel/Desszert sorok csak részletek
		if skipMenuLines && (strings.HasPrefix(n, "leves:") || strings.HasPrefix(n, "foetel:") || strings.HasPrefix(n, "desszert:")) {
			continue
		}
		if m := explicitPriceRe.FindAllStringSubmatch(line, -1); m != nil && (strings.Contains(n, "menu") || strings.Contains(n, "ajanlat") || strings.HasPrefix(n, "+ desszert") || strings.HasPrefix(n, "desszert")) {
			p := parsePrice(m[len(m)-1][1])
			food += p
			found = true
			switch {
			case strings.HasPrefix(n, "heti menu"):
				skipMenuLines = true
				packs = append(packs, packLine{"", packSoup, 1}, packLine{"", packTray, 1})
				if strings.Contains(n, "haromfogasos") {
					packs = append(packs, packLine{"", packTray, 1})
				}
			case strings.Contains(n, "ajanlat"):
				packs = append(packs, packLine{"", packSoup, 1}, packLine{"", packTray, 1})
			default: // desszert
				packs = append(packs, packLine{"", packTray, 1})
			}
			continue
		}
		for _, seg := range splitSegments(line) {
			qty, rest := parseQty(seg)
			toks := tokens(rest)
			for round := 0; round < 4 && len(toks) > 0; round++ {
				ref, used := bestMatch(toks, rest)
				if ref == nil {
					break
				}
				label, price := pickVariant(ref.it, rest)
				food += price * qty
				pk := packFor(ref.sec, ref.it, label)
				packs = append(packs, packLine{roleFor(ref.sec, ref.it), pk, qty})
				found = true
				var left []string
				for j, t := range toks {
					if !used[j] {
						left = append(left, t)
					}
				}
				toks = sigTokensOf(left)
				qty = 1
			}
		}
	}
	pack += packTotal(packs)
	return food, pack, exact && found, found
}

func sigTokensOf(t []string) []string {
	var out []string
	for _, x := range t {
		if len(x) >= 3 && !stopWords[x] {
			out = append(out, x)
		}
	}
	return out
}

// applyPrice: a rendelés árának (újra)számolása. A kézzel megadott összeget nem bántja.
func applyPrice(o *Order) {
	if o.TotalManual {
		return
	}
	food, pack, exact, found := priceText(o.Items)
	o.Food, o.Pack = food, pack
	o.TotalEst = found && !exact
	o.Total = 0
	if found {
		o.Total = food + pack + max(o.DeliveryFee, 0)
	}
}
