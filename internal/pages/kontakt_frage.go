package pages

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/kontaktantwort"
	"github.com/mdopp/homes/internal/texte"
)

// KontaktFrage is the one page of Homes without a login (owner
// 2026-11-07): the link a person sent one of their contacts,
// https://kontakt.<domain>/<token>. homes.<domain> stays wholly behind
// Authelia; this page has a host of its own (Laib's homes route
// `kontakt`, auth none, Caddy limiting the rate and the body) and a
// listener of its own (HOMES_KONTAKT_LISTEN) on which Homes registers
// nothing but this page (KontaktServer): a routing mistake on the public
// host reaches no other Homes route. It shows that contact's own fields
// and nothing of the house, takes one answer and is dead then, or after
// 14 days. A token that is no link, one answered, one over and one never
// made all get the same page; requests are limited per address and in
// all here too; the answer goes into kontaktantwort's store, never into
// an address book. No secret and no Remote-User is read here.

// KontaktServer is everything the public listener serves: the page at
// /<token>, and a robots.txt and a favicon that say nothing.
func KontaktServer(cfg StartConfig) http.Handler {
	page := KontaktFrage(cfg)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("User-agent: *\nDisallow: /\n"))
		case "/favicon.ico":
			w.WriteHeader(http.StatusNoContent)
		default:
			page(w, r)
		}
	})
}

// The limits: per client address a burst of fragePro and one more every
// frageTakt; for everyone together fragAlle, one more a second.
const (
	fragePro   = 10
	frageTakt  = 30 * time.Second
	frageAlle  = 120
	frageBytes = 32 << 10 // a form of 12 numbers, mails and addresses is a few KB
)

// drossel is a token bucket per address, and one for all.
type drossel struct {
	mu   sync.Mutex
	je   map[string]*eimer
	alle eimer
	now  func() time.Time
}

type eimer struct {
	voll float64
	am   time.Time
}

func (e *eimer) nimm(now time.Time, cap float64, takt time.Duration) bool {
	if e.am.IsZero() {
		e.voll = cap
	}
	e.voll = min(cap, e.voll+now.Sub(e.am).Seconds()/takt.Seconds())
	e.am = now
	if e.voll < 1 {
		return false
	}
	e.voll--
	return true
}

func (d *drossel) darf(ip string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	if len(d.je) > 10000 { // an address that waited long enough is full again: forget it
		for k, e := range d.je {
			if now.Sub(e.am) > fragePro*frageTakt {
				delete(d.je, k)
			}
		}
	}
	e := d.je[ip]
	if e == nil {
		e = &eimer{}
		d.je[ip] = e
	}
	return e.nimm(now, fragePro, frageTakt) && d.alle.nimm(now, frageAlle, time.Second)
}

// clientIP is the address Caddy saw (it sets X-Forwarded-For to it, the
// client's own value dropped), else the connection's.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[len(parts)-1])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// fremd is a POST another site sent: the browser says so, or the Origin
// is not this host. The token in the address is the capability; this
// keeps a page elsewhere from posting with it.
func fremd(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site != "same-origin"
	}
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err != nil || u.Host != r.Host
	}
	return false
}

