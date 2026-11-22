// Vadvirág Irányítópult – az egyes ellenőrzések (percenként): weboldal, rendelések, üzenetek,
// hibák a weboldalon, lemez, napi összefoglaló.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata"
)

// runChecks: percenként
func runChecks() {
	s := getSettings()
	// 1. weboldal
	var h siteHealth
	_, err := getJSON(siteStats, "/health", &h)
	result("site", err == nil, errText(err, "verzió "+h.Version),
		"🚨 A weboldal nem válaszol", "A Vadvirág Weboldal app nem érhető el az Umbrelen. Nézd meg, fut-e az app.", "A weboldal újra válaszol.", 2)
	// 2. rendeléskezelő
	var info map[string]any
	_, err2 := getJSON(ordersDash, "/dash/info", &info)
	result("orders", err2 == nil, errText(err2, "fogadja a rendeléseket"),
		"🚨 A rendeléskezelő nem válaszol", "A Vadvirág Rendelések app nem érhető el. Online rendelést most nem lehet fogadni.", "A rendeléskezelő újra válaszol.", 2)
	// 3. a weboldal eléri-e a rendeléskezelőt
	if err == nil {
		result("link", h.UpstreamOK, map[bool]string{true: "a weboldal eléri a rendeléskezelőt", false: "a weboldal nem éri el a rendeléskezelőt (" + h.Upstream + ")"}[h.UpstreamOK],
			"🚨 Az online rendelés nem működik", "A weboldal nem éri el a rendeléskezelőt, a vendégek nem tudnak rendelni.", "A weboldal újra eléri a rendeléskezelőt.", 2)
	}
	// 4. kívülről (5 percenként)
	mon.Lock()
	duePublic := s.PublicURL != "" && time.Since(mon.lastPublic) >= 5*time.Minute
	if duePublic {
		mon.lastPublic = time.Now()
	}
	mon.Unlock()
	if duePublic {
		ok, detail := checkPublic(s.PublicURL)
		result("public", ok, detail, "🚨 A vadviragetterem.com nem érhető el", "A weboldal kívülről (az interneten át) nem tölt be.", "A vadviragetterem.com újra elérhető.", 2)
	}
	// 5. tárhely
	if free, total, err := diskFree(dataDir); err == nil && total > 0 {
		pct := float64(free) / float64(total) * 100
		result("disk", pct >= 10, fmt.Sprintf("%.0f%% szabad (%.1f GB / %.1f GB)", pct, float64(free)/1e9, float64(total)/1e9),
			"⚠️ Kevés a szabad hely az Umbrelen", "A tárhely majdnem megtelt; a rendelések mentése elakadhat.", "Van újra elég szabad hely.", 1)
	}
	if err2 == nil {
		checkOrders(s)
		checkMessages(s)
	}
	if err == nil {
		checkSiteErrors()
	}
	checkSummary(s)
}

func errText(err error, ok string) string {
	if err != nil {
		return "nem elérhető: " + err.Error()
	}
	return ok
}

func checkPublic(base string) (bool, string) {
	u := strings.TrimRight(base, "/") + "/healthz"
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Vadvirag-Iranyitopult/"+version+" (monitor)")
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return false, "hiba: " + err.Error()
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode == 200 && strings.TrimSpace(string(b)) == "ok" {
		return true, "elérhető (" + u + ")"
	}
	d := fmt.Sprintf("HTTP %d", resp.StatusCode)
	if cf := cfErrorCode(string(b)); cf != "" {
		d += ", Cloudflare hiba " + cf
	}
	return false, d + " – " + u
}

func cfErrorCode(s string) string {
	i := strings.Index(s, "Error ")
	if i < 0 {
		return ""
	}
	s = s[i+6:]
	j := 0
	for j < len(s) && j < 5 && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	return s[:j]
}

func diskFree(p string) (free, total uint64, err error) {
	var fs syscall.Statfs_t
	if err = syscall.Statfs(p, &fs); err != nil {
		return
	}
	return fs.Bavail * uint64(fs.Bsize), fs.Blocks * uint64(fs.Bsize), nil
}

