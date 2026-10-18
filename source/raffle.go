package main

// Nyereményjáték-sorsolás: a megosztók nevét bemásolják, a program kriptográfiai
// véletlennel húz nyertest (és ha kell, pótnyertest), és minden sorsolást elment.

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Raffle struct {
	ID       int       `json:"id"`
	At       time.Time `json:"at"`
	Title    string    `json:"title,omitempty"`
	Link     string    `json:"link,omitempty"`
	Count    int       `json:"count"`
	Names    []string  `json:"names"`
	Winners  []string  `json:"winners"`
	Reserves []string  `json:"reserves,omitempty"`
}

var raffleMu sync.Mutex

func (a *App) rafflePath() string { return filepath.Join(a.dataDir, "sorsolasok.json") }

func (a *App) loadRaffles() []Raffle {
	var rs []Raffle
	if b, err := os.ReadFile(a.rafflePath()); err == nil {
		json.Unmarshal(b, &rs)
	}
	return rs
}

// Facebookról másolt listában gyakori, nem névnek számító sorok.
var (
	junkLine = regexp.MustCompile(`(?i)^(megosztás|megosztotta|megosztva|követés|követem|barátnak jelölés|ismerős hozzáadása|üzenet|kedvelem|tetszik|válasz|hozzászólás|add friend|follow|following|message|like|reply|share|shared|public|nyilvános|ismerősök|friends|\d+\s*(p|ó|n|h|m|perc|óra|nap|hét|mp|min|hr|d|w|y)\.?|·.*|\d+)$`)
	spaceRe  = regexp.MustCompile(`\s+`)
	// [Név](https://…) – ilyen formában jön, ha a lista Markdown-ként másolódik.
	mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

func cleanNames(raw string) []string {
	seen := map[string]bool{}
	var out []string
	raw = mdLink.ReplaceAllString(raw, "$1")
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(strings.Trim(ln, "•-–*,;\t"))
		ln = spaceRe.ReplaceAllString(ln, " ")
		if ln == "" || len([]rune(ln)) < 2 || len([]rune(ln)) > 80 || junkLine.MatchString(ln) {
			continue
		}
		k := norm(ln)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, ln)
	}
	return out
}

func pick(n int) int {
	v, _ := rand.Int(rand.Reader, big.NewInt(int64(n)))
	return int(v.Int64())
}

// POST /api/raffle/preview – csak tisztítja és megszámolja a neveket.
func (a *App) handleRafflePreview(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Names string `json:"names"`
	}
	if err := readJSON(r, &in, 1<<20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	writeJSON(w, 200, map[string]any{"names": cleanNames(in.Names)})
}

// POST /api/raffle – sorsolás.
func (a *App) handleRaffleDraw(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Title    string `json:"title"`
		Link     string `json:"link"`
		Names    string `json:"names"`
		Winners  int    `json:"winners"`
		Reserves int    `json:"reserves"`
	}
	if err := readJSON(r, &in, 1<<20); err != nil {
		writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
		return
	}
	names := cleanNames(in.Names)
	if in.Winners < 1 {
		in.Winners = 1
	}
	if in.Winners > 10 || in.Reserves < 0 || in.Reserves > 10 {
		writeJSON(w, 400, map[string]string{"error": "Legfeljebb 10 nyertes és 10 pótnyertes lehet."})
		return
	}
	if len(names) < in.Winners+in.Reserves {
		writeJSON(w, 400, map[string]string{"error": "Kevesebb a résztvevő, mint ahány nyertest sorsolnál."})
		return
	}
	pool := append([]string(nil), names...)
	draw := func(k int) []string {
		var got []string
		for i := 0; i < k; i++ {
			j := pick(len(pool))
			got = append(got, pool[j])
			pool = append(pool[:j], pool[j+1:]...)
		}
		return got
	}
	rf := Raffle{At: time.Now(), Title: clip(in.Title, 120), Link: clip(in.Link, 500), Count: len(names), Names: names}
	rf.Winners = draw(in.Winners)
	rf.Reserves = draw(in.Reserves)

	raffleMu.Lock()
	defer raffleMu.Unlock()
	rs := a.loadRaffles()
	rf.ID = 1
	if len(rs) > 0 {
		rf.ID = rs[len(rs)-1].ID + 1
	}
	rs = append(rs, rf)
	if len(rs) > 200 {
		rs = rs[len(rs)-200:]
	}
	if b, err := json.MarshalIndent(rs, "", "  "); err == nil {
		tmp := a.rafflePath() + ".tmp"
		if os.WriteFile(tmp, b, 0o600) == nil {
			os.Rename(tmp, a.rafflePath())
		}
	}
	writeJSON(w, 200, rf)
}

// GET /api/raffle – korábbi sorsolások (legújabb elöl).
func (a *App) handleRaffleList(w http.ResponseWriter, r *http.Request) {
	raffleMu.Lock()
	rs := a.loadRaffles()
	raffleMu.Unlock()
	for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
		rs[i], rs[j] = rs[j], rs[i]
	}
	if rs == nil {
		rs = []Raffle{}
	}
	writeJSON(w, 200, rs)
}
