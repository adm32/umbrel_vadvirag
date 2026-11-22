package main

// „Automatikus” nézet és elemzési export.
//
//   - GET  /api/schedule        – minden, ami magától változik (weboldal témája, napi és hétvégi ajánlat,
//     rendelési idősávok, korábbi zárás, előrendelés, nyereményjáték, az irányítópult saját figyelései)
//   - POST /api/schedule/site   – a weboldal kapcsolói (téma, heti menü, hétvégi ajánlat)
//   - POST /api/schedule/orders – a Rendelések kapcsolói (szüneteltetés, korábbi zárás, előrendelés)
//   - GET  /api/export?days=90  – letölthető JSON az elemzéshez (személyes adatok nélkül)

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

func registerAutomation(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/schedule", handleSchedule)
	mux.HandleFunc("POST /api/schedule/site", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		if json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in) != nil {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		out, err := postJSON(siteStats, "/schedule", in)
		relay(w, out, err)
	})
	mux.HandleFunc("POST /api/schedule/orders", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Do   string `json:"do"`
			Note string `json:"note"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in) != nil || in.Do == "" {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		out, err := postJSON(ordersDash, "/dash/action", in)
		relay(w, out, err)
	})
	mux.HandleFunc("GET /api/export", handleExport)
}

// nextSummary: mikor megy ki a következő napi összesítő.
func nextSummary(s Settings, now time.Time) time.Time {
	now = now.In(budapest)
	at, err := time.ParseInLocation("15:04", s.SummaryTime, budapest)
	if err != nil {
		return time.Time{}
	}
	t := time.Date(now.Year(), now.Month(), now.Day(), at.Hour(), at.Minute(), 0, 0, budapest)
	mon.Lock()
	done := mon.lastSummary == now.Format("2006-01-02")
	mon.Unlock()
	if done || now.After(t.Add(3*time.Hour)) {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func handleSchedule(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"now": time.Now()}
	var wg sync.WaitGroup
	var mu sync.Mutex
	set := func(k string, v any) { mu.Lock(); out[k] = v; mu.Unlock() }
	wg.Add(2)
	go func() {
		defer wg.Done()
		var v json.RawMessage
		if _, err := getJSON(siteStats, "/schedule", &v); err != nil {
			set("site_error", err.Error())
		} else {
			set("site", v)
		}
	}()
	go func() {
		defer wg.Done()
		var v json.RawMessage
		if _, err := getJSON(ordersDash, "/dash/schedule", &v); err != nil {
			set("orders_error", err.Error())
		} else {
			set("orders", v)
		}
	}()
	wg.Wait()
	s := getSettings()
	mon.Lock()
	lastPub := mon.lastPublic
	mon.Unlock()
	dash := map[string]any{"daily_summary": s.DailySummary, "summary_time": s.SummaryTime, "pending_minutes": s.PendingMinutes,
		"public_url": s.PublicURL, "orders_url": s.OrdersURL, "notify_new_order": s.NotifyNewOrder, "notify_messages": s.NotifyMessages,
		"ntfy_ready": s.NtfyTopic != ""}
	if s.DailySummary {
		dash["summary_next"] = nextSummary(s, time.Now())
	}
	if s.PublicURL != "" && !lastPub.IsZero() {
		dash["public_next"] = lastPub.Add(5 * time.Minute)
	}
	out["dashboard"] = dash
	writeJSON(w, 200, out)
}

// ---------------------------------------------------------------- export

func handleExport(w http.ResponseWriter, r *http.Request) {
	days, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if days <= 0 || days > 400 {
		days = 90
	}
	now := time.Now().In(budapest)
	from := now.AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	var (
		wg   sync.WaitGroup
		site struct {
			Days []map[string]any `json:"days"`
		}
		orders   []Order
		pre      []map[string]any
		msgs     []Message
		sched    json.RawMessage
		siteErr  error
		orderErr error
	)
	wg.Add(5)
	go func() { defer wg.Done(); _, siteErr = getJSON(siteStats, fmt.Sprintf("/stats?days=%d", days), &site) }()
	go func() {
		defer wg.Done()
		_, orderErr = getJSON(ordersDash, fmt.Sprintf("/dash/orders?days=%d", days), &orders)
	}()
	go func() { defer wg.Done(); getJSON(ordersDash, "/dash/preorders", &pre) }()
	go func() { defer wg.Done(); getJSON(ordersDash, "/dash/messages", &msgs) }()
	go func() { defer wg.Done(); getJSON(siteStats, "/schedule", &sched) }()
	wg.Wait()

	// a sütit elfogadó vendégek: honnan jöttek (a kód helyett csak a forrás)
	known := map[string]struct {
		Ref      string `json:"ref"`
		Campaign string `json:"campaign"`
		Days     int    `json:"days"`
	}{}
	var keys []string
	for _, o := range orders {
		if o.Visitor != "" && len(keys) < 400 {
			keys = append(keys, o.Visitor)
		}
	}
	if len(keys) > 0 {
		getJSON(siteStats, "/known?ck="+strings.Join(keys, ","), &known)
	}
	custN := map[string]int{}
	for _, o := range orders {
		if o.Customer != "" && !o.Test {
			custN[o.Customer]++
		}
	}
	type line struct {
		Name string `json:"name"`
		Qty  int    `json:"qty"`
	}
	var outOrders []map[string]any
	dish := map[string]int{}
	byHour, byWeekday := make([]int, 24), make([]int, 7)
	n, rejected, revenue, priced := 0, 0, 0, 0
	for _, o := range orders {
		if o.Test || o.Created.In(budapest).Format("2006-01-02") < from {
			continue
		}
		var lines []line
		for _, l := range strings.Split(o.Items, "\n") {
			var q int
			if _, err := fmt.Sscanf(l, "%d×", &q); err == nil {
				if i := strings.Index(l, "× "); i >= 0 {
					name := l[i+len("× "):]
					if j := strings.LastIndex(name, " – "); j >= 0 {
						name = name[:j]
					}
					lines = append(lines, line{name, q})
					if o.Status != "rejected" {
						dish[name] += q
					}
				}
			}
		}
		t := o.Created.In(budapest)
		n++
		byHour[t.Hour()]++
		byWeekday[(int(t.Weekday())+6)%7]++
		if o.Status == "rejected" {
			rejected++
		} else if o.Total > 0 {
			revenue += o.Total
			priced++
		}
		row := map[string]any{"id": o.ID, "created": t.Format("2006-01-02 15:04"), "weekday": t.Weekday().String(), "source": o.Source,
			"mode": o.Mode, "town": o.City, "status": o.Status, "payment": o.Payment, "food": o.Food, "packaging": o.Pack,
			"delivery_fee": o.DeliveryFee, "total": o.Total, "total_estimated": o.TotalEst, "free_delivery": o.Free,
			"customer": o.Customer, "customer_orders_total": custN[o.Customer]}
		if len(lines) > 0 {
			row["lines"] = lines
		} else {
			row["text"] = strings.TrimSpace(strings.TrimPrefix(o.Items, "Szabadon leírt rendelés:"))
		}
		if o.Reason != "" {
			row["reject_reason"] = o.Reason
		}
		if !o.AcceptedAt.IsZero() && o.AcceptedAt.After(o.Created) {
			row["minutes_to_accept"] = int(o.AcceptedAt.Sub(o.Created).Minutes())
		}
		if k, ok := known[o.Visitor]; ok {
			row["came_from"] = k.Ref
			if k.Campaign != "" {
				row["campaign"] = k.Campaign
			}
			row["visitor_days"] = k.Days
		}
		outOrders = append(outOrders, row)
	}
	type kv struct {
		Name string `json:"name"`
		Qty  int    `json:"qty"`
	}
	var top []kv
	for k, v := range dish {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].Qty > top[j].Qty })
	if len(top) > 25 {
		top = top[:25]
	}
	var outPre []map[string]any
	for _, p := range pre {
		delete(p, "name")
		delete(p, "phone")
		delete(p, "email")
		delete(p, "street")
		delete(p, "note")
		delete(p, "drive_min")
		outPre = append(outPre, p)
	}
	msgKinds := map[string]int{}
	var upcoming []map[string]any
	today := now.Format("2006-01-02")
	for _, m := range msgs {
		if m.At.In(budapest).Format("2006-01-02") >= from {
			msgKinds[m.Kind]++
		}
		if m.Date >= today && !m.Done {
			upcoming = append(upcoming, map[string]any{"kind": m.Kind, "date": m.Date, "time": m.Time, "guests": m.Guests})
		}
	}
	var siteDays []map[string]any
	for _, d := range site.Days {
		if s, _ := d["date"].(string); s >= from {
			siteDays = append(siteDays, d)
		}
	}
	avg := 0
	if priced > 0 {
		avg = revenue / priced
	}
	exp := map[string]any{
		"about": "Vadvirág Étterem (Zalaudvarnok) – a weboldal és az online rendelések elemzési exportja. " +
			"Személyes adat nincs benne: a vendégeket csak egy telefonszámból képzett névtelen kód (customer) azonosítja. " +
			"Az összegek forintban vannak; total = food + packaging + delivery_fee. A teszt és saját rendelések kimaradnak. " +
			"website.daily: napi összesítők (visitors = egyedi látogató, views = oldalmegnyitás, refs = honnan jöttek, sections = megnézett részek, " +
			"events = kattintások, adds = kosárba tett ételek azonosítója, towns/towns_no = keresett települések, fun_* = tölcsér lépései). " +
			"Az időpontok budapesti idő szerint vannak.",
		"generated": now.Format(time.RFC3339),
		"period":    map[string]any{"from": from, "to": today, "days": days},
		"summary": map[string]any{"orders": n, "rejected": rejected, "revenue_huf": revenue, "average_order_huf": avg,
			"orders_by_hour": byHour, "orders_by_weekday_mon_first": byWeekday, "top_dishes": top,
			"preorders": len(outPre), "messages_by_kind": msgKinds},
		"orders":                outOrders,
		"preorders":             outPre,
		"upcoming_reservations": upcoming,
		"website":               map[string]any{"daily": siteDays},
		"dish_names":            dishNames(),
	}
	if len(sched) > 0 {
		exp["automations"] = sched
	}
	if siteErr != nil {
		exp["website_error"] = siteErr.Error()
	}
	if orderErr != nil {
		exp["orders_error"] = orderErr.Error()
	}
	b, _ := json.MarshalIndent(exp, "", " ")
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="vadvirag-elemzes-%s-%dnap.json"`, today, days))
	w.Write(b)
}