// KontaktFrage is /<token>: GET the form, POST the answer.
func KontaktFrage(cfg StartConfig) http.HandlerFunc {
	d := &drossel{je: map[string]*eimer{}, now: time.Now}
	if cfg.Now != nil {
		d.now = cfg.Now
	}
	return func(w http.ResponseWriter, r *http.Request) {
		nonce := frageNonce()
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Set("Referrer-Policy", "no-referrer") // the token is in the address
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; script-src 'nonce-"+nonce+"'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
			h.Set("Allow", "GET, HEAD, POST")
			frageSeite(w, http.StatusMethodNotAllowed, nonce, wort(texte.FrageUngueltig), "<p>"+wort(texte.FrageUngueltigText)+"</p>")
			return
		}
		if !d.darf(clientIP(r)) {
			h.Set("Retry-After", "60")
			frageSeite(w, http.StatusTooManyRequests, nonce, wort(texte.FrageZuViele), "<p>"+wort(texte.FrageZuVieleText)+"</p>")
			return
		}
		token := strings.TrimPrefix(r.URL.Path, "/")
		ungueltig := func() {
			frageSeite(w, http.StatusNotFound, nonce, wort(texte.FrageUngueltig), "<p>"+wort(texte.FrageUngueltigText)+"</p>")
		}
		if cfg.Antworten == nil || r.URL.RawQuery != "" {
			ungueltig()
			return
		}
		f, err := cfg.Antworten.Zum(token)
		if err != nil {
			ungueltig()
			return
		}
		if r.Method != http.MethodPost {
			frageSeite(w, http.StatusOK, nonce, wort(texte.FrageTitel), frageForm(token, f.Vorher, f.Bis, ""))
			return
		}
		if fremd(r) {
			ungueltig()
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, frageBytes)
		if err := r.ParseForm(); err != nil {
			frageSeite(w, http.StatusRequestEntityTooLarge, nonce, wort(texte.FrageTitel), frageForm(token, f.Vorher, f.Bis, texte.T(texte.FrageZuGross)))
			return
		}
		nachher, ok := frageNachher(r)
		if !ok {
			frageSeite(w, http.StatusOK, nonce, wort(texte.FrageTitel), frageForm(token, nachher, f.Bis, texte.T(texte.FrageDatum)))
			return
		}
		as := kontaktantwort.Vergleich(f.Vorher, nachher)
		var hinweise []string
		fetch(r, func(ctx context.Context) {
			hinweise = kontaktantwort.Pruefung{Land: cfg.PhoneCountry, DNS: cfg.DNS, Now: time.Now()}.Pruefen(ctx, f.Vorher, nachher, as)
		})
		_, err = cfg.Antworten.Beantworten(token, kontaktantwort.Antwort{Bestaetigt: len(as) == 0, Aenderungen: as, Hinweise: hinweise})
		switch {
		case errors.Is(err, kontaktantwort.ErrWeg):
			ungueltig()
			return
		case err != nil:
			log.Printf("homes: kontakt-frage %s: not kept: %v", f.ID, err)
			frageSeite(w, http.StatusOK, nonce, wort(texte.FrageTitel), frageForm(token, nachher, f.Bis, texte.T(texte.FrageNicht)))
			return
		}
		log.Printf("homes: kontakt-frage %s answered: %d changes", f.ID, len(as))
		frageSeite(w, http.StatusOK, nonce, wort(texte.FrageDanke), "<p>"+wort(texte.FrageDankeText)+"</p>")
	}
}

func frageNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawStdEncoding.EncodeToString(b)
}

// frageNachher is the form as fields, in the places they were shown;
// false when the birthday is not a day (the form again, nothing kept).
func frageNachher(r *http.Request) (kontaktantwort.Felder, bool) {
	f := kontaktantwort.Felder{Vorname: r.PostFormValue("vorname"), Nachname: r.PostFormValue("nachname"), Mails: r.PostForm["mail"],
		Firma: r.PostFormValue("firma"), Position: r.PostFormValue("position")}
	at := func(vs []string, i int) string {
		if i < len(vs) {
			return vs[i]
		}
		return ""
	}
	tel, art := r.PostForm["tel"], r.PostForm["tel-art"]
	for i := range min(len(tel), kontaktantwort.GrenzeJeArt) {
		f.Telefone = append(f.Telefone, kontaktantwort.Telefon{Nummer: tel[i], Art: at(art, i)})
	}
	str, plz, ort, land := r.PostForm["adr-strasse"], r.PostForm["adr-plz"], r.PostForm["adr-ort"], r.PostForm["adr-land"]
	for i := range min(len(str), kontaktantwort.GrenzeJeArt) {
		f.Adressen = append(f.Adressen, kontaktantwort.Adresse{Strasse: str[i], PLZ: at(plz, i), Ort: at(ort, i), Land: at(land, i)})
	}
	f = kontaktantwort.Sauber(f)
	geb, ok := tag(r.PostFormValue("geb-tag"), r.PostFormValue("geb-monat"), r.PostFormValue("geb-jahr"))
	f.Geburtstag = geb
	return f, ok
}

// tag is day, month and the year if given as JJJJ-MM-TT or --MM-TT; all
// three empty is none; false when they are no day.
func tag(t, m, j string) (string, bool) {
	t, m, j = strings.TrimSpace(t), strings.TrimSpace(m), strings.TrimSpace(j)
	if t == "" && m == "" && j == "" {
		return "", true
	}
	ti, err1 := strconv.Atoi(t)
	mi, err2 := strconv.Atoi(m)
	s := fmt.Sprintf("--%02d-%02d", mi, ti)
	if j != "" {
		ji, err := strconv.Atoi(j)
		if err != nil || len(j) != 4 {
			return "", false
		}
		s = fmt.Sprintf("%04d-%02d-%02d", ji, mi, ti)
	}
	return s, err1 == nil && err2 == nil && caldav.ValidBirthday(s)
}

