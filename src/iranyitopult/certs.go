package main

// A konténer "üres" (FROM scratch), nincs benne a rendszer tanúsítványtára, ezért
// HTTPS-en (vadviragetterem.com ellenőrzése, ntfy.sh értesítések) nem tudná ellenőrizni
// a szervereket. A Mozilla gyökértanúsítványait a programba fordítjuk (ugyanaz, mint a
// Rendelések appban); ha a gépen van saját tár, az elsőbbséget kap.

import (
	"crypto/x509"
	_ "embed"
	"log"
)

//go:embed certs/roots.pem
var rootsPEM []byte

func init() {
	p := x509.NewCertPool()
	if p.AppendCertsFromPEM(rootsPEM) {
		x509.SetFallbackRoots(p)
	} else {
		log.Print("figyelem: a beépített tanúsítványok nem tölthetők be")
	}
}
