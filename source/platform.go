package main

// Platformfüggetlen segédek: beágyazott tanúsítványok és időzóna-adatok, hogy a
// program üres (scratch) Docker-képben is működjön (Umbrel), Windows-on pedig
// ugyanúgy, mint eddig.

import (
	"crypto/x509"
	_ "embed"
	_ "time/tzdata" // Europe/Budapest akkor is, ha a rendszeren nincs időzóna-adatbázis
)

// Mozilla gyökértanúsítványok (forrás: golang.org/x/crypto/x509roots/fallback).
// Csak akkor használja a Go, ha a rendszernek nincs saját tanúsítványtára.
//
//go:embed certs/roots.pem
var fallbackRootsPEM []byte

func init() {
	p := x509.NewCertPool()
	if p.AppendCertsFromPEM(fallbackRootsPEM) {
		x509.SetFallbackRoots(p)
	}
}
