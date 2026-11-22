// Vadvirág Rendelések (rendeléskezelő) – a program belépési pontja.
//
// main() itt indul: beállítás betöltése, a rendelések megnyitása, a szerverek (kezelőprogram és
// nyilvános rendelőoldal a PORT-on, belső adatcsatorna a DASH_ADDR-en) és a háttérfolyamatok.
// Melyik fájlban mi van: ../../README.md („Hol mit találsz”).
package main

import (
	"embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

//go:embed web/*
var webFS embed.FS

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

func fatal(container bool, a ...any) {
	fmt.Println(append([]any{"HIBA:"}, a...)...)
	if !container {
		fmt.Println("Nyomj Entert a kilépéshez.")
		fmt.Scanln()
	}
	os.Exit(1)
}

func main() {
	dirFlag := flag.String("dir", "", "adatmappa (alapból a program mappája, vagy a DATA_DIR környezeti változó)")
	noBrowser := flag.Bool("nobrowser", false, "ne nyissa meg a böngészőt")
	flag.Parse()

	// Dockerben (Umbrel) a DATA_DIR mondja meg, hova mentsünk.
	container := os.Getenv("DATA_DIR") != ""
	dir := *dirFlag
	if dir == "" {
		dir = os.Getenv("DATA_DIR")
	}
	if dir == "" {
		if exe, err := os.Executable(); err == nil {
			dir = filepath.Dir(exe)
		} else {
			dir = "."
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fatal(container, "nem hozható létre az adatmappa:", err)
	}
	cfgPath := filepath.Join(dir, "beallitasok.json")
	_, existed := os.Stat(cfgPath)
	cfg, err := loadConfig(cfgPath)
	if err != nil {
		fatal(container, err)
	}
	if existed != nil && container {
		// Umbrelen nincs konzolablak: a jelszót az első megnyitáskor a böngészőben kell megadni.
		cfg.Jelszo = ""
		if err := saveConfig(cfgPath, cfg); err != nil {
			fatal(container, err)
		}
	}
	if p, err := strconv.Atoi(os.Getenv("PORT")); err == nil && p > 0 {
		cfg.Port = p
	}
	st, err := openStore(filepath.Join(dir, "rendelesek.json"))
	if err != nil {
		fatal(container, err)
	}
	if ensureVapid(&cfg) {
		if err := saveConfig(cfgPath, cfg); err != nil {
			log.Printf("Nem sikerült menteni a beállításokat: %v", err)
		}
	}
	if p, err := newPusher(cfg, filepath.Join(dir, "ertesitesek.json")); err != nil {
		log.Printf("Értesítések nem elérhetők: %v", err)
	} else {
		push = p
	}
	app := newApp(cfg, st)
	app.cfgPath, app.dataDir, app.container = cfgPath, dir, container
	go app.wixLoop()

	fmt.Println("==================================================")
	fmt.Println("  " + cfg.EtteremNeve + " – rendeléskezelő " + appVersion)
	fmt.Println("==================================================")
	local := fmt.Sprintf("http://localhost:%d", cfg.Port)
	if container {
		fmt.Println("  Adatmappa:", dir, " · belső port:", cfg.Port)
		if app.needsSetup() {
			fmt.Println("  Első indítás: nyisd meg az alkalmazást az Umbrel kezdőlapjáról, és add meg a jelszót.")
		}
	} else {
		fmt.Println("  Ezen a gépen:  ", local)
		for _, u := range lanURLs(cfg.Port) {
			fmt.Println("  Telefonról:    ", u, " (ugyanazon a Wi-Fi-n)")
		}
		if app.needsSetup() {
			fmt.Println("  Első indítás: nyisd meg a fenti címet, és add meg a jelszót.")
		} else {
			fmt.Println("  Belépési jelszó:", cfg.Jelszo, "  (módosítható a program Beállítások menüjében)")
		}
		if existed != nil {
			fmt.Println("  (Első indítás – létrejött a beallitasok.json)")
		}
		fmt.Println("  Ezt az ablakot hagyd nyitva, amíg fogadod a rendeléseket!")
	}
	fmt.Println("==================================================")

	app.startDash()
	srv := &http.Server{Handler: app.routes(), ReadHeaderTimeout: 10 * time.Second}
	// Újraindításkor a régi példány még pár pillanatig foglalhatja a portot.
	var ln net.Listener
	for i := 0; ; i++ {
		ln, err = net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
		if err == nil {
			break
		}
		if i >= 20 {
			fatal(container, "a szerver nem indul:", err, "(lehet, hogy a program már fut, vagy a", cfg.Port, "port foglalt)")
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !*noBrowser && !container {
		go func() { time.Sleep(700 * time.Millisecond); openBrowser(local) }()
	}
	if err := srv.Serve(ln); err != nil {
		fatal(container, "a szerver leállt:", err)
	}
}