func checkOrders(s Settings) {
	var orders []Order
	if _, err := getJSON(ordersDash, "/dash/orders?days=2", &orders); err != nil {
		return
	}
	now := time.Now()
	var send []Alert
	mon.Lock()
	for _, o := range orders {
		// új rendelés
		if !mon.seenOrders[o.ID] {
			mon.seenOrders[o.ID] = true
			if mon.ordersPrimed && s.NotifyNewOrder {
				where := o.Mode
				if o.City != "" {
					where += ", " + o.City
				}
				t := ""
				if o.Total > 0 {
					t = " – " + huf(o.Total) + " Ft"
				}
				if o.Test {
					t += " (teszt)"
				}
				send = append(send, Alert{Key: "order", Title: fmt.Sprintf("🛒 Új rendelés #%d%s", o.ID, t), Msg: where + "\n" + o.Items, Level: "order", Priority: 4})
			}
		}
		if o.Test {
			continue // teszt / saját rendelésről nem riasztunk
		}
		// túl régóta vár
		if o.Status == "new" && s.PendingMinutes > 0 && now.Sub(o.Created) > time.Duration(s.PendingMinutes)*time.Minute &&
			now.Sub(o.Created) < 12*time.Hour && !mon.pendingSent[o.ID] {
			mon.pendingSent[o.ID] = true
			send = append(send, Alert{Key: "pending", Title: fmt.Sprintf("⏰ Rendelés #%d már %d perce vár", o.ID, int(now.Sub(o.Created).Minutes())),
				Msg: "Még senki nem fogadta el vagy utasította el.\n" + o.Items, Level: "error", Priority: 5})
		}
		// a vendégnek szóló e-mail nem ment el
		if o.EmailState == "failed" && !mon.emailSent[o.ID] && now.Sub(o.Created) < 24*time.Hour {
			mon.emailSent[o.ID] = true
			send = append(send, Alert{Key: "email", Title: fmt.Sprintf("✉️ Nem ment el az e-mail (#%d)", o.ID),
				Msg: "A vendég nem kapta meg a visszaigazoló e-mailt: " + o.EmailErr + "\nÉrdemes felhívni.", Level: "error", Priority: 4})
		}
	}
	mon.ordersPrimed = true
	mon.Unlock()
	for _, a := range send {
		notify(a)
	}
}

func checkMessages(s Settings) {
	var msgs []Message
	if _, err := getJSON(ordersDash, "/dash/messages", &msgs); err != nil {
		return
	}
	var send []Alert
	kinds := map[string]string{"asztal": "Asztalfoglalás", "rendezveny": "Rendezvény ajánlatkérés", "egyeb": "Kérdés"}
	mon.Lock()
	for _, m := range msgs {
		if !mon.seenMsgs[m.ID] {
			mon.seenMsgs[m.ID] = true
			if mon.msgsPrimed && s.NotifyMessages {
				d := m.Name
				if m.Date != "" {
					d += ", " + m.Date + " " + m.Time
				}
				if m.Guests > 0 {
					d += fmt.Sprintf(", %d fő", m.Guests)
				}
				send = append(send, Alert{Key: "message", Title: "✉️ Új üzenet a weboldalról: " + kinds[m.Kind], Msg: d, Level: "info", Priority: 3})
			}
		}
	}
	mon.msgsPrimed = true
	mon.Unlock()
	for _, a := range send {
		notify(a)
	}
}

// checkSiteErrors: a weboldal hibái (böngésző és szerver). Legfeljebb 30 percenként egy összesítő értesítés.
func checkSiteErrors() {
	var st siteStatsResp
	if _, err := getJSON(siteStats, "/stats?days=1", &st); err != nil {
		return
	}
	mon.Lock()
	var fresh []siteErr
	for _, e := range st.Errors {
		if e.At.After(mon.lastErrAt) {
			fresh = append(fresh, e)
		}
	}
	if len(fresh) > 0 {
		mon.lastErrAt = fresh[len(fresh)-1].At
		for _, e := range fresh {
			if e.Kind != "upstream" { // azt a "link" figyelés jelzi
				mon.errBuffer = append(mon.errBuffer, e)
			}
		}
	}
	var send *Alert
	if len(mon.errBuffer) > 0 && time.Since(mon.lastErrNotify) > 30*time.Minute {
		var b strings.Builder
		for i, e := range mon.errBuffer {
			if i == 5 {
				fmt.Fprintf(&b, "… és még %d", len(mon.errBuffer)-5)
				break
			}
			kind := map[string]string{"js": "böngésző", "server": "szerver"}[e.Kind]
			fmt.Fprintf(&b, "• [%s] %s (%s)\n", kind, e.Msg, e.Page)
		}
		send = &Alert{Key: "errors", Title: fmt.Sprintf("⚠️ %d hiba a weboldalon", len(mon.errBuffer)), Msg: strings.TrimSpace(b.String()), Level: "error", Priority: 3}
		mon.errBuffer = nil
		mon.lastErrNotify = time.Now()
	}
	mon.Unlock()
	if send != nil {
		notify(*send)
	}
}

