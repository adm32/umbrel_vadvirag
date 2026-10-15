package main

// Web Push értesítések (VAPID + aes128gcm, RFC 8291 / 8292) – csak a Go standard könyvtárral.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

var b64 = base64.RawURLEncoding

type PushSub struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Device  string    `json:"device,omitempty"`
	Created time.Time `json:"created"`
}

type Pusher struct {
	mu      sync.Mutex
	path    string
	subs    []PushSub
	priv    *ecdsa.PrivateKey
	pubB64  string
	subject string
}

var push *Pusher

// ensureVapid: ha nincs VAPID kulcs a beállításokban, létrehoz egyet. true = módosult.
func ensureVapid(cfg *Config) bool {
	if cfg.VapidPrivat != "" && cfg.VapidPublikus != "" {
		return false
	}
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	d := make([]byte, 32)
	k.D.FillBytes(d)
	cfg.VapidPrivat = b64.EncodeToString(d)
	cfg.VapidPublikus = b64.EncodeToString(elliptic.Marshal(elliptic.P256(), k.X, k.Y)) //nolint
	return true
}

func newPusher(cfg Config, path string) (*Pusher, error) {
	d, err := b64.DecodeString(cfg.VapidPrivat)
	if err != nil || len(d) != 32 {
		return nil, errors.New("hibás vapid_privat kulcs")
	}
	priv := new(ecdsa.PrivateKey)
	priv.Curve = elliptic.P256()
	priv.D = new(big.Int).SetBytes(d)
	priv.X, priv.Y = elliptic.P256().ScalarBaseMult(d) //nolint
	subj := "mailto:" + cfg.SMTP.FeladoEmail
	if cfg.SMTP.FeladoEmail == "" {
		subj = "mailto:rendeles@vadviragetterem.com"
	}
	p := &Pusher{path: path, priv: priv, pubB64: cfg.VapidPublikus, subject: subj}
	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &p.subs)
	}
	return p, nil
}

func (p *Pusher) saveLocked() {
	data, _ := json.MarshalIndent(p.subs, "", " ")
	os.WriteFile(p.path, data, 0o600)
}

func (p *Pusher) add(s PushSub) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, x := range p.subs {
		if x.Endpoint == s.Endpoint {
			p.subs[i] = s
			p.saveLocked()
			return
		}
	}
	p.subs = append(p.subs, s)
	p.saveLocked()
}

func (p *Pusher) remove(endpoint string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := p.subs[:0]
	for _, x := range p.subs {
		if x.Endpoint != endpoint {
			out = append(out, x)
		}
	}
	p.subs = out
	p.saveLocked()
}

func (p *Pusher) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.subs)
}

// broadcast: mindenkinek elküldi; a lejárt feliratkozásokat törli. Visszatér: sikeres darabszám.
func (p *Pusher) broadcast(payload any) int {
	body, _ := json.Marshal(payload)
	p.mu.Lock()
	subs := append([]PushSub(nil), p.subs...)
	p.mu.Unlock()
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for _, s := range subs {
		wg.Add(1)
		go func(s PushSub) {
			defer wg.Done()
			code, err := p.send(s, body)
			switch {
			case err == nil && code < 300:
				mu.Lock()
				ok++
				mu.Unlock()
			case code == 404 || code == 410:
				log.Printf("Értesítés: lejárt feliratkozás törölve (%s)", s.Device)
				p.remove(s.Endpoint)
			default:
				log.Printf("Értesítés hiba (%s): %v %d", s.Device, err, code)
			}
		}(s)
	}
	wg.Wait()
	return ok
}

func hkdfExtract(salt, ikm []byte) []byte {
	m := hmac.New(sha256.New, salt)
	m.Write(ikm)
	return m.Sum(nil)
}

func hkdfExpand(prk, info []byte, n int) []byte {
	m := hmac.New(sha256.New, prk)
	m.Write(info)
	m.Write([]byte{1})
	return m.Sum(nil)[:n]
}

