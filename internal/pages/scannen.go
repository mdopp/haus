package pages

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mdopp/haus/internal/laib"
)

// ScannenPath is where the Dokumente page's Scannen buttons post (haus#76).
const ScannenPath = "/scannen"

// What Scannen says (haus#76, laib#568): the scan service's state in the
// family's words, never its error.
const (
	saysScanOld     = "Scannen geht auf dieser Box noch nicht: Laib kann es hier noch nicht."
	saysScanOldAd   = "Für Admins: Haus braucht Laib ab v0.20.0 und in policy.yml beim haus-Hand das Verb scan."
	saysScanSilent  = "Der Scanner antwortet gerade nicht."
	saysScanNotDone = "Das hat nicht geklappt: Der Scanner antwortet gerade nicht."
	saysScanUnset   = "Scannen ist auf dieser Box noch nicht eingerichtet. „Scannen einrichten“ legt einen Vorschlag in die Inbox; die Box richtet den Dienst ein, sobald du ihn freigibst."
	saysSetupHow    = "Ein Scanner im Netz scannt Stapel direkt von hier. Suchen findet Epson- und AirScan-Geräte im selben Netz; sonst die Adresse von Hand."
	saysFolderSetup = "Ein Scanner, der in einen Netzwerkordner scannt, wird in seinem eigenen Menü eingerichtet:"
	saysNoStacks    = "Noch kein Stapel. Was gescannt ist, steht hier mit seinen Dokumenten."
	saysStackHow    = "Eine Datei ist ein Stapel; die Box trennt die Dokumente selbst, entfernt leere Seiten und legt alles in Paperless ab."
	saysAsked       = "Im Heimnetz wurden %d Geräte direkt gefragt."
	saysNotFound    = "Kein Scanner gefunden. Ist er an und im selben Netz? Sonst unten die Adresse von Hand eintragen, etwa epson2:net:192.168.178.87."
	saysOrderLate   = "Laib hat den Auftrag angenommen, aber nach %s kein Ergebnis gemeldet. Noch einmal versuchen; sonst weiß laib diag mehr."
	maxStacks       = 5
)

// scanDoes is what the page may order: a new stack, and after a jam on,
// finish with what is there, or drop it; an admin also sets the scanner up
// (haus#77, laib#571): search, check, a probe sheet. redo is the CLI's.
var scanDoes = map[string]bool{"start": true, "weiter": true, "abschliessen": true, "verwerfen": true}
var scanSetup = map[string]bool{"suchen": true, "pruefen": true, "probe": true}

// scanWaits is how long the page waits for a setup order's result before
// it says none came (haus#80): Laib only takes the order, the service
// writes the result into its status file.
var scanWaits = map[string]time.Duration{"suchen": time.Minute, "pruefen": 30 * time.Second, "probe": 2 * time.Minute}

// scanRuns is what the page says while a setup order runs, and what its
// button says the moment it is pressed.
var scanRuns = map[string]string{"suchen": "Sucht im Netz nach Scannern …", "pruefen": "Prüft den Scanner …", "probe": "Liest eine Probeseite …"}

// scanGeraet is a SANE device as laib#571 takes it (scanimage -L's name,
// nothing that could break the unit's line).
var scanGeraet = regexp.MustCompile(`^[a-z0-9_]+:[^"'\\\n\r%$]{1,120}$`)

// what the page says after „Einrichten“: a fixed sentence per outcome, so
// no device lands in the URL (adresse's way)
var scanSaid = map[string]string{
	"vorgeschlagen":  "Vorgeschlagen. Der Vorschlag liegt in Laibs Eingang, der Owner gibt ihn dort frei; dann richtet Laib den Scanner ein und prüft ihn von selbst.",
	"geraet":         "Die Adresse nimmt Laib so nicht: etwa epson2:net:192.168.178.87, ohne Anführungszeichen, % und $.",
	"nicht-moeglich": "Das ging gerade nicht. Später noch einmal versuchen; sonst weiß laib diag mehr.",
}

// scanPerson is a name the scan service takes for its closing message
// (laib#568: box.yml people, lower case); another Remote-User gets the
// box's default.
var scanPerson = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

