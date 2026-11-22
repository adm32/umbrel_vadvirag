// Vadvirág Irányítópult – adatlekérés a másik két app belső portjáról
// (weboldal: SITE_STATS :9090, Rendelések: ORDERS_DASH :9091).

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
	_ "time/tzdata"
)

// ---------------------------------------------------------------- adatlekérés a másik két apptól

func getJSON(bases []string, path string, v any) (string, error) {
	var last error
	for _, b := range bases {
		resp, err := httpc.Get(b + path)
		if err != nil {
			last = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			last = fmt.Errorf("HTTP %d", resp.StatusCode)
			continue
		}
		if err := json.Unmarshal(body, v); err != nil {
			last = err
			continue
		}
		return b, nil
	}
	if last == nil {
		last = errors.New("nincs megadott cím")
	}
	return "", last
}

type Order struct {
	ID          int       `json:"id"`
	Source      string    `json:"source"`
	Created     time.Time `json:"created"`
	Name        string    `json:"name"`
	Mode        string    `json:"mode"`
	City        string    `json:"city"`
	Items       string    `json:"items"`
	Total       int       `json:"total"`
	DeliveryFee int       `json:"delivery_fee"`
	Payment     string    `json:"payment"`
	Status      string    `json:"status"`
	StatusAt    time.Time `json:"status_at"`
	AcceptedAt  time.Time `json:"accepted_at"`
	ETA         int       `json:"eta"`
	Reason      string    `json:"reason"`
	EmailState  string    `json:"email_state"`
	EmailErr    string    `json:"email_err"`
	Food        int       `json:"food"`
	Pack        int       `json:"pack"`
	TotalEst    bool      `json:"total_est,omitempty"`
	TotalManual bool      `json:"total_manual,omitempty"`
	Test        bool      `json:"test,omitempty"`
	TestAuto    bool      `json:"test_auto,omitempty"`
	Own         bool      `json:"own,omitempty"`
	Customer    string    `json:"cust"`
	Free        bool      `json:"free_delivery,omitempty"`
	Visitor     string    `json:"visitor,omitempty"`
}

type Message struct {
	ID     int       `json:"id"`
	At     time.Time `json:"at"`
	Kind   string    `json:"kind"`
	Name   string    `json:"name"`
	Date   string    `json:"date"`
	Time   string    `json:"time"`
	Guests int       `json:"guests"`
	Done   bool      `json:"done"`
}

type siteHealth struct {
	Version          string    `json:"version"`
	Started          time.Time `json:"started"`
	Upstream         string    `json:"upstream"`
	UpstreamOK       bool      `json:"upstream_ok"`
	MenuLive         bool      `json:"menu_live"`
	ErrorsSinceStart int       `json:"errors_since_start"`
	SiteURL          string    `json:"site_url"`
}

type siteErr struct {
	At   time.Time `json:"at"`
	Kind string    `json:"kind"`
	Msg  string    `json:"msg"`
	Src  string    `json:"src"`
	Page string    `json:"page"`
}

type siteStatsResp struct {
	Now              time.Time         `json:"now"`
	Live             []json.RawMessage `json:"live"`
	Days             []json.RawMessage `json:"days"`
	Errors           []siteErr         `json:"errors"`
	ErrorsSinceStart int               `json:"errors_since_start"`
	Version          string            `json:"version"`
}
