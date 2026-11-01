package main

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestDashScheduleAndActions(t *testing.T) {
	dir := t.TempDir()
	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, &Store{})
	a.dataDir, a.cfgPath = dir, filepath.Join(dir, "beallitasok.json")
	do := func(action string) int {
		w := httptest.NewRecorder()
		a.handleDashAction(w, httptest.NewRequest("POST", "/dash/action", strings.NewReader(`{"do":"`+action+`","note":"karbantartás"}`)))
		return w.Code
	}
	if do("pause") != 200 || !a.conf().OnlineSzunetel || a.conf().OnlineUzenet != "karbantartás" {
		t.Fatal("pause")
	}
	if do("resume") != 200 || a.conf().OnlineSzunetel {
		t.Fatal("resume")
	}
	if do("close_today") != 200 || !a.closedToday() || do("open_today") != 200 || a.closedToday() {
		t.Fatal("closed today")
	}
	if preOff != nil && (do("preorder_soldout") != 200 || !a.preSoldOut() || do("preorder_available") != 200 || a.preSoldOut()) {
		t.Fatal("preorder sold out")
	}
	if do("nonsense") != 400 {
		t.Fatal("unknown action should fail")
	}
	w := httptest.NewRecorder()
	a.handleDashSchedule(w, httptest.NewRequest("GET", "/dash/schedule", nil))
	var out map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out["daily"] == nil || out["giveaway"] == nil {
		t.Fatalf("schedule: %v %s", err, w.Body.String())
	}
}