// Scannen is where the buttons post: one order to Laib (laib scan), then
// back to Dokumente (303, a reload sends nothing again). Only a person,
// only from Haus's own page; the closing message is for them. Haus orders
// a scan, it decides nothing about the box (haus#1).
func Scannen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Scannen geht nur mit dem Knopf auf der Seite.", http.StatusMethodNotAllowed)
			return
		}
		if cfg.Laib == nil {
			http.Redirect(w, r, DokumentePath, http.StatusSeeOther)
			return
		}
		if !fromHaus(r) {
			http.Error(w, "Scannen geht nur von Haus' eigener Seite.", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Das Formular ist nicht lesbar.", http.StatusBadRequest)
			return
		}
		do := r.PostForm.Get("tun")
		if do == "einrichten" {
			if adminOnly(w, r) {
				scanEinrichten(w, r, cfg)
			}
			return
		}
		if scanSetup[do] && !isAdmin(r) {
			http.Error(w, "Den Scanner richtet ein Admin ein.", http.StatusForbidden)
			return
		}
		if !scanDoes[do] && !scanSetup[do] {
			http.Error(w, "Das lässt sich hier nicht tun.", http.StatusBadRequest)
			return
		}
		cfg.Usage.Count("scannen." + do) // what was pressed, never by whom (haus#50)
		person := r.Header.Get("Remote-User")
		if !scanPerson.MatchString(person) {
			person = ""
		}
		if _, err := cfg.Laib.ScanOrder(r.Context(), do, person); err != nil {
			log.Printf("haus: scannen: %s: %v", do, err)
			writeDokumente(w, r, cfg, http.StatusBadGateway, "", "", saysScanNotDone)
			return
		}
		to := DokumentePath
		if scanSetup[do] { // the page waits for the result there (haus#80)
			now, _ := cfg.clock()
			to += "?tut=" + do + "&seit=" + strconv.FormatInt(now.Unix(), 10) + "#scanner"
		}
		http.Redirect(w, r, to, http.StatusSeeOther)
	}
}

// scanEinrichten proposes the scanner (laib#571), or without the service
// the service: one proposal, which Laib's dial and the owner decide; back
// to the page with a fixed sentence and the way to the inbox.
func scanEinrichten(w http.ResponseWriter, r *http.Request, cfg StartConfig) {
	say := func(key, nr string) {
		to := DokumentePath + "?gesagt=" + key
		if nr != "" {
			to += "&vorschlag=" + nr
		}
		http.Redirect(w, r, to+"#scanner", http.StatusSeeOther)
	}
	geraet := strings.TrimSpace(r.PostForm.Get("geraet"))
	if geraet != "" && !scanGeraet.MatchString(geraet) {
		say("geraet", "")
		return
	}
	p, err := cfg.Laib.ScanEinrichten(r.Context(), geraet)
	if err != nil {
		log.Printf("haus: scannen: einrichten by %s: %v", r.Header.Get("Remote-User"), err)
		say("nicht-moeglich", "")
		return
	}
	log.Printf("haus: scannen: einrichten by %s: Vorschlag #%d", r.Header.Get("Remote-User"), p.Proposal)
	nr := ""
	if p.Proposal > 0 {
		nr = strconv.Itoa(p.Proposal)
	}
	say("vorgeschlagen", nr)
}

// scanning is what Dokumente knows of the scan service: nothing (no
// Laib), its state, or why not; and the setup order just pressed.
type scanning struct {
	on    bool
	st    laib.Scan
	err   error
	order scanOrder
}

// scanOrder is a setup order the page follows until its result is in the
// status file or scanWaits has passed (haus#80); do is "" for none.
type scanOrder struct {
	do   string
	done bool
	late bool
}

// readScan asks Laib for the scan service's state; without a Laib, off.
func readScan(r *http.Request, cfg StartConfig) scanning {
	if cfg.Laib == nil {
		return scanning{}
	}
	s := scanning{on: true}
	fetch(r, func(ctx context.Context) { s.st, s.err = cfg.Laib.ScanStatus(ctx) })
	now, _ := cfg.clock()
	s.order = followOrder(r.URL.Query(), s.st, now)
	return s
}

