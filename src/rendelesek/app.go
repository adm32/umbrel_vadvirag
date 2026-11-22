// Vadvirág Rendelések – az App: a futó program állapota (beállítások + rendelések),
// és egy új rendelés felvétele (ingest).

package main

import (
	"log"
	"sort"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------- app

type App struct {
	cfgMu     sync.RWMutex
	cfg       Config
	cfgPath   string
	dataDir   string
	container bool // Dockerben / Umbrelen fut
	st        *Store
	feeIx     []feeEntry

	wixMu     sync.Mutex
	wixLastOK time.Time
	wixErr    string
	labels    map[string]map[string]string
	labelTry  map[string]time.Time
}

func norm(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.NewReplacer("á", "a", "é", "e", "í", "i", "ó", "o", "ö", "o", "ő", "o", "ú", "u", "ü", "u", "ű", "u").Replace(s)
}

func newApp(cfg Config, st *Store) *App {
	a := &App{cfg: cfg, st: st, labels: map[string]map[string]string{}, labelTry: map[string]time.Time{}}
	for name, fee := range cfg.Dijak {
		a.feeIx = append(a.feeIx, feeEntry{norm(name), name, fee, cfg.Menetidok[name]})
	}
	sort.Slice(a.feeIx, func(i, j int) bool { return len(a.feeIx[i].norm) > len(a.feeIx[j].norm) })
	if st != nil && st.path != "" {
		a.migrateOrders()
	}
	return a
}

// conf: a beállítások pillanatnyi másolata (a beállítások menüből futás közben is változhatnak).
func (a *App) conf() Config {
	a.cfgMu.RLock()
	defer a.cfgMu.RUnlock()
	return a.cfg
}

// updateConf: beállítás módosítása és mentése egy lépésben.
func (a *App) updateConf(fn func(c *Config)) error {
	a.cfgMu.Lock()
	defer a.cfgMu.Unlock()
	c := a.cfg
	fn(&c)
	if err := saveConfig(a.cfgPath, c); err != nil {
		return err
	}
	a.cfg = c
	return nil
}

func (a *App) needsSetup() bool { return a.conf().Jelszo == "" }

// ingest: új rendelés felvétele. Visszatér: felvettük-e (false = már megvolt).
func (a *App) ingest(o *Order) bool {
	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	if o.ExtID != "" {
		for _, x := range a.st.Orders {
			if x.ExtID == o.ExtID {
				return false
			}
		}
	}
	o.ID = a.st.NextID
	a.st.NextID++
	if o.Created.IsZero() {
		o.Created = time.Now()
	}
	o.Status = StNew
	o.StatusAt = time.Now()
	o.DeliveryFee = a.fee(o.Mode, o.City)
	if o.FreeDelivery && o.DeliveryFee > 0 {
		o.DeliveryFee = 0
	}
	o.DriveMin = a.driveMins(o.Mode, o.City)
	applyPrice(o)
	a.autoTest(o)
	a.st.Orders = append(a.st.Orders, o)
	a.st.saveLocked()
	log.Printf("Új rendelés #%d (%s, %s)", o.ID, o.Name, o.Source)
	go a.notifyNewOrder(*o)
	return true
}
