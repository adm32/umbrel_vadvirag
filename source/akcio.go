package main

// Heti akció (akcio.json): a megadott napokon egy étlaprész adott méretű ételei mellé ajándék jár
// (most: minden családi, 50 cm-es pizza mellé 1 l Coca-Cola vagy Fanta). A weboldalas rendelésnél a
// vendég a kosárban választ, a rendelésben pedig külön, 0 Ft-os sorban látszik, így a konyha is tudja.

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

//go:embed akcio.json
var promoJSON []byte

type Promo struct {
	ID      string            `json:"id"`
	From    string            `json:"from"`
	To      string            `json:"to"`
	Section string            `json:"section"`
	Variant string            `json:"variant"`
	Gift    string            `json:"gift"`
	Choices []string          `json:"choices"`
	Title   map[string]string `json:"title"`
	Text    map[string]string `json:"text"`
	Days    map[string]string `json:"days"`
}

var promo *Promo

func init() {
	var p Promo
	if err := json.Unmarshal(promoJSON, &p); err != nil {
		log.Printf("akcio.json hibás: %v", err)
		return
	}
	if p.ID != "" && len(p.Choices) > 0 {
		promo = &p
	}
}

// on: fut-e az akció ebben a pillanatban (a „to” nap végéig).
func (p *Promo) on(now time.Time) bool {
	if p == nil {
		return false
	}
	from, to := bpTime(p.From), bpTime(p.To)
	return !from.IsZero() && !to.IsZero() && !now.Before(from) && now.Before(to.AddDate(0, 0, 1))
}

// eligible: ez a tétel (étel + méret) jogosít-e ajándékra.
func (p *Promo) eligible(id, variant string) bool {
	it := menuIdx[id]
	if it == nil || variant != p.Variant {
		return false
	}
	for _, s := range menuSections {
		if s.ID != p.Section {
			continue
		}
		for _, x := range s.Items {
			if x.ID == id {
				return true
			}
		}
	}
	return false
}

func (a *App) handlePublicPromo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, 200, publicPromo(time.Now()))
}

func publicPromo(now time.Time) map[string]any {
	p := promo
	if !p.on(now) {
		return map[string]any{"active": false}
	}
	return map[string]any{"active": true, "id": p.ID, "from": p.From, "to": p.To, "section": p.Section, "variant": p.Variant,
		"gift": p.Gift, "choices": p.Choices, "title": p.Title, "text": p.Text, "days": p.Days}
}

// promoGiven: a weboldalas rendelésekben eddig kiadott ajándékok száma (választásonként).
func (a *App) promoGiven(p *Promo) map[string]int {
	out := map[string]int{}
	from, to := bpTime(p.From), bpTime(p.To).AddDate(0, 0, 1)
	if a.st == nil {
		return out
	}
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	for _, o := range a.st.Orders {
		if o.Test || o.Status == StRejected || o.Created.Before(from) || !o.Created.Before(to) {
			continue
		}
		for _, l := range strings.Split(o.Items, "\n") {
			var n int
			for _, c := range p.Choices {
				if _, err := fmt.Sscanf(l, "%d× Ajándék "+p.Gift+" "+c+" – 0 Ft", &n); err == nil && strings.HasSuffix(strings.TrimSpace(l), c+" – 0 Ft") {
					out[c] += n
				}
			}
		}
	}
	return out
}

// promoLines: az ajándék sorai a rendeléshez („2× Ajándék 1 l Coca-Cola – 0 Ft”).
// A vendég választásai sorban a jogosult pizzákhoz tartoznak; ami hiányzik vagy ismeretlen, az az első választás.
func promoLines(items []publicLine, picks []string, now time.Time) []string {
	p := promo
	if !p.on(now) {
		return nil
	}
	n := 0
	for _, l := range items {
		if p.eligible(l.ID, l.Variant) && l.Qty > 0 {
			n += l.Qty
		}
	}
	if n == 0 {
		return nil
	}
	count := map[string]int{}
	for i := 0; i < n; i++ {
		c := p.Choices[0]
		if i < len(picks) {
			for _, x := range p.Choices {
				if picks[i] == x {
					c = x
				}
			}
		}
		count[c]++
	}
	var out []string
	for _, c := range p.Choices {
		if count[c] > 0 {
			out = append(out, fmt.Sprintf("%d× Ajándék %s %s – 0 Ft", count[c], p.Gift, c))
		}
	}
	return out
}
