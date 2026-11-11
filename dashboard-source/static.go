package main

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"sync"
)

// The pages built into the program are sent gzipped (they are 60–120 kB of HTML, CSS and
// script, about a quarter of that compressed), and with an ETag where the browser may keep a
// copy, so an unchanged page costs a 304 instead of a full download. The compressed copy is
// made once per file and kept in memory.
type staticCopy struct {
	gz   []byte
	etag string
}

var staticCopies sync.Map // name -> *staticCopy

func sendStatic(w http.ResponseWriter, r *http.Request, name string, data []byte, ctype, cache string) {
	v, ok := staticCopies.Load(name)
	if !ok {
		sum := sha256.Sum256(data)
		var b bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
		zw.Write(data)
		zw.Close()
		v, _ = staticCopies.LoadOrStore(name, &staticCopy{gz: b.Bytes(), etag: `"` + hex.EncodeToString(sum[:8]) + `"`})
	}
	c := v.(*staticCopy)
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Cache-Control", cache)
	h.Add("Vary", "Accept-Encoding")
	if cache != "no-store" {
		h.Set("ETag", c.etag)
		if m := r.Header.Get("If-None-Match"); m != "" && strings.Contains(m, c.etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	body := data
	if len(c.gz) < len(data) && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		body = c.gz
	}
	h.Set("Content-Length", strconv.Itoa(len(body)))
	if r.Method != http.MethodHead {
		w.Write(body)
	}
}
