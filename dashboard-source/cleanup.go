package main

// A statisztika tisztítása az irányítópultból: teszt / saját rendelések jelölése (a Rendelések
// appban tárolódik), egy-egy látogatás kivétele és a „saját eszköz” link (a weboldalon tárolódik).

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
)

var digitsOnly = regexp.MustCompile(`^\d{1,9}$`)

// postJSON: kérés a másik app belső portjára; az első elérhető címet használja.
func postJSON(bases []string, path string, body any) (json.RawMessage, error) {
	b, _ := json.Marshal(body)
	var last error
	for _, base := range bases {
		resp, err := httpc.Post(base+path, "application/json", bytes.NewReader(b))
		if err != nil {
			last = err
			continue
		}
		out, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			last = fmt.Errorf("HTTP %d: %s", resp.StatusCode, bytes.TrimSpace(out))
			continue
		}
		return out, nil
	}
	if last == nil {
		last = errors.New("nincs megadott cím")
	}
	return nil, last
}

func relay(w http.ResponseWriter, out json.RawMessage, err error) {
	if err != nil {
		writeJSON(w, 502, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(out)
}

func registerCleanup(mux *http.ServeMux) {
	// POST /api/orders/{id}/test {"test":true} – teszt / nem teszt
	mux.HandleFunc("POST /api/orders/{id}/test", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Test bool `json:"test"`
		}
		id := r.PathValue("id")
		if !digitsOnly.MatchString(id) || json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&in) != nil {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		out, err := postJSON(ordersDash, "/dash/orders/"+id+"/test", in)
		relay(w, out, err)
	})
	// POST /api/orders/{id}/own {"own":true} – ennek a rendelésnek a telefonszáma az enyém
	mux.HandleFunc("POST /api/orders/{id}/own", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Own bool `json:"own"`
		}
		id := r.PathValue("id")
		if !digitsOnly.MatchString(id) || json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&in) != nil {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		out, err := postJSON(ordersDash, "/dash/orders/"+id+"/own", in)
		relay(w, out, err)
	})
	// POST /api/visits/exclude {"date":"…","id":"…"} – egy látogatás kivétele
	mux.HandleFunc("POST /api/visits/exclude", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Date string `json:"date"`
			ID   string `json:"id"`
		}
		if json.NewDecoder(io.LimitReader(r.Body, 1<<10)).Decode(&in) != nil || in.ID == "" {
			writeJSON(w, 400, map[string]string{"error": "hibás kérés"})
			return
		}
		out, err := postJSON(siteStats, "/exclude", in)
		relay(w, out, err)
	})
	// GET /api/staff-link – a „saját eszköz” link
	mux.HandleFunc("GET /api/staff-link", func(w http.ResponseWriter, r *http.Request) {
		var out json.RawMessage
		_, err := getJSON(siteStats, "/staff-token", &out)
		relay(w, out, err)
	})
}