// followOrder reads ?tut=…&seit=… (the redirect after a setup order): done
// when the result the order writes is as new as the order, late when it
// is not and scanWaits has passed. A link older than ten minutes is none.
func followOrder(q url.Values, st laib.Scan, now time.Time) scanOrder {
	do := q.Get("tut")
	sec, err := strconv.ParseInt(q.Get("seit"), 10, 64)
	if !scanSetup[do] || err != nil {
		return scanOrder{}
	}
	since := time.Unix(sec, 0)
	if age := now.Sub(since); age < -time.Minute || age > 10*time.Minute {
		return scanOrder{}
	}
	newer := func(at string) bool {
		t, err := time.Parse(time.RFC3339, at)
		return err == nil && !t.Before(since)
	}
	o := scanOrder{do: do}
	switch do {
	case "suchen":
		o.done = st.Suche.Zustand != "läuft" && newer(st.Suche.At)
	default:
		o.done = newer(st.Pruefung.At)
	}
	o.late = !o.done && now.Sub(since) > scanWaits[do]
	return o
}

// waitWords is scanWaits in the page's words.
func waitWords(d time.Duration) string {
	switch {
	case d == time.Minute:
		return "einer Minute"
	case d%time.Minute == 0:
		return strconv.Itoa(int(d/time.Minute)) + " Minuten"
	}
	return strconv.Itoa(int(d/time.Second)) + " Sekunden"
}

// waiting is a setup order whose result has not come yet.
func (o scanOrder) waiting() bool { return o.do != "" && !o.done && !o.late }

// active is a stack running or waiting: the section goes to the top.
func (s scanning) active() bool {
	return s.on && s.err == nil && (busy(s.st.Zustand) || s.st.Zustand == "wartet-auf-rest")
}

// moving is a state that changes by itself: a stack in work, a search,
// a setup order whose result is still to come.
func (s scanning) moving() bool {
	return s.on && s.err == nil && (busy(s.st.Zustand) || s.st.Suche.Zustand == "läuft" || s.order.waiting())
}

// refresh is how soon the page looks again: soon while an admin waits for
// an order's result, calmer for a stack.
func (s scanning) refresh() string {
	if s.order.waiting() {
		return "2"
	}
	return "5"
}

// busy is a state that moves on by itself, without anyone's button.
func busy(zustand string) bool {
	return zustand == "scannt" || zustand == "trennt" || zustand == "legt-ab"
}

// scanState is the state in the family's words: a line and its detail.
func scanState(st laib.Scan) (string, string) {
	switch st.Zustand {
	case "bereit":
		return "Bereit", "Den Stapel in den Einzug legen."
	case "scannt":
		if st.Seite > 0 {
			return "Scannt", "Seite " + strconv.Itoa(st.Seite)
		}
		return "Scannt", ""
	case "wartet-auf-rest":
		if st.Blatt > 0 {
			n := strconv.Itoa(st.Blatt)
			return "Nach Blatt " + n + " abgebrochen", "Den Rest ab Blatt " + n + " einlegen, dann „Weiter“."
		}
		return "Abgebrochen", "Den Rest einlegen, dann „Weiter“."
	case "trennt":
		return "Trennt die Dokumente", "Das Hausmodell liest die Seiten."
	case "legt-ab":
		return "Legt die Dokumente ab", ""
	case "fertig":
		return "Fertig", "Bereit für den nächsten Stapel."
	case "fehler":
		return "Der letzte Stapel ging schief", oneLine(st.Detail)
	}
	return oneLine(st.Detail), ""
}

