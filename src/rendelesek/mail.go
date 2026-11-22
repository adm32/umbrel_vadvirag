// Vadvirág Rendelések – e-mail: a visszaigazoló levél piszkozata és küldése SMTP-n.

package main

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"mime"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------- e-mail

type MailDraft struct {
	To      string `json:"to"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

func (a *App) addressLine(o *Order) string {
	if o.Mode == modeDelivery {
		s := "Kiszállítás: " + strings.TrimSpace(o.City+", "+o.Street)
		s = strings.TrimSuffix(strings.TrimSpace(s), ",")
		if o.DeliveryFee > 0 {
			s += fmt.Sprintf(" (szállítási díj: %s Ft)", formatHUF(o.DeliveryFee))
		}
		return s
	}
	return "Átvétel: személyesen az étteremben"
}

func formatHUF(n int) string {
	s := strconv.Itoa(n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ' ')
		}
		out = append(out, c)
	}
	return string(out)
}

func (a *App) draft(o *Order) *MailDraft {
	if o.Email == "" || (o.Status != StPreparing && o.Status != StRejected) {
		return nil
	}
	name := o.Name
	if name == "" {
		name = "Vendégünk"
	}
	var subj, body string
	if o.Status == StPreparing {
		subj = fmt.Sprintf("%s – a rendelését készítjük (#%d)", a.conf().EtteremNeve, o.ID)
		body = fmt.Sprintf("Kedves %s!\n\nRendelését megkaptuk, elkezdtük készíteni.\n", name)
		if o.ETA > 0 {
			body += fmt.Sprintf("Várható elkészülési idő: kb. %d perc.\n", o.ETA)
		}
		body += "\n" + a.addressLine(o) + "\n\nAz Ön rendelése:\n" + o.Items + "\n\nKöszönjük, hogy minket választott!\n"
	} else {
		subj = fmt.Sprintf("%s – a rendelését sajnos nem tudjuk teljesíteni (#%d)", a.conf().EtteremNeve, o.ID)
		body = fmt.Sprintf("Kedves %s!\n\nSajnáljuk, de a rendelését most nem tudjuk teljesíteni.\n", name)
		if o.Reason != "" {
			body += "Ok: " + o.Reason + "\n"
		}
		body += "\nA rendelése:\n" + o.Items + "\n\nElnézést kérünk, és szeretettel várjuk legközelebb!\n"
	}
	body += "\n" + a.conf().EtteremNeve
	if a.conf().EtteremTelefon != "" {
		body += " · " + a.conf().EtteremTelefon
	}
	body += "\n"
	return &MailDraft{To: o.Email, Subject: subj, Body: body}
}

func (a *App) smtpEnabled() bool {
	return a.conf().SMTP.Szerver != "" && (a.conf().SMTP.FeladoEmail != "" || a.conf().SMTP.Felhasznalo != "")
}

func wrap76(s string) string {
	var b strings.Builder
	for len(s) > 76 {
		b.WriteString(s[:76] + "\r\n")
		s = s[76:]
	}
	b.WriteString(s)
	return b.String()
}

func sendMail(c SMTPConf, to, subject, body string) error {
	from := c.FeladoEmail
	if from == "" {
		from = c.Felhasznalo
	}
	if c.Port == 0 {
		c.Port = 587
	}
	fromHdr := from
	if c.FeladoNev != "" {
		fromHdr = mime.QEncoding.Encode("utf-8", c.FeladoNev) + " <" + from + ">"
	}
	msg := "From: " + fromHdr + "\r\n" +
		"To: " + to + "\r\n" +
		"Subject: " + mime.QEncoding.Encode("utf-8", subject) + "\r\n" +
		"Date: " + time.Now().Format(time.RFC1123Z) + "\r\n" +
		"Message-ID: <" + randStr(20, "abcdef0123456789") + "@vadvirag.local>\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\n" +
		wrap76(base64.StdEncoding.EncodeToString([]byte(body))) + "\r\n"

	addr := net.JoinHostPort(c.Szerver, strconv.Itoa(c.Port))
	d := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if c.Port == 465 {
		conn, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: c.Szerver})
	} else {
		conn, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	conn.SetDeadline(time.Now().Add(45 * time.Second))
	cl, err := smtp.NewClient(conn, c.Szerver)
	if err != nil {
		conn.Close()
		return err
	}
	defer cl.Close()
	if c.Port != 465 {
		if ok, _ := cl.Extension("STARTTLS"); ok {
			if err := cl.StartTLS(&tls.Config{ServerName: c.Szerver}); err != nil {
				return err
			}
		}
	}
	if c.Felhasznalo != "" {
		if err := cl.Auth(loginAuth(c.Felhasznalo, c.Jelszo, c.Szerver)); err != nil {
			return err
		}
	}
	if err := cl.Mail(from); err != nil {
		return err
	}
	if err := cl.Rcpt(to); err != nil {
		return err
	}
	w, err := cl.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write([]byte(msg)); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return cl.Quit()
}

// PLAIN hitelesítés, localhoston titkosítás nélkül is (teszthez), egyébként csak TLS-en.
type plainAuth struct{ user, pass, host string }

func loginAuth(user, pass, host string) smtp.Auth { return &plainAuth{user, pass, host} }
func (p *plainAuth) Start(si *smtp.ServerInfo) (string, []byte, error) {
	if !si.TLS && !(p.host == "localhost" || p.host == "127.0.0.1") {
		return "", nil, errors.New("a kapcsolat nem titkosított, a jelszót nem küldöm el")
	}
	return "PLAIN", []byte("\x00" + p.user + "\x00" + p.pass), nil
}
func (p *plainAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if more {
		return nil, errors.New("váratlan szerver válasz")
	}
	return nil, nil
}

func (a *App) emailAsync(id int) {
	a.st.mu.Lock()
	o := a.find(id)
	if o == nil {
		a.st.mu.Unlock()
		return
	}
	d := a.draft(o)
	switch {
	case d == nil:
		o.EmailState = "none"
		o.EmailErr = "nincs e-mail cím"
		a.st.saveLocked()
		a.st.mu.Unlock()
		return
	case !a.smtpEnabled():
		o.EmailState = "none"
		o.EmailErr = "az e-mail küldés nincs beállítva"
		a.st.saveLocked()
		a.st.mu.Unlock()
		return
	}
	o.EmailState = "sending"
	o.EmailErr = ""
	a.st.saveLocked()
	a.st.mu.Unlock()

	err := sendMail(a.conf().SMTP, d.To, d.Subject, d.Body)

	a.st.mu.Lock()
	defer a.st.mu.Unlock()
	if o := a.find(id); o != nil {
		o.EmailAt = time.Now()
		if err != nil {
			o.EmailState = "failed"
			o.EmailErr = err.Error()
			log.Printf("E-mail hiba (#%d): %v", id, err)
		} else {
			o.EmailState = "sent"
			o.EmailErr = ""
			log.Printf("E-mail elküldve (#%d → %s)", id, d.To)
		}
		a.st.saveLocked()
	}
}

func (a *App) find(id int) *Order {
	for _, o := range a.st.Orders {
		if o.ID == id {
			return o
		}
	}
	return nil
}
