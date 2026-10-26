package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func beaconAs(ip, ua, body string, cookies ...*http.Cookie) {
	r := httptest.NewRequest("POST", "/api/t", strings.NewReader(body))
	r.Header.Set("User-Agent", ua)
	r.Header.Set("CF-Connecting-IP", ip)
	r.Header.Set("CF-IPCountry", map[bool]string{true: "US", false: "HU"}[strings.HasPrefix(ip, "9.")])
	for _, c := range cookies {
		r.AddCookie(c)
	}
	handleBeacon(httptest.NewRecorder(), r)
}

func dayJSON() string {
	st.Lock()
	defer st.Unlock()
	recompute()
	d := *st.f.Days[dayKey(time.Now())]
	b, _ := json.Marshal(d)
	return string(b)
}

func resetStats() {
	st.Lock()
	st.f = statsFile{Days: map[string]*Day{}, Sessions: map[string]map[string]*Session{}, Known: map[string]*Known{}, Salt: "x", SaltDay: dayKey(time.Now())}
	st.Unlock()
}

const iphone = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 Version/17.0 Mobile/15E148 Safari/604.1"
const metaUA = "Mozilla/5.0 (Linux; Android 10; K) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Mobile Safari/537.36"

func realVisit() {
	beaconAs("84.1.1.1", iphone, `{"t":"view","p":"/","l":"hu","w":390,"h":844,"r":"https://www.google.com/"}`)
	beaconAs("84.1.1.1", iphone, `{"t":"sec","n":"etlap"}`)
	beaconAs("84.1.1.1", iphone, `{"t":"ev","n":"add","v":"pizzak-pizza-salami"}`)
}

func TestRemoveSessionRestoresTotals(t *testing.T) {
	resetStats()
	realVisit()
	want := dayJSON()
	// egy második látogató sok mindent csinál, majd kivesszük
	ua2 := "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 Version/17.0 Safari/605.1.15"
	beaconAs("84.2.2.2", ua2, `{"t":"view","p":"/","l":"hu","w":1440,"h":900,"r":"https://www.facebook.com/","dk":true}`)
	beaconAs("84.2.2.2", ua2, `{"t":"view","p":"/en","l":"en"}`)
	beaconAs("84.2.2.2", ua2, `{"t":"sec","n":"kiszallitas"}`)
	beaconAs("84.2.2.2", ua2, `{"t":"ev","n":"town","v":"Türje","ok":true}`)
	beaconAs("84.2.2.2", ua2, `{"t":"ev","n":"town","v":"Hévíz","ok":false}`)
	beaconAs("84.2.2.2", ua2, `{"t":"ev","n":"add","v":"burger-smash-burger"}`)
	beaconAs("84.2.2.2", ua2, `{"t":"err","msg":"x"}`)
	if dayJSON() == want {
		t.Fatal("second visitor not counted")
	}
	st.Lock()
	var key string
	for k, s := range st.f.Sessions[dayKey(time.Now())] {
		if s.OS == "macOS" {
			key = k
		}
	}
	removeSessionLocked(dayKey(time.Now()), key)
	st.Unlock()
	if got := dayJSON(); got != want {
		var a, b map[string]any
		json.Unmarshal([]byte(got), &a)
		json.Unmarshal([]byte(want), &b)
		for k := range b {
			if !reflect.DeepEqual(a[k], b[k]) {
				t.Errorf("%s: got %v want %v", k, a[k], b[k])
			}
		}
	}
}

func TestMetaBotCleanupAndStaff(t *testing.T) {
	resetStats()
	realVisit()
	want := dayJSON()
	// régi (szűrés előtti) Facebook-ellenőrző: közvetlenül a munkamenetbe írjuk, ahogy régen bekerült
	metaNetsSave := metaNets
	metaNets = nil // a régi verzió még nem szűrte
	beaconAs("9.9.9.9", metaUA, `{"t":"view","p":"/","l":"hu","w":412,"h":915,"r":"https://m.facebook.com/"}`)
	beaconAs("9.9.9.9", metaUA, `{"t":"sec","n":"rolunk"}`)
	beaconAs("9.9.9.9", metaUA, `{"t":"err","msg":"e.target.matches is not a function"}`)
	metaNets = metaNetsSave
	if dayJSON() == want {
		t.Fatal("bot visit not recorded")
	}
	st.Lock()
	st.f.CleanVer = 0
	st.Unlock()
	cleanupOnStart()
	if got := dayJSON(); got != want {
		t.Errorf("after cleanup totals differ:\n got %s\nwant %s", got, want)
	}
	// saját eszköz link
	rec := httptest.NewRecorder()
	handleStaffToken(rec, httptest.NewRequest("GET", "/staff-token", nil))
	var tk struct{ Token string }
	json.Unmarshal(rec.Body.Bytes(), &tk)
	rec = httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/sajat?t="+tk.Token, nil)
	req.Header.Set("User-Agent", iphone)
	req.Header.Set("CF-Connecting-IP", "84.1.1.1")
	handleStaffLink(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Set-Cookie"), staffCookie) {
		t.Fatalf("staff link failed: %d", rec.Code)
	}
	if d := dayJSON(); strings.Contains(d, `"visitors":1`) {
		t.Error("own earlier visit should have been removed")
	}
	before := dayJSON()
	beaconAs("84.1.1.1", iphone, `{"t":"view","p":"/","l":"hu"}`, &http.Cookie{Name: staffCookie, Value: tk.Token})
	if dayJSON() != before {
		t.Error("staff device still counted")
	}
	rec = httptest.NewRecorder()
	handleStaffLink(rec, httptest.NewRequest("GET", "/sajat?t=wrong", nil))
	if rec.Code != 404 {
		t.Error("wrong token accepted")
	}
}

func TestForeignErr(t *testing.T) {
	for _, m := range []string{"TypeError: undefined is not an object (evaluating 'window._META.IABInnerFrame')", "Script error.", "ResizeObserver loop limit exceeded"} {
		if !foreignErr(m, "") {
			t.Errorf("idegen hibának kellene lennie: %q", m)
		}
	}
	if foreignErr("TypeError: x is undefined", "https://www.vadviragetterem.com/app.js?v=1:120") {
		t.Error("a saját hiba nem idegen")
	}
}