// scanSection is the Dokumente page's Scannen (haus#76): the state,
// „Stapel scannen“ with a scanner and nothing running, after a jam
// Weiter, Abschließen and Verwerfen; the inbox folder; the last stacks,
// each document leading to Paperless where it opens. A box without the
// service shows nothing; a Laib that cannot yet says so to an admin.
func scanSection(r *http.Request, cfg StartConfig, s scanning) string {
	if !s.on {
		return ""
	}
	if errors.Is(s.err, laib.ErrNoScan) {
		if !isAdmin(r) {
			return ""
		}
		return `<section class="weg scannen" id=scanner><h2>Scannen</h2>` + scanSaying(r, cfg) + `<p class=k>` + html.EscapeString(saysScanUnset) + `</p>` +
			`<div class=knoepfe>` + scanButton("Scannen einrichten", "einrichten") + `</div>` + byHand("Mit diesem Scanner einrichten") + `</section>` + scanScript
	}
	if errors.Is(s.err, laib.ErrNoTool) {
		if !isAdmin(r) {
			return ""
		}
		return `<section class=weg><h2>Scannen</h2><p class=k>` + html.EscapeString(saysScanOld+" "+saysScanOldAd) + `</p></section>`
	}
	var b strings.Builder
	b.WriteString(`<section class="weg scannen" aria-live=polite><h2>Scannen</h2>`)
	if s.err != nil {
		log.Printf("haus: dokumente page: scan: %v", s.err)
		b.WriteString(`<p>` + html.EscapeString(saysScanSilent) + `</p></section>`)
		return b.String()
	}
	st := s.st
	text, detail := scanState(st)
	b.WriteString(`<p class=zustand><b>` + html.EscapeString(text) + `</b>`)
	if detail != "" {
		b.WriteString(` <span class=k>` + html.EscapeString(detail) + `</span>`)
	}
	b.WriteString(`</p>`)
	order := scanButton
	switch {
	case st.Zustand == "wartet-auf-rest":
		b.WriteString(`<div class=knoepfe>` + order("Weiter", "weiter") + order("Abschließen", "abschliessen") + order("Verwerfen", "verwerfen") + `</div>`)
	case st.Scanner && (st.Zustand == "bereit" || st.Zustand == "fertig" || st.Zustand == "fehler"):
		b.WriteString(`<div class=knoepfe>` + order("Stapel scannen", "start") + `</div>`)
	}
	eingang := st.Eingang
	if eingang == "" {
		eingang = "scan-eingang"
	}
	where := `\data\` + eingang + ` auf dem Netzlaufwerk`
	if cfg.SambaHost != "" {
		where = `\\` + cfg.SambaHost + `\data\` + eingang
	}
	b.WriteString(`<p class=k>Oder den Scanner in den Ordner <span class=adresse>` + html.EscapeString(where) + `</span> scannen lassen (nur zu Hause im WLAN). ` + html.EscapeString(saysStackHow) + `</p>`)
	b.WriteString(stacks(r, cfg, st.Letzte))
	if isAdmin(r) {
		b.WriteString(scannerSetup(r, cfg, st, s.order))
	}
	b.WriteString(`</section>` + scanScript)
	return b.String()
}

// einzelheiten is the raw error behind a sentence (laib#589), folded away
// under „Einzelheiten“ for an admin; "" for none.
func einzelheiten(raw string) string {
	if raw = oneLine(raw); raw == "" {
		return ""
	}
	return `<details class=k><summary>Einzelheiten</summary><p class=adresse>` + html.EscapeString(raw) + `</p></details>`
}

// scanButton is one order's button, a POST to /scannen; scanScript turns
// a setup order's into „Sucht …“ the moment it is pressed.
func scanButton(label, do string) string {
	runs := ""
	if say := scanRuns[do]; say != "" {
		runs = ` data-laeuft="` + html.EscapeString(say) + `"`
	}
	return `<form method=post action=` + ScannenPath + `><input type=hidden name=tun value=` + do + `><button` + runs + `>` + html.EscapeString(label) + `</button></form>`
}

// scanScript answers a press at once (haus#79): the button says what runs
// and takes no second press while the order goes to Laib. Without script
// the redirect's page says the same a moment later.
const scanScript = `<script>(function(){document.querySelectorAll('form[action="` + ScannenPath + `"]').forEach(function(f){f.addEventListener('submit',function(){` +
	`var b=f.querySelector('button');if(!b)return;setTimeout(function(){b.disabled=true;b.textContent=b.getAttribute('data-laeuft')||'Einen Moment …'},0)})})})()</script>`

// byHand is the field for a device the search does not find.
func byHand(label string) string {
	return `<form method=post action=` + ScannenPath + ` class=von-hand><input type=hidden name=tun value=einrichten>` +
		`<label for=geraet>Adresse von Hand</label><input id=geraet type=text name=geraet required maxlength=128 autocomplete=off autocapitalize=none spellcheck=false placeholder="epson2:net:192.168.178.87">` +
		`<button>` + html.EscapeString(label) + `</button></form>`
}

// scanSaying is the sentence after „Einrichten“, with the way to the inbox.
func scanSaying(r *http.Request, cfg StartConfig) string {
	q := r.URL.Query()
	key := q.Get("gesagt")
	said := scanSaid[key]
	if said == "" {
		return ""
	}
	if key != "vorgeschlagen" {
		return `<div class=achtung role=alert><p>` + html.EscapeString(said) + `</p></div>`
	}
	if link := inboxLink(cfg.LaibURL, q.Get("vorschlag")); link != "" {
		return `<div class=gut role=status><span class=punkt></span><p>` + html.EscapeString(said) + ` ` + link + `</p></div>`
	}
	return `<div class=gut role=status><span class=punkt></span><p>` + html.EscapeString(said) + `</p></div>`
}

// scannerSetup is „Scanner einrichten“ for admins (haus#77, laib#571): the
// last check — „Scanner antwortet“ or why not — with Erneut prüfen and
// Probeseite; Scanner suchen and what it found, each with Diesen
// einrichten; a device by hand; and for a scanner that scans to a folder
// what goes into its own menu. Open while no scanner is set, a search
// runs, or just after an order.
func scannerSetup(r *http.Request, cfg StartConfig, st laib.Scan, o scanOrder) string {
	var b strings.Builder
	open := ""
	if !st.Scanner || st.Suche.Zustand == "läuft" || r.URL.Query().Get("gesagt") != "" || o.do != "" {
		open = " open"
	}
	b.WriteString(`<details id=scanner class=einrichten` + open + `><summary>Scanner einrichten</summary>`)
	b.WriteString(scanSaying(r, cfg))
	b.WriteString(`<p class=k>` + html.EscapeString(saysSetupHow) + `</p>`)
	checked := o.done && (o.do == "pruefen" || o.do == "probe")
	if p := st.Pruefung; p.At != "" && !(o.waiting() && o.do != "suchen") {
		say, raw := oneLine(p.Detail), oneLine(p.Einzelheiten)
		if say == "" && p.OK {
			say = "Scanner antwortet"
		}
		role := ""
		if checked { // the answer to the button just pressed
			role = " role=status"
		}
		open := `<div class=gut` + role + `><span class=punkt></span><p>`
		if !p.OK {
			open = `<div class="achtung selbst"` + role + `><p>` // its own dot, in the warning's colour
		}
		b.WriteString(open + html.EscapeString(say))
		if p.Geraet != "" {
			b.WriteString(` <span class="k adresse">` + html.EscapeString(p.Geraet) + `</span>`)
		}
		b.WriteString(`</p>`)
		b.WriteString(einzelheiten(raw))
		b.WriteString(`</div>`)
	}
	switch {
	case o.waiting():
		b.WriteString(`<p class=laeuft role=status>` + html.EscapeString(scanRuns[o.do]) + `</p>`)
	case o.late:
		b.WriteString(`<div class=achtung role=alert><p>` + html.EscapeString(fmt.Sprintf(saysOrderLate, waitWords(scanWaits[o.do]))) + `</p></div>`)
	}
	free := !busy(st.Zustand) && st.Zustand != "wartet-auf-rest"
	b.WriteString(`<div class=knoepfe>`)
	if st.Scanner && free && !o.waiting() {
		b.WriteString(scanButton("Erneut prüfen", "pruefen") + scanButton("Probeseite", "probe"))
	}
	if st.Suche.Zustand != "läuft" && !o.waiting() {
		b.WriteString(scanButton("Scanner suchen", "suchen"))
	}
	b.WriteString(`</div>`)
	searched := o.done && o.do == "suchen"
	nothing := false
	switch su := st.Suche; su.Zustand {
	case "läuft":
		if o.do != "suchen" {
			b.WriteString(`<p class=laeuft role=status>` + html.EscapeString(scanRuns["suchen"]) + `</p>`)
		}
	case "fertig", "fehler":
		if o.waiting() && o.do == "suchen" {
			break // the last search's result is not this one's
		}
		if len(su.Funde) == 0 {
			nothing = true
			role := ""
			if searched {
				role = " role=alert"
			}
			say := saysNotFound
			if su.Zustand == "fehler" && oneLine(su.Detail) != "" { // fertig's detail says what saysNotFound says
				say = oneLine(su.Detail)
			}
			b.WriteString(`<div class="achtung selbst"` + role + `><p>` + html.EscapeString(say) + `</p>`)
			if su.Gefragt > 0 {
				b.WriteString(`<p>` + html.EscapeString(fmt.Sprintf(saysAsked, su.Gefragt)) + `</p>`)
			}
			b.WriteString(einzelheiten(su.Einzelheiten) + `</div>`)
			break
		}
		if len(su.Funde) > 0 {
			b.WriteString(`<ul class=funde>`)
			for _, f := range su.Funde {
				name := oneLine(f.Name)
				if name == "" {
					name = f.Geraet
				}
				b.WriteString(`<li><span>` + html.EscapeString(name) + ` <span class="k adresse">` + html.EscapeString(f.Geraet) + `</span></span>`)
				if scanGeraet.MatchString(f.Geraet) {
					b.WriteString(`<form method=post action=` + ScannenPath + `><input type=hidden name=tun value=einrichten><input type=hidden name=geraet value="` + html.EscapeString(f.Geraet) +
						`"><button aria-label="` + html.EscapeString(name+" einrichten") + `">Diesen einrichten</button></form>`)
				}
				b.WriteString(`</li>`)
			}
			b.WriteString(`</ul>`)
		}
		if d := oneLine(su.Detail); d != "" {
			b.WriteString(`<p class=k>` + html.EscapeString(d) + `</p>`)
		}
	}
	field := byHand("Einrichten")
	if nothing && searched { // nothing found: the field is the next step
		field = strings.Replace(field, `<input id=geraet `, `<input id=geraet autofocus `, 1)
	}
	b.WriteString(field)
	host := `\<box>`
	if cfg.SambaHost != "" {
		host = `\` + cfg.SambaHost
	}
	eingang := st.Eingang
	if eingang == "" {
		eingang = "scan-eingang"
	}
	b.WriteString(`<p class=k>` + html.EscapeString(saysFolderSetup) + `</p><ul class=k>` +
		`<li>Freigabe <span class=adresse>` + html.EscapeString(host+`\data`) + `</span>, Ordner <span class=adresse>` + html.EscapeString(eingang) + `</span></li>` +
		`<li>Benutzer und Passwort: ein Konto fürs <a href=` + netzlaufwerkPath + `>Netzlaufwerk</a></li></ul>`)
	b.WriteString(`</details>`)
	return b.String()
}

// stacks is the last stacks, newest first: when, how many documents from
// how many pages, and each document by its title.
func stacks(r *http.Request, cfg StartConfig, all []laib.Stack) string {
	var b strings.Builder
	b.WriteString(`<h3>Letzte Stapel</h3>`)
	if len(all) == 0 {
		return b.String() + `<p class=k>` + html.EscapeString(saysNoStacks) + `</p>`
	}
	now, loc := cfg.clock()
	opens := cfg.paperlessOpens(r)
	b.WriteString(`<ul class=stapel>`)
	for i, s := range all {
		if i == maxStacks {
			b.WriteString(`<li class=k>und ` + strconv.Itoa(len(all)-maxStacks) + ` weitere</li>`)
			break
		}
		b.WriteString(`<li><b>` + html.EscapeString(stackLine(s)) + `</b> <span class=k>` + html.EscapeString(stackDetail(s, now, loc)) + `</span>`)
		if isAdmin(r) {
			b.WriteString(einzelheiten(s.Einzelheiten))
		}
		if len(s.Dokumente) > 0 {
			b.WriteString(`<ul>`)
			for _, d := range s.Dokumente {
				title := oneLine(d.Titel)
				if title == "" {
					title = oneLine(d.Datei)
				}
				if opens {
					b.WriteString(`<li><a href="` + html.EscapeString(cfg.paperless().href("/documents?title_content="+url.QueryEscape(title))) + `">` + html.EscapeString(title) + `</a></li>`)
				} else {
					b.WriteString(`<li>` + html.EscapeString(title) + `</li>`)
				}
			}
			b.WriteString(`</ul>`)
		}
		b.WriteString(`</li>`)
	}
	b.WriteString(`</ul>`)
	return b.String()
}

// stackLine is a stack's result in a few words.
func stackLine(s laib.Stack) string {
	switch s.Ende {
	case "abgebrochen":
		return "Verworfen, nichts abgelegt"
	case "fehler":
		return "Ging schief"
	}
	switch n := len(s.Dokumente); n {
	case 0:
		return "Kein Dokument"
	case 1:
		return "1 Dokument"
	default:
		return strconv.Itoa(n) + " Dokumente"
	}
}

// stackDetail is when, from where, and how many pages.
func stackDetail(s laib.Stack, now time.Time, loc *time.Location) string {
	var parts []string
	if t, err := time.Parse(time.RFC3339, s.At); err == nil {
		t = t.In(loc)
		day := dayWords(t, loc)
		if y, m, d := now.In(loc).Date(); t.Year() == y && t.Month() == m && t.Day() == d {
			day = "heute"
		}
		parts = append(parts, day+", "+t.Format("15:04"))
	}
	switch s.Quelle {
	case "scanner":
		parts = append(parts, "vom Scanner")
	case "ordner":
		parts = append(parts, "aus dem Ordner")
	}
	if s.Seiten > 0 {
		p := strconv.Itoa(s.Seiten) + " Seiten"
		if s.Seiten == 1 {
			p = "1 Seite"
		}
		if s.Leer > 0 {
			p += ", " + strconv.Itoa(s.Leer) + " leer entfernt"
		}
		parts = append(parts, p)
	}
	if s.Ende == "fehler" && oneLine(s.Meldung) != "" {
		parts = append(parts, oneLine(s.Meldung))
	}
	return strings.Join(parts, " · ")
}

const scanStyle = `<style>.scannen .zustand{margin:10px 0 0}.scannen .knoepfe{display:flex;flex-wrap:wrap;gap:8px;margin:12px 0 0}.scannen .knoepfe button{margin:0;width:auto}
.scannen h3{font-size:.85rem;margin:18px 0 0;color:var(--muted);font-weight:600}.stapel{margin:6px 0 0;padding:0;list-style:none}.stapel>li{padding:8px 0;border-top:1px solid var(--line)}
.stapel ul{margin:4px 0 0;padding-left:18px}.stapel li li{font-size:.9rem;overflow-wrap:anywhere}
.einrichten{margin:18px 0 0;padding:12px 0 0;border-top:1px solid var(--line)}.einrichten summary{cursor:pointer;font-weight:600;min-height:44px;display:flex;align-items:center}
.funde{margin:10px 0 0;padding:0;list-style:none}.funde li{display:flex;flex-wrap:wrap;align-items:center;justify-content:space-between;gap:8px;padding:8px 0;border-top:1px solid var(--line)}.funde button{margin:0;width:auto}
.einrichten ul.k{margin:6px 0 0;padding-left:18px;font-size:.85rem}.scannen .gut,.scannen .achtung{margin:10px 0 0}
.laeuft{margin:12px 0 0;font-weight:600}.laeuft::before{content:"";display:inline-block;width:8px;height:8px;border-radius:50%;background:var(--accent);margin-right:8px;vertical-align:1px;animation:puls 1.2s ease-in-out infinite}
@keyframes puls{50%{opacity:.25}}@media (prefers-reduced-motion:reduce){.laeuft::before{animation:none}}.scannen button:disabled{opacity:.6}</style>`