// checkSummary: napi összesítő a beállított időpontban
func checkSummary(s Settings) {
	if !s.DailySummary {
		return
	}
	now := time.Now().In(budapest)
	day := now.Format("2006-01-02")
	mon.Lock()
	done := mon.lastSummary == day
	mon.Unlock()
	// csak a beállított időpont utáni 3 órában (így egy késő esti újraindítás nem küldi újra / későn)
	at, err := time.ParseInLocation("15:04", s.SummaryTime, budapest)
	if done || err != nil {
		return
	}
	at = time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, budapest)
	if now.Before(at) || now.After(at.Add(3*time.Hour)) {
		return
	}
	mon.Lock()
	mon.lastSummary = day
	mon.Unlock()
	var b strings.Builder
	var st siteStatsResp
	if _, err := getJSON(siteStats, "/stats?days=1", &st); err == nil && len(st.Days) > 0 {
		var d struct {
			Views    int `json:"views"`
			Visitors int `json:"visitors"`
		}
		json.Unmarshal(st.Days[len(st.Days)-1], &d)
		fmt.Fprintf(&b, "👀 %d látogató, %d oldalmegnyitás\n", d.Visitors, d.Views)
	}
	var orders []Order
	if _, err := getJSON(ordersDash, "/dash/orders?days=2", &orders); err == nil {
		n, rev, rej := 0, 0, 0
		dish := map[string]int{}
		for _, o := range orders {
			if o.Created.In(budapest).Format("2006-01-02") != day {
				continue
			}
			if o.Test {
				continue // teszt / saját rendelés: nem számít
			}
			n++
			if o.Status == "rejected" {
				rej++
				continue
			}
			rev += o.Total
			for _, l := range strings.Split(o.Items, "\n") {
				var q int
				var rest string
				if _, err := fmt.Sscanf(l, "%d×", &q); err == nil {
					if i := strings.Index(l, "× "); i >= 0 {
						rest = l[i+len("× "):]
						if j := strings.LastIndex(rest, " – "); j >= 0 {
							rest = rest[:j]
						}
						dish[rest] += q
					}
				}
			}
		}
		fmt.Fprintf(&b, "🛒 %d rendelés", n)
		if rej > 0 {
			fmt.Fprintf(&b, " (%d elutasítva)", rej)
		}
		if rev > 0 {
			fmt.Fprintf(&b, ", %s Ft bevétel", huf(rev))
		}
		b.WriteString("\n")
		type kv struct {
			k string
			v int
		}
		var top []kv
		for k, v := range dish {
			top = append(top, kv{k, v})
		}
		sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v })
		if len(top) > 0 {
			fmt.Fprintf(&b, "⭐ Legnépszerűbb: %s (%d db)", top[0].k, top[0].v)
		}
	}
	// előrendelések: ma beérkezett, és ami holnapra szól (a konyhának jó előre tudni)
	var pre []struct {
		At     time.Time `json:"at"`
		Date   string    `json:"date"`
		Total  int       `json:"total"`
		Status string    `json:"status"`
		Lines  []struct {
			Qty int `json:"qty"`
		} `json:"lines"`
	}
	if _, err := getJSON(ordersDash, "/dash/preorders", &pre); err == nil && len(pre) > 0 {
		tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
		nNew, sum, nTom, portions := 0, 0, 0, 0
		for _, p := range pre {
			if p.Status == "lemondva" {
				continue
			}
			if p.At.In(budapest).Format("2006-01-02") == day {
				nNew++
				sum += p.Total
			}
			if p.Date == tomorrow {
				nTom++
				for _, l := range p.Lines {
					portions += l.Qty
				}
			}
		}
		if nNew > 0 {
			fmt.Fprintf(&b, "\n📅 %d új előrendelés (%s Ft)", nNew, huf(sum))
		}
		if nTom > 0 {
			fmt.Fprintf(&b, "\n🍽 Holnapra: %d előrendelés, %d adag", nTom, portions)
		}
	}
	mon.save()
	notify(Alert{Key: "summary", Title: "🌺 Vadvirág – napi összesítő", Msg: strings.TrimSpace(b.String()), Level: "info", Priority: 2})
}
