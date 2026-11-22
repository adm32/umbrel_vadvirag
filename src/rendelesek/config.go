// Vadvirág Rendelések – beállítások: a Config szerkezet, alapértékek, a config.json betöltése és mentése.

package main

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
)

// ---------------------------------------------------------------- beállítások

type SMTPConf struct {
	Szerver     string `json:"szerver"`
	Port        int    `json:"port"`
	Felhasznalo string `json:"felhasznalo"`
	Jelszo      string `json:"jelszo"`
	FeladoNev   string `json:"felado_neve"`
	FeladoEmail string `json:"felado_email"`
}

type WixConf struct {
	APIKulcs      string `json:"api_kulcs"`
	SiteID        string `json:"site_id"`
	FormID        string `json:"form_id"`
	APICim        string `json:"api_cim"`
	LekerdezesSec int    `json:"lekerdezes_masodperc"`
}

type Config struct {
	Port           int            `json:"port"`
	Jelszo         string         `json:"jelszo"`
	EtteremNeve    string         `json:"etterem_neve"`
	EtteremTelefon string         `json:"etterem_telefon"`
	Titok          string         `json:"belso_titok"`
	WebhookKulcs   string         `json:"webhook_kulcs"`
	VapidPrivat    string         `json:"vapid_privat"`
	VapidPublikus  string         `json:"vapid_publikus"`
	SMTP           SMTPConf       `json:"email"`
	Wix            WixConf        `json:"wix"`
	Dijak          map[string]int `json:"szallitasi_dijak"`
	DijakVerzio    int            `json:"szallitasi_dijak_verzio"` // ha kisebb, mint a beépített tábla verziója, frissítjük
	Menetidok      map[string]int `json:"menetidok_perc"`          // autós út percben településenként
	IdoOpciok      []int          `json:"elkeszulesi_percek"`
	Okok           []string       `json:"elutasitas_okai"`
	// Online rendelés szüneteltetése (pl. zárva vagy túl sok a munka) – a vendég ezt látja.
	OnlineSzunetel bool `json:"online_szunetel,omitempty"`
	// Elfogyott: az előrendelhető alkalmi ajánlatok (elorendeles.json id) – ami itt true, az nem látszik a weboldalon
	Elfogyott map[string]bool `json:"elfogyott,omitempty"`
	// KorabbanZarva: ezen a napon (YYYY-MM-DD, budapesti idő) korábban bezártunk – a weboldal zárva mutat, nem lehet rendelni. Másnap magától lejár.
	KorabbanZarva string `json:"korabban_zarva,omitempty"`
	// Ntfy: értesítés az ntfy appba (téma, szerver, mikről)
	Ntfy NtfyConf `json:"ntfy,omitempty"`
	// Saját telefonszámok kódja (ownKey): az innen jövő rendelések tesztnek számítanak
	SajatTelefonok []string `json:"sajat_telefonok,omitempty"`
	OnlineUzenet   string   `json:"online_uzenet,omitempty"`
}

func randStr(n int, alphabet string) string {
	b := make([]byte, n)
	rand.Read(b)
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out)
}

func defaultConfig() Config {
	return Config{
		Port:           8080,
		Jelszo:         randStr(8, "abcdefghjkmnpqrstuvwxyz23456789"),
		EtteremNeve:    "Vadvirág Étterem",
		EtteremTelefon: "+36 20 339 5099",
		Titok:          randStr(40, "abcdef0123456789"),
		WebhookKulcs:   randStr(24, "abcdefghjkmnpqrstuvwxyz23456789"),
		SMTP:           SMTPConf{Szerver: "", Port: 587, FeladoNev: "Vadvirág Étterem"},
		Wix: WixConf{
			SiteID:        "a2cb67a5-41c9-4cfb-bb42-2282a8a16198",
			FormID:        "eacf2b73-6d4f-420a-98a8-c51d026da098",
			APICim:        "https://www.wixapis.com",
			LekerdezesSec: 20,
		},
		Dijak:       defaultFees(),
		DijakVerzio: feeTableVersion,
		Menetidok:   defaultDriveTimes(),
		IdoOpciok:   []int{15, 20, 30, 45, 60, 90},
		Okok: []string{
			"Sajnos már túl késő van, a konyha bezárt",
			"Az egyik étel elfogyott",
			"Ide most nem tudunk kiszállítani",
			"Jelenleg nagyon sok a rendelés",
		},
	}
}

func loadConfig(path string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, saveConfig(path, cfg)
	}
	if err != nil {
		return cfg, err
	}
	// a mentett tábla ne keveredjen a beépítettel, és a régi (verzió nélküli) fájl frissüljön
	cfg.Dijak, cfg.DijakVerzio, cfg.Menetidok = nil, 0, nil
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("a beallitasok.json hibás (%v) – javítsd, vagy töröld, hogy újra létrejöjjön", err)
	}
	if cfg.Port == 0 {
		cfg.Port = 8080
	}
	if cfg.Wix.LekerdezesSec < 5 {
		cfg.Wix.LekerdezesSec = 20
	}
	if cfg.Wix.APICim == "" {
		cfg.Wix.APICim = "https://www.wixapis.com"
	}
	if cfg.Titok == "" {
		cfg.Titok = randStr(40, "abcdef0123456789")
	}
	if cfg.DijakVerzio < feeTableVersion || len(cfg.Dijak) == 0 {
		log.Printf("Szállítási díjak frissítése: %d település (tábla v%d)", len(defaultFees()), feeTableVersion)
		cfg.Dijak, cfg.DijakVerzio, cfg.Menetidok = defaultFees(), feeTableVersion, defaultDriveTimes()
		if err := saveConfig(path, cfg); err != nil {
			log.Printf("a frissített díjtábla mentése nem sikerült: %v", err)
		}
	}
	return cfg, nil
}

func saveConfig(path string, cfg Config) error {
	data, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(path, data, 0o600)
}
