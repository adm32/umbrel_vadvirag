package main

import "net/http"

// POST /api/orders/{id}/total {"total": 4500} – a pontos végösszeg megadása (pl. a szabadon leírt
// rendelésnél, amikor a személyzet kiszámolta). 0 = vissza a becsléshez.
func (a *App) handleSetTotal(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Total int `json:"total"`
	}
	id, ok := a.orderID(r)
	if !ok || readJSON(r, &in, 1<<10) != nil || in.Total < 0 || in.Total > 1_000_000 {
		writeJSON(w, 400, map[string]string{"error": "hibás összeg"})
		return
	}
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	o := a.find(id)
	if o == nil {
		writeJSON(w, 404, map[string]string{"error": "nincs ilyen rendelés"})
		return
	}
	if in.Total == 0 {
		o.TotalManual = false
		applyPrice(o)
	} else {
		o.Total, o.TotalManual, o.TotalEst = in.Total, true, false
		// az ételek része: a végösszeg mínusz a szállítás és a (becsült) doboz
		o.Food = max(in.Total-max(o.DeliveryFee, 0)-o.Pack, 0)
	}
	if o.Extra == nil {
		o.Extra = map[string]string{}
	}
	if o.TotalManual {
		o.Extra["Végösszeg"] = formatHUF(o.Total) + " Ft (megadva)"
	} else if o.Total > 0 {
		o.Extra["Végösszeg"] = "kb. " + formatHUF(o.Total) + " Ft (becslés)"
	}
	a.st.saveLocked()
	writeJSON(w, 200, o)
}