// tagTeile is a stored birthday as the form's day, month and year.
func tagTeile(s string) (t, m, j string) {
	y, md, ok := strings.Cut(strings.TrimPrefix(s, "--"), "-")
	if strings.HasPrefix(s, "--") {
		md, y, ok = s[2:], "", true
	}
	mo, d, ok2 := strings.Cut(md, "-")
	if !ok || !ok2 {
		return "", "", ""
	}
	return strings.TrimLeft(d, "0"), strings.TrimLeft(mo, "0"), y
}

// frageForm is the form (owner 2026-11-07: first and last name, numbers
// with their kind, e-mail, address, birthday with the year optional,
// company and position): the fields as they are, one empty to add of the
// lists, and the two ways to send.
func frageForm(token string, f kontaktantwort.Felder, bis time.Time, problem string) string {
	var b strings.Builder
	in := func(name, label, value, typ, extra string) {
		b.WriteString(`<label>` + html.EscapeString(label) + `<input name=` + name + ` type=` + typ + ` value="` + html.EscapeString(value) + `" maxlength=200 ` + extra + `></label>`)
	}
	auf := func(legend string) { b.WriteString(`<fieldset><legend>` + html.EscapeString(legend) + `</legend>`) }
	zu := func() { b.WriteString(`</fieldset>`) }
	b.WriteString(`<p>` + wort(texte.FrageHinweis) + `</p>` + gesagt("", problem))
	b.WriteString(`<form method=post action="/` + html.EscapeString(token) + `" autocomplete=off>`)
	auf(texte.T(texte.ArtName))
	in("vorname", texte.T(texte.ArtVorname), f.Vorname, "text", "autocomplete=given-name")
	in("nachname", texte.T(texte.ArtNachname), f.Nachname, "text", "autocomplete=family-name")
	zu()
	auf(texte.T(texte.FrageNummern))
	for _, p := range append(f.Telefone, kontaktantwort.Telefon{}) {
		label := texte.T(texte.ArtTelefon)
		if p.Nummer == "" {
			label = texte.T(texte.FrageWeitereNummer)
		}
		b.WriteString(`<div class=nummer>`)
		in("tel", label, p.Nummer, "tel", "inputmode=tel")
		b.WriteString(`<label>` + wort(texte.FrageNummerArt) + `<select name=tel-art><option value="">–</option>`)
		for _, a := range kontaktantwort.Arten {
			sel := ""
			if a == p.Art || (p.Art == "" && p.Nummer == "" && a == "mobil") {
				sel = " selected"
			}
			b.WriteString(`<option value=` + a + sel + `>` + wort(nummerArt[a]) + `</option>`)
		}
		b.WriteString(`</select></label></div>`)
	}
	zu()
	auf(texte.T(texte.ArtMail))
	for _, m := range append(f.Mails, "") {
		label := texte.T(texte.ArtMail)
		if m == "" {
			label = texte.T(texte.FrageWeitereMail)
		}
		in("mail", label, m, "email", "inputmode=email")
	}
	zu()
	for i, a := range append(f.Adressen, kontaktantwort.Adresse{}) {
		legend := texte.T(texte.ArtAdresse)
		if a.Leer() && i > 0 {
			legend = texte.T(texte.FrageWeitereAdresse)
		}
		auf(legend)
		in("adr-strasse", texte.T(texte.FrageStrasse), a.Strasse, "text", "autocomplete=street-address")
		b.WriteString(`<div class=zeile>`)
		in("adr-plz", texte.T(texte.FragePLZ), a.PLZ, "text", "inputmode=numeric autocomplete=postal-code")
		in("adr-ort", texte.T(texte.FrageOrt), a.Ort, "text", "autocomplete=address-level2")
		b.WriteString(`</div>`)
		in("adr-land", texte.T(texte.FrageLand), a.Land, "text", "autocomplete=country-name")
		zu()
	}
	auf(texte.T(texte.ArtGeburtstag))
	t, m, j := tagTeile(f.Geburtstag)
	b.WriteString(`<div class=tag>`)
	in("geb-tag", texte.T(texte.FrageTag), t, "text", "inputmode=numeric maxlength=2")
	in("geb-monat", texte.T(texte.FrageMonat), m, "text", "inputmode=numeric maxlength=2")
	in("geb-jahr", texte.T(texte.FrageJahr), j, "text", "inputmode=numeric maxlength=4")
	b.WriteString(`</div>`)
	zu()
	auf(texte.T(texte.FrageBeruf))
	in("firma", texte.T(texte.ArtFirma), f.Firma, "text", "autocomplete=organization")
	in("position", texte.T(texte.ArtPosition), f.Position, "text", "autocomplete=organization-title")
	zu()
	b.WriteString(`<p class=k>` + wort(texte.FrageLoeschen) + `</p>`)
	b.WriteString(`<p class=knoepfe><button name=aktion value=senden data-laeuft="` + wort(texte.FrageSendet) + `">` + wort(texte.FrageSenden) + `</button>` +
		`<button name=aktion value=stimmt class=zweit data-laeuft="` + wort(texte.FrageSendet) + `">` + wort(texte.FrageStimmt) + `</button></p></form>`)
	b.WriteString(`<p class=k>` + html.EscapeString(fmt.Sprintf(texte.T(texte.FrageBis), bis.Local().Format("02.01.2006"))) + `</p>`)
	return b.String()
}

