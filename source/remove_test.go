package main

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRemoveOrder(t *testing.T) {
	st := &Store{Orders: []*Order{{ID: 1, Status: StNew, Email: "a@b.hu"}, {ID: 2, Status: StNew}}}
	a := newApp(Config{Dijak: defaultFees(), Menetidok: defaultDriveTimes()}, st)
	call := func(id, how string) int {
		r := httptest.NewRequest("POST", "/api/orders/"+id+"/remove", strings.NewReader(`{"how":"`+how+`"}`))
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		a.handleRemove(w, r)
		return w.Code
	}
	if call("1", "close") != 200 || st.Orders[0].Status != StDone || st.Orders[0].EmailState != "" {
		t.Fatal("close should mark done without email")
	}
	if call("2", "delete") != 200 || len(st.Orders) != 1 || st.Orders[0].ID != 1 {
		t.Fatal("delete should remove the order")
	}
	if call("9", "delete") != 404 {
		t.Fatal("unknown id")
	}
}
