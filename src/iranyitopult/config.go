// Vadvirág Irányítópult – beállítások (settings.json): ntfy téma, figyelési küszöbök.

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	_ "time/tzdata"
)

// ---------------------------------------------------------------- beállítások

type Settings struct {
	NtfyServer     string          `json:"ntfy_server"`
	NtfyTopic      string          `json:"ntfy_topic"`
	NtfyToken      string          `json:"ntfy_token,omitempty"`
	PublicURL      string          `json:"public_url"`
	OrdersURL      string          `json:"orders_url"` // a rendeléskezelő címe (az értesítésre koppintva ez nyílik meg)
	PendingMinutes int             `json:"pending_minutes"`
	DailySummary   bool            `json:"daily_summary"`
	SummaryTime    string          `json:"summary_time"`
	NotifyNewOrder bool            `json:"notify_new_order"`
	NotifyMessages bool            `json:"notify_messages"`
	Muted          map[string]bool `json:"muted,omitempty"` // kikapcsolt figyelések
}

func defaultSettings() Settings {
	return Settings{NtfyServer: "https://ntfy.sh", PublicURL: "https://www.vadviragetterem.com", PendingMinutes: 10,
		DailySummary: true, SummaryTime: "22:15", Muted: map[string]bool{}}
}

var (
	setMu    sync.Mutex
	settings = defaultSettings()
)

func loadJSON(name string, v any) {
	if b, err := os.ReadFile(filepath.Join(dataDir, name)); err == nil {
		json.Unmarshal(b, v)
	}
}

func saveJSON(name string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(dataDir, name)
	if err := os.WriteFile(p+".tmp", b, 0o600); err != nil {
		return err
	}
	return os.Rename(p+".tmp", p)
}

func getSettings() Settings {
	setMu.Lock()
	defer setMu.Unlock()
	s := settings
	m := map[string]bool{}
	for k, v := range settings.Muted {
		m[k] = v
	}
	s.Muted = m
	return s
}
