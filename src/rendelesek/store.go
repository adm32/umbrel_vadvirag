// Vadvirág Rendelések – rendelések tárolása: az Order szerkezet és az orders.json (Store).

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// ---------------------------------------------------------------- rendelések

const (
	StNew       = "new"
	StPreparing = "preparing"
	StDone      = "done"
	StRejected  = "rejected"
)

type Order struct {
	ID           int               `json:"id"`
	Source       string            `json:"source"`
	ExtID        string            `json:"ext_id,omitempty"`
	Created      time.Time         `json:"created"`
	Name         string            `json:"name"`
	Email        string            `json:"email"`
	Phone        string            `json:"phone"`
	Mode         string            `json:"mode"` // "Személyes átvétel" | "Kiszállítás"
	City         string            `json:"city"`
	Street       string            `json:"street"`
	Items        string            `json:"items"`
	Note         string            `json:"note,omitempty"`
	Extra        map[string]string `json:"extra,omitempty"`
	DeliveryFee  int               `json:"delivery_fee"`            // -1 = ismeretlen település
	FreeDelivery bool              `json:"free_delivery,omitempty"` // napi ajánlat előrendelése 11:00 előtt: nincs szállítási díj
	DriveMin     int               `json:"drive_min,omitempty"`     // autós menetidő percben a futárnak
	Status       string            `json:"status"`
	ETA          int               `json:"eta,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	StatusAt     time.Time         `json:"status_at"`
	AcceptedAt   time.Time         `json:"accepted_at,omitempty"` // mikor fogadtuk el (a vendég ehhez képest látja a várható időt)
	EmailState   string            `json:"email_state,omitempty"` // sending|sent|failed|none
	EmailErr     string            `json:"email_err,omitempty"`
	EmailAt      time.Time         `json:"email_at,omitempty"`
	Total        int               `json:"total,omitempty"`   // weboldali rendelésnél: végösszeg (szállítással, ha ismert)
	Payment      string            `json:"payment,omitempty"` // Készpénz | Kártya
	Token        string            `json:"token,omitempty"`   // a vendég ezzel követi a rendelése állapotát
	Visitor      string            `json:"visitor,omitempty"` // a weboldal sütijének hash-e (csak ha a vendég elfogadta) – az irányítópult ebből látja, honnan jött
	// Ár részletei (lásd pricing.go): Total = Food + Pack + szállítás
	Food        int  `json:"food,omitempty"`
	Pack        int  `json:"pack,omitempty"`
	TotalEst    bool `json:"total_est,omitempty"`    // a szövegből becsült ár
	TotalManual bool `json:"total_manual,omitempty"` // a személyzet adta meg – nem számoljuk újra
	// Teszt / saját rendelés: a statisztikákból kimarad
	Test        bool `json:"test,omitempty"`
	TestAuto    bool `json:"test_auto,omitempty"`    // automatikusan jelölve (pl. „teszt” a névben)
	TestChecked bool `json:"test_checked,omitempty"` // kézzel eldöntve – az automatika nem írja felül
}

const modePickup = "Személyes átvétel"
const modeDelivery = "Kiszállítás"

type Store struct {
	mu       sync.Mutex
	path     string
	Orders   []*Order `json:"orders"`
	NextID   int      `json:"next_id"`
	PriceVer int      `json:"price_ver,omitempty"` // a régi rendelések árát ennyiedik becslővel számoltuk
}

func openStore(path string) (*Store, error) {
	s := &Store{path: path, NextID: 1}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, s); err != nil {
		return nil, fmt.Errorf("a rendelesek.json sérült: %v", err)
	}
	if s.NextID < 1 {
		s.NextID = 1
	}
	return s, nil
}

// saveLocked: hívó tartja a zárat.
func (s *Store) saveLocked() {
	if s.path == "" { // memóriában tartott tár (tesztek): nincs mit menteni
		return
	}
	data, _ := json.MarshalIndent(s, "", " ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		log.Printf("MENTÉSI HIBA: %v", err)
		return
	}
	if err := os.Rename(tmp, s.path); err != nil {
		log.Printf("MENTÉSI HIBA: %v", err)
	}
}