// frageSeite is the page: its own calm frame, no menu, no login, nothing
// fetched from elsewhere (the pages' font and icons are behind the login).
func frageSeite(w http.ResponseWriter, code int, nonce, title, body string) {
	var b strings.Builder
	b.WriteString(`<!doctype html><html lang=de><meta charset=utf-8><meta name=viewport content="width=device-width,initial-scale=1"><meta name=color-scheme content="light dark">` +
		`<meta name=robots content="noindex,nofollow"><title>` + title + `</title>` + frageStyle + `<main><h1>` + title + `</h1>` + body + `</main>` +
		`<script nonce="` + nonce + `">document.addEventListener('submit',function(e){var b=e.submitter;if(!b||!b.hasAttribute('data-laeuft'))return;` +
		`setTimeout(function(){b.textContent=b.getAttribute('data-laeuft');document.querySelectorAll('button').forEach(function(x){x.disabled=true})},0)})</script>`)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	w.Write([]byte(b.String()))
}

const frageStyle = `<style>:root{color-scheme:light;--bg:#f6f7f9;--card:#fff;--ink:#111827;--muted:#5b6474;--line:#d9dee6;--accent:#c2560c;--warn:#a15c00}
@media (prefers-color-scheme:dark){:root{color-scheme:dark;--bg:#0c0c0e;--card:#16161c;--ink:#f2f2f5;--muted:#a0a6b4;--line:#2a2a33;--accent:#f97316;--warn:#f59e0b}}
*{box-sizing:border-box}body{margin:0;background:var(--bg);color:var(--ink);font:17px/1.5 system-ui,-apple-system,"Segoe UI",Roboto,sans-serif}
main{max-width:32rem;margin:0 auto;padding:28px 16px 48px}h1{font-size:1.5rem;line-height:1.25;margin:0 0 12px}p{margin:0 0 14px}.k{color:var(--muted);font-size:.92rem}
fieldset{margin:0 0 14px;padding:14px;border:0;border-radius:16px;background:var(--card)}legend{float:left;width:100%;margin:0 0 8px;font-weight:600}
label{display:block;margin:0 0 10px;font-size:.88rem;color:var(--muted)}label:last-child{margin-bottom:0}
input,select{display:block;width:100%;margin-top:4px;padding:12px;font:inherit;color:var(--ink);background:var(--bg);border:1px solid var(--line);border-radius:12px}
input:focus{outline:2px solid var(--accent);outline-offset:1px}.zeile{display:grid;grid-template-columns:7rem 1fr;gap:10px}
.nummer{display:grid;grid-template-columns:1fr 9rem;gap:10px;margin-bottom:6px}.tag{display:grid;grid-template-columns:1fr 1fr 1.4fr;gap:10px}
.knoepfe{display:flex;flex-direction:column;gap:10px;margin-top:20px}
button{min-height:48px;padding:0 18px;font:inherit;font-weight:600;border:0;border-radius:14px;background:var(--accent);color:#fff}button.zweit{background:var(--card);color:var(--ink);border:1px solid var(--line)}
button[disabled]{opacity:.6}.achtung{padding:12px 14px;border-radius:12px;border:1px solid var(--warn);color:var(--warn);margin:0 0 14px}.achtung p{margin:0}</style>`
