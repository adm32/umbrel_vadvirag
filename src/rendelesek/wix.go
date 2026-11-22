// Vadvirág Rendelések – Wix: az űrlap-beküldések lekérdezése és a webhook tartalmának feldolgozása.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ---------------------------------------------------------------- Wix lekérdezés

func (a *App) wixDo(method, path string, body any) (map[string]any, error) {
	c := a.conf().Wix
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	}
	req, _ := http.NewRequest(method, strings.TrimRight(c.APICim, "/")+path, rd)
	req.Header.Set("Authorization", c.APIKulcs)
	req.Header.Set("wix-site-id", c.SiteID)
	req.Header.Set("Content-Type", "application/json")
	cl := &http.Client{Timeout: 20 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("Wix hiba %d: %s", resp.StatusCode, msg)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func collectLabels(v any, out map[string]string) {
	switch t := v.(type) {
	case map[string]any:
		if target, ok := t["target"].(string); ok && target != "" {
			for _, k := range []string{"label", "title", "name", "displayName"} {
				if s, ok := t[k].(string); ok && s != "" {
					out[target] = s
					break
				}
			}
			if _, done := out[target]; !done {
				for _, sub := range t {
					if m, ok := sub.(map[string]any); ok {
						for _, k := range []string{"label", "title", "name", "displayName"} {
							if s, ok := m[k].(string); ok && s != "" {
								out[target] = s
								break
							}
						}
					}
					if _, done := out[target]; done {
						break
					}
				}
			}
		}
		for _, x := range t {
			collectLabels(x, out)
		}
	case []any:
		for _, x := range t {
			collectLabels(x, out)
		}
	}
}

func (a *App) formLabels(formID string) map[string]string {
	a.wixMu.Lock()
	if l, ok := a.labels[formID]; ok {
		a.wixMu.Unlock()
		return l
	}
	if t, ok := a.labelTry[formID]; ok && time.Since(t) < 5*time.Minute {
		a.wixMu.Unlock()
		return nil
	}
	a.labelTry[formID] = time.Now()
	a.wixMu.Unlock()

	res, err := a.wixDo("GET", "/form-schema-service/v4/forms/"+formID, nil)
	if err != nil {
		log.Printf("Wix űrlap-címkék nem érhetők el (%v) – találgatással dolgozom", err)
		return nil
	}
	l := map[string]string{}
	collectLabels(res, l)
	a.wixMu.Lock()
	a.labels[formID] = l
	a.wixMu.Unlock()
	return l
}

func (a *App) pollWix() error {
	c := a.conf().Wix
	filter := map[string]any{"namespace": "wix.form_app.form"}
	if c.FormID != "" {
		filter["formId"] = c.FormID
	}
	body := map[string]any{"query": map[string]any{
		"filter":       filter,
		"sort":         []map[string]string{{"fieldName": "createdDate", "order": "DESC"}},
		"cursorPaging": map[string]any{"limit": 100},
	}}
	res, err := a.wixDo("POST", "/form-submission-service/v4/submissions/namespace/query", body)
	if err != nil {
		return err
	}
	subs, _ := res["submissions"].([]any)
	for i := len(subs) - 1; i >= 0; i-- { // régebbi előbb
		m, _ := subs[i].(map[string]any)
		if m == nil {
			continue
		}
		if st, _ := m["status"].(string); st == "PENDING" || st == "PAYMENT_WAITING" || st == "PAYMENT_CANCELED" {
			continue
		}
		id, _ := m["id"].(string)
		if id == "" {
			id, _ = m["_id"].(string)
		}
		formID, _ := m["formId"].(string)
		vals := map[string]string{}
		if sv, ok := m["submissions"].(map[string]any); ok {
			for k, v := range sv {
				flatten(k, v, vals)
			}
		}
		if len(vals) == 0 {
			continue
		}
		o := mapFields(vals, a.formLabels(formID))
		o.Source = "wix"
		o.ExtID = "wix:" + id
		if cd, _ := m["createdDate"].(string); cd != "" {
			if t, err := time.Parse(time.RFC3339, cd); err == nil {
				o.Created = t
			}
		}
		a.ingest(o)
	}
	return nil
}

func (a *App) wixLoop() {
	if a.conf().Wix.APIKulcs == "" {
		log.Printf("Wix lekérdezés: nincs API kulcs megadva (beallitasok.json) – a rendelések csak webhookon / kézzel érkeznek.")
		return
	}
	for {
		err := a.pollWix()
		a.wixMu.Lock()
		if err != nil {
			a.wixErr = err.Error()
			log.Printf("Wix lekérdezés hiba: %v", err)
		} else {
			a.wixErr = ""
			a.wixLastOK = time.Now()
		}
		a.wixMu.Unlock()
		time.Sleep(time.Duration(a.conf().Wix.LekerdezesSec) * time.Second)
	}
}

// wixSubmissions: a Wix "Send HTTP request" tartalmából (data.submissions[] = {label,value})
// címke->érték párokat készít. Ha nincs ilyen tömb, nil-t ad vissza.
func wixSubmissions(parsed any) (map[string]string, string) {
	root, _ := parsed.(map[string]any)
	if root == nil {
		return nil, ""
	}
	d, _ := root["data"].(map[string]any)
	if d == nil {
		d = root
	}
	arr, _ := d["submissions"].([]any)
	if len(arr) == 0 {
		return nil, ""
	}
	out := map[string]string{}
	for i, x := range arr {
		m, _ := x.(map[string]any)
		if m == nil {
			continue
		}
		label, _ := m["label"].(string)
		if label == "" {
			label = fmt.Sprintf("mező %d", i+1)
		}
		v := valToString(m["value"])
		if v == "" {
			continue
		}
		if old, ok := out[label]; ok {
			v = old + ", " + v
		}
		out[label] = v
	}
	id, _ := d["submissionId"].(string)
	return out, id
}
