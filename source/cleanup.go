package main

// Teszt- és saját rendelések kiszűrése a statisztikából, és a régi rendelések árának
// újraszámolása (egyszer, frissítés után).

import (
	"crypto/sha256"
	"encoding/hex"
	"log"
	"strings"
)

// ezek a szavak a névben, a megjegyzésben, az e-mail címben vagy a rendelés szövegében tesztre utalnak
var testWords = map[string]bool{"teszt": true, "test": true, "tesztrendeles": true, "proba": true, "probarendeles": true,
	"asdf": true, "qwert": true, "qwertz": true, "qwerty": true, "xxx": true, "dummy": true, "lorem": true, "ipsum": true}

func looksLikeTest(o *Order) bool {
	email := o.Email
	if i := strings.Index(email, "@"); i >= 0 {
		email = email[:i]
	}
	for _, s := range []string{o.Name, o.Note, email, o.Items} {
		for _, t := range tokens(s) {
			if testWords[t] {
				return true
			}
		}
	}
	return false
}

// ownKey: a telefonszám (utolsó 9 számjegy) állandó, visszafejthetetlen kódja.
func ownKey(phone string) string {
	var d []rune
	for _, c := range phone {
		if c >= '0' && c <= '9' {
			d = append(d, c)
		}
	}
	if len(d) < 6 {
		return ""
	}
	if len(d) > 9 {
		d = d[len(d)-9:]
	}
	h := sha256.Sum256([]byte("vadvirag-sajat|" + string(d)))
	return hex.EncodeToString(h[:8])
}

func (a *App) isOwnPhone(phone string) bool {
	k := ownKey(phone)
	if k == "" {
		return false
	}
	for _, x := range a.conf().SajatTelefonok {
		if x == k {
			return true
		}
	}
	return false
}

// autoTest: új vagy régi rendelés automatikus jelölése (a kézzel eldöntötteket nem bántja).
func (a *App) autoTest(o *Order) {
	if o.TestChecked {
		return
	}
	t := looksLikeTest(o) || a.isOwnPhone(o.Phone)
	o.Test, o.TestAuto = t, t
}

// migrateOrders: induláskor egyszer – árak újraszámolása és tesztek jelölése.
func (a *App) migrateOrders() {
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	changed := false
	if a.st.PriceVer < priceVersion {
		priced, est, none := 0, 0, 0
		for _, o := range a.st.Orders {
			applyPrice(o)
			switch {
			case o.Total == 0:
				none++
			case o.TotalEst:
				est++
			default:
				priced++
			}
		}
		a.st.PriceVer = priceVersion
		changed = true
		log.Printf("Rendelések ára újraszámolva: %d pontos, %d becsült, %d ár nélkül", priced, est, none)
	}
	tests := 0
	for _, o := range a.st.Orders {
		before := o.Test
		a.autoTest(o)
		if o.Test != before {
			changed = true
		}
		if o.Test {
			tests++
		}
	}
	if changed {
		log.Printf("Teszt / saját rendelésként jelölve (a statisztikából kimarad): %d", tests)
		a.st.saveLocked()
	}
}
