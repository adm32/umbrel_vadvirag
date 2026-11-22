// Vadvirág Rendelések – mezők felismerése: a Wix-űrlap és más források mezőiből Order lesz.

package main

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------- mezők felismerése

var digitsRe = regexp.MustCompile(`\d`)

func classify(label string) string {
	n := norm(label)
	has := func(subs ...string) bool {
		for _, s := range subs {
			if strings.Contains(n, s) {
				return true
			}
		}
		return false
	}
	switch {
	case has("mail"):
		return "email"
	case has("telefon", "phone", "mobil"):
		return "phone"
	case has("atvetel", "pickup", "delivery_mode", "szallitas modja"):
		return "mode"
	case has("telepules", "varos", "city"):
		return "city"
	case has("utca", "hazszam", "street", "address", "cim"):
		return "street"
	case has("rendel", "etel", "order", "items"):
		return "items"
	case has("megjegyz", "note", "comment", "message"):
		return "note"
	case n == "nev" || strings.HasPrefix(n, "nev ") || strings.HasPrefix(n, "nev(") || has("name", "first_name", "last_name"):
		return "name"
	}
	return ""
}

func normMode(v string) string {
	if strings.Contains(norm(v), "kisz") || strings.Contains(norm(v), "deliver") || strings.Contains(norm(v), "haz") {
		return modeDelivery
	}
	return modePickup
}

// mapFields: kulcs->érték párokból rendelés. labels: kulcs->címke (ha ismert).
func mapFields(kv map[string]string, labels map[string]string) *Order {
	o := &Order{Extra: map[string]string{}}
	var names []string
	var rest [][2]string
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimSpace(kv[k])
		if v == "" {
			continue
		}
		label := k
		if l, ok := labels[k]; ok && l != "" {
			label = l
		}
		switch classify(label) {
		case "email":
			o.Email = v
		case "phone":
			o.Phone = v
		case "mode":
			o.Mode = normMode(v)
		case "city":
			o.City = v
		case "street":
			o.Street = v
		case "items":
			o.Items = v
		case "note":
			o.Note = v
		case "name":
			names = append(names, v)
		default:
			rest = append(rest, [2]string{label, v})
		}
	}
	o.Name = strings.Join(names, " ")
	// érték alapú tippelés a fel nem ismert mezőkre
	var leftovers [][2]string
	for _, p := range rest {
		v := p[1]
		switch {
		case o.Email == "" && strings.Contains(v, "@") && !strings.ContainsAny(v, " \n"):
			o.Email = v
		case o.Mode == "" && (norm(v) == "kiszallitas" || norm(v) == "szemelyes atvetel"):
			o.Mode = normMode(v)
		case o.Phone == "" && len(digitsRe.FindAllString(v, -1)) >= 8 && len(v) <= 20 && !strings.ContainsAny(v, "abcdefghijklmnopqrstuvwxyzáéíóöőúüű"):
			o.Phone = v
		default:
			leftovers = append(leftovers, p)
		}
	}
	// a leghosszabb maradék szöveg legyen a rendelés, ha még nincs
	if o.Items == "" && len(leftovers) > 0 {
		bi := 0
		for i, p := range leftovers {
			if len(p[1]) > len(leftovers[bi][1]) {
				bi = i
			}
		}
		o.Items = leftovers[bi][1]
		leftovers = append(leftovers[:bi], leftovers[bi+1:]...)
	}
	for _, p := range leftovers {
		o.Extra[p[0]] = p[1]
	}
	if len(o.Extra) == 0 {
		o.Extra = nil
	}
	if o.Mode == "" {
		if o.City != "" || o.Street != "" {
			o.Mode = modeDelivery
		} else {
			o.Mode = modePickup
		}
	}
	return o
}

func valToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		if t {
			return "igen"
		}
		return "nem"
	case []any:
		var parts []string
		for _, x := range t {
			if s := valToString(x); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	case map[string]any:
		for _, k := range []string{"stringValue", "numberValue", "boolValue"} {
			if x, ok := t[k]; ok {
				return valToString(x)
			}
		}
		if lv, ok := t["listValue"].(map[string]any); ok {
			return valToString(lv["values"])
		}
		// pl. cím objektum
		if f, ok := t["formatted"].(string); ok {
			return f
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			if s := valToString(t[k]); s != "" {
				parts = append(parts, s)
			}
		}
		return strings.Join(parts, ", ")
	}
	return fmt.Sprint(v)
}

func flatten(prefix string, v any, out map[string]string) {
	if m, ok := v.(map[string]any); ok {
		simple := false
		for _, k := range []string{"stringValue", "numberValue", "boolValue", "listValue"} {
			if _, has := m[k]; has {
				simple = true
			}
		}
		if !simple {
			for k, x := range m {
				key := k
				if prefix != "" {
					key = prefix + "." + k
				}
				flatten(key, x, out)
			}
			return
		}
	}
	if arr, ok := v.([]any); ok {
		allSimple := true
		for _, x := range arr {
			switch x.(type) {
			case map[string]any, []any:
				allSimple = false
			}
		}
		if !allSimple {
			for i, x := range arr {
				flatten(fmt.Sprintf("%s.%d", prefix, i), x, out)
			}
			return
		}
	}
	if s := valToString(v); s != "" {
		out[prefix] = s
	}
}