func encryptPayload(s PushSub, plaintext []byte) ([]byte, error) {
	uaPubRaw, err := b64.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	if err != nil {
		return nil, err
	}
	authSecret, err := b64.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	if err != nil {
		return nil, err
	}
	uaPub, err := ecdh.P256().NewPublicKey(uaPubRaw)
	if err != nil {
		return nil, err
	}
	asPriv, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	asPub := asPriv.PublicKey().Bytes()
	secret, err := asPriv.ECDH(uaPub)
	if err != nil {
		return nil, err
	}
	keyInfo := append(append([]byte("WebPush: info\x00"), uaPubRaw...), asPub...)
	ikm := hkdfExpand(hkdfExtract(authSecret, secret), keyInfo, 32)
	salt := make([]byte, 16)
	rand.Read(salt)
	prk := hkdfExtract(salt, ikm)
	cek := hkdfExpand(prk, []byte("Content-Encoding: aes128gcm\x00"), 16)
	nonce := hkdfExpand(prk, []byte("Content-Encoding: nonce\x00"), 12)
	blk, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(blk)
	ct := gcm.Seal(nil, nonce, append(append([]byte{}, plaintext...), 2), nil)
	var buf bytes.Buffer
	buf.Write(salt)
	binary.Write(&buf, binary.BigEndian, uint32(4096))
	buf.WriteByte(byte(len(asPub)))
	buf.Write(asPub)
	buf.Write(ct)
	return buf.Bytes(), nil
}

func (p *Pusher) vapidJWT(aud string) (string, error) {
	h := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	c, _ := json.Marshal(map[string]any{"aud": aud, "exp": time.Now().Add(12 * time.Hour).Unix(), "sub": p.subject})
	signing := h + "." + b64.EncodeToString(c)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, p.priv, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

func (p *Pusher) send(s PushSub, payload []byte) (int, error) {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" {
		return 0, fmt.Errorf("hibás végpont")
	}
	body, err := encryptPayload(s, payload)
	if err != nil {
		return 0, err
	}
	jwt, err := p.vapidJWT(u.Scheme + "://" + u.Host)
	if err != nil {
		return 0, err
	}
	req, _ := http.NewRequest("POST", s.Endpoint, bytes.NewReader(body))
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", "3600")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+p.pubB64)
	resp, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("push szerver: %s", resp.Status)
	}
	return resp.StatusCode, nil
}

func (a *App) notifyNewOrder(o Order) {
	if push == nil || push.count() == 0 {
		return
	}
	title := fmt.Sprintf("Új rendelés #%d – %s", o.ID, o.Name)
	body := o.Mode
	if o.Mode == modeDelivery && o.City != "" {
		body += " · " + o.City
	}
	items := strings.ReplaceAll(strings.TrimSpace(o.Items), "\n", ", ")
	if len([]rune(items)) > 120 {
		items = string([]rune(items)[:120]) + "…"
	}
	if items != "" {
		body += "\n" + items
	}
	push.broadcast(map[string]any{"title": title, "body": body, "tag": fmt.Sprintf("order-%d", o.ID), "url": "/"})
}

// ---- HTTP

func (a *App) handlePushKey(w http.ResponseWriter, r *http.Request) {
	n := 0
	if push != nil {
		n = push.count()
	}
	writeJSON(w, 200, map[string]any{"key": a.conf().VapidPublikus, "devices": n})
}

func (a *App) handlePushSub(w http.ResponseWriter, r *http.Request) {
	var s PushSub
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&s); err != nil || s.Endpoint == "" || s.Keys.P256dh == "" || s.Keys.Auth == "" {
		writeJSON(w, 400, map[string]string{"error": "hibás feliratkozás"})
		return
	}
	if !strings.HasPrefix(s.Endpoint, "https://") {
		writeJSON(w, 400, map[string]string{"error": "hibás végpont"})
		return
	}
	s.Created = time.Now()
	if len(s.Device) > 80 {
		s.Device = s.Device[:80]
	}
	push.add(s)
	log.Printf("Értesítések bekapcsolva: %s (összesen %d eszköz)", s.Device, push.count())
	writeJSON(w, 200, map[string]any{"ok": true, "devices": push.count()})
}

func (a *App) handlePushUnsub(w http.ResponseWriter, r *http.Request) {
	var s struct {
		Endpoint string `json:"endpoint"`
	}
	json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&s)
	if s.Endpoint != "" {
		push.remove(s.Endpoint)
	}
	writeJSON(w, 200, map[string]any{"ok": true, "devices": push.count()})
}

func (a *App) handlePushTest(w http.ResponseWriter, r *http.Request) {
	n := push.broadcast(map[string]any{"title": "Teszt értesítés", "body": "Ha ezt látod, az értesítések működnek ezen az eszközön. ✓", "tag": "test", "url": "/"})
	writeJSON(w, 200, map[string]any{"ok": true, "sent": n, "devices": push.count()})
}
