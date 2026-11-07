package pages

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mdopp/homes/internal/caldav"
	"github.com/mdopp/homes/internal/haushalt"
	"github.com/mdopp/homes/internal/kontaktantwort"
	"github.com/mdopp/homes/internal/texte"
)

// Kontakt prüfen lassen (owner 2026-11-07, kontaktantwort): the person
// picks one of their Handy-Kontakte or, an admin, one of the house's,
// sees what the link will show, makes it, and sends it from the phone's
// share sheet with a prepared sentence (the Bote writes only to the
// household). The answers come back to Kontakt-Antworten: each field
// before → after with what the checks found, taken one by one or all,
// or discarded. Nothing reaches an address book without that.
const (
	PruefenPath   = KontaktePath + "/pruefen"
	AntwortenPath = KontaktePath + "/antworten"
)

// pruefenAn: the store, Radicale and the public page's address are there.
func (cfg StartConfig) pruefenAn() bool {
	return cfg.Antworten != nil && cfg.Contacts != nil && cfg.KontaktURL != ""
}

// pruefenLink is the quiet line under Kontakte: the start, and the
// answers waiting.
func pruefenLink(r *http.Request, cfg StartConfig) string {
	if !cfg.pruefenAn() {
		return ""
	}
	out := `<p class=k><a href=` + PruefenPath + `>` + wort(texte.PruefenTitel) + `</a> · ` + wort(texte.PruefenKurz) + `</p>`
	if n := len(cfg.Antworten.ZuPruefen(r.Header.Get("Remote-User"))); n > 0 {
		out += `<p class=k><a href=` + AntwortenPath + `>` + html.EscapeString(antwortenAufgabe(n)) + `</a></p>`
	}
	return out
}

// pruefenKnopf is a house contact's „Kontakt prüfen lassen“, for an admin.
func pruefenKnopf(r *http.Request, cfg StartConfig, uid string) string {
	if !cfg.pruefenAn() || !isAdmin(r) {
		return ""
	}
	return `<p class=weiter-in><a href="` + PruefenPath + `?buch=haushalt&amp;uid=` + html.EscapeString(url.QueryEscape(uid)) + `">` + wort(texte.PruefenTitel) + `</a></p>`
}

func wort(id texte.ID) string { return html.EscapeString(texte.T(id)) }

func antwortenAufgabe(n int) string {
	return fmt.Sprintf(texte.T(texte.AntwortenAufgabe), texte.Anzahl(n, "Kontakt-Antwort", "Kontakt-Antworten"))
}

// pruefenKontakte are the contacts of buch the viewer may have checked.
func pruefenKontakte(ctx context.Context, r *http.Request, cfg StartConfig, buch string) ([]caldav.Contact, error) {
	if buch == kontaktantwort.Haushalt {
		all, _, err := cfg.Contacts.Contacts(ctx)
		return all, err
	}
	return meineKontakte(ctx, cfg, r.Header.Get("Remote-User"))
}

// Pruefen is the start: the list to pick from (?buch=privat|haushalt), a
// contact's page before the link (?uid=), and the link made (POST).
func Pruefen(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !cfg.pruefenAn() {
			notSetContacts.write(w, r, cfg, texte.T(texte.PruefenTitel))
			return
		}
		if r.Method != http.MethodPost && !showOnly(w, r, texte.T(texte.PruefenTitel)) {
			return
		}
		if r.Method == http.MethodPost {
			r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
			r.ParseForm()
		}
		buch, uid := r.FormValue("buch"), r.FormValue("uid")
		if buch != kontaktantwort.Haushalt {
			buch = kontaktantwort.Privat
		}
		if buch == kontaktantwort.Haushalt && !isAdmin(r) {
			missing{http.StatusForbidden, texte.T(texte.PruefenNurAdmins), texte.T(texte.PruefenTitel), PruefenPath, texte.T(texte.PruefenTitel)}.write(w, r, cfg)
			return
		}
		var all []caldav.Contact
		var err error
		fetch(r, func(ctx context.Context) { all, err = pruefenKontakte(ctx, r, cfg, buch) })
		b := familyHead(r, cfg, texte.T(texte.PruefenTitel), "Kontakte")
		b.WriteString(kontakteStyle + pruefenStyle)
		if err != nil {
			log.Printf("homes: kontakt prüfen: %s: %v", buch, err)
			b.WriteString("<h1>" + wort(texte.PruefenTitel) + "</h1><p class=k>" + saysNoContacts + "</p>")
			writePage(w, http.StatusOK, b)
			return
		}
		if uid == "" {
			pruefenListe(b, r, cfg, buch, all)
			writePage(w, http.StatusOK, b)
			return
		}
		for _, ct := range all {
			if ct.UID != uid {
				continue
			}
			f := kontaktantwort.FelderVon(ct)
			if r.Method == http.MethodPost {
				pruefenErstellen(w, r, cfg, b, buch, uid, f)
				return
			}
			b.WriteString("<h1>" + html.EscapeString(fmt.Sprintf(texte.T(texte.PruefenVorher), f.Name())) + "</h1><p class=k>" + wort(texte.PruefenHinweis) + "</p>")
			b.WriteString("<p>" + html.EscapeString(fmt.Sprintf(texte.T(texte.PruefenWas), cmp.Or(f.Vorname, f.Name()))) + "</p>" + felderKarte(f))
			b.WriteString(`<form method=post action=` + PruefenPath + `><input type=hidden name=buch value=` + buch + `><input type=hidden name=uid value="` + html.EscapeString(uid) +
				`"><button data-laeuft="` + wort(texte.PruefenErstelltLaeuft) + `">` + wort(texte.PruefenErstellen) + `</button></form>`)
			writePage(w, http.StatusOK, b)
			return
		}
		b.WriteString("<h1>" + wort(texte.PruefenTitel) + "</h1><p>" + wort(texte.PruefenWeg) + "</p><p><a href=" + PruefenPath + "?buch=" + buch + ">" + wort(texte.PruefenZurueck) + "</a></p>")
		writePage(w, http.StatusNotFound, b)
	}
}

// pruefenListe is the list to pick from, with the switch between the two books.
func pruefenListe(b *strings.Builder, r *http.Request, cfg StartConfig, buch string, all []caldav.Contact) {
	b.WriteString("<h1>" + wort(texte.PruefenTitel) + "</h1><p class=k>" + wort(texte.PruefenHinweis) + "</p>")
	if n := len(cfg.Antworten.ZuPruefen(r.Header.Get("Remote-User"))); n > 0 {
		b.WriteString(`<p class=weiter-in><a href=` + AntwortenPath + `>` + html.EscapeString(antwortenAufgabe(n)) + `</a></p>`)
	}
	if isAdmin(r) {
		cur := func(x string) string {
			if x == buch {
				return " aria-current=page"
			}
			return ""
		}
		b.WriteString(`<p class=buecher><a href=` + PruefenPath + `?buch=privat` + cur(kontaktantwort.Privat) + `>` + wort(texte.PruefenHandy) + `</a>` +
			`<a href=` + PruefenPath + `?buch=haushalt` + cur(kontaktantwort.Haushalt) + `>` + wort(texte.PruefenHaus) + `</a></p>`)
	}
	if len(all) == 0 {
		b.WriteString("<p class=k>" + wort(texte.PruefenLeer) + "</p>")
		return
	}
	field := wort(texte.KontakteSuchen)
	b.WriteString(`<input type=search id=kontakte-suche placeholder="` + field + `" aria-label="` + field + `" autocomplete=off><p class=k id=kontakte-keiner data-vorlage="` + wort(texte.KontakteKeiner) + `" hidden></p>`)
	kontakteListe(b, kontaktZeilen(all, func(uid string) string { return PruefenPath + "?buch=" + buch + "&uid=" + url.QueryEscape(uid) }), len(all) >= kontakteBuchstaben)
	b.WriteString(kontakteScript)
}

// pruefenErstellen makes the link and shows it once, with the sentence to send.
func pruefenErstellen(w http.ResponseWriter, r *http.Request, cfg StartConfig, b *strings.Builder, buch, uid string, f kontaktantwort.Felder) {
	if !fromHomes(r) {
		http.Error(w, "Nur von Homes' eigener Seite.", http.StatusForbidden)
		return
	}
	token, frage, err := cfg.Antworten.Neu(r.Header.Get("Remote-User"), buch, uid, f)
	if err != nil {
		log.Printf("homes: kontakt prüfen: the link not made: %v", err)
		b.WriteString("<h1>" + wort(texte.PruefenTitel) + "</h1>" + gesagt("", texte.T(texte.PruefenNicht)))
		writePage(w, http.StatusOK, b)
		return
	}
	log.Printf("homes: kontakt prüfen: link %s made (%s)", frage.ID, buch)
	link := strings.TrimRight(cfg.KontaktURL, "/") + "/" + token
	text := fmt.Sprintf(texte.T(texte.PruefenText), cmp.Or(f.Vorname, f.Name()), link)
	b.WriteString("<h1>" + html.EscapeString(f.Name()) + "</h1>" + gesagt(texte.T(texte.PruefenErstellt), ""))
	b.WriteString(`<textarea id=pruefen-text readonly rows=6 aria-label="` + wort(texte.PruefenNachricht) + `">` + html.EscapeString(text) + `</textarea>` +
		`<p class=knoepfe><button type=button id=pruefen-teilen data-titel="` + wort(texte.PruefenTitel) + `">` + wort(texte.PruefenTeilen) + `</button>` +
		`<button type=button id=pruefen-kopieren data-fertig="` + wort(texte.PruefenKopiert) + `">` + wort(texte.PruefenKopieren) + `</button></p>` +
		`<p class=k>` + wort(texte.PruefenNurJetzt) + `</p><p><a href=` + PruefenPath + `?buch=` + buch + `>` + wort(texte.PruefenZurueck) + `</a></p>` + pruefenScript)
	writePage(w, http.StatusOK, b)
}

// felderKarte is the fields as the link shows them.
func felderKarte(f kontaktantwort.Felder) string {
	var b strings.Builder
	b.WriteString("<dl class=felder>")
	row := func(art, v string) {
		if v != "" {
			b.WriteString("<dt>" + html.EscapeString(art) + "</dt><dd>" + html.EscapeString(v) + "</dd>")
		}
	}
	row(texte.T(texte.ArtVorname), f.Vorname)
	row(texte.T(texte.ArtNachname), f.Nachname)
	for _, p := range f.Telefone {
		row(texte.T(texte.ArtTelefon), nummerText(p.Nummer, p.Art))
	}
	for _, m := range f.Mails {
		row(texte.T(texte.ArtMail), m)
	}
	for _, a := range f.Adressen {
		row(texte.T(texte.ArtAdresse), a.Zeile())
	}
	if f.Geburtstag != "" {
		row(texte.T(texte.ArtGeburtstag), haushalt.BirthdayText(f.Geburtstag))
	}
	row(texte.T(texte.ArtFirma), f.Firma)
	row(texte.T(texte.ArtPosition), f.Position)
	b.WriteString("</dl>")
	return b.String()
}

const pruefenStyle = `<style>.felder{display:grid;grid-template-columns:auto 1fr;gap:6px 14px;margin:12px 0 18px;padding:14px;border-radius:16px;background:var(--card)}
.felder dt{color:var(--muted);font-size:.9rem}.felder dd{margin:0;overflow-wrap:anywhere}
.buecher{display:flex;gap:8px;flex-wrap:wrap}.buecher a{padding:8px 14px;border-radius:999px;background:var(--card);color:var(--ink)}.buecher a[aria-current]{background:var(--accent-soft);color:var(--accent);font-weight:600}
#pruefen-text{width:100%;box-sizing:border-box;font:inherit;padding:12px;border-radius:12px;border:1px solid var(--line);background:var(--card);color:var(--ink)}
.knoepfe{display:flex;gap:10px;flex-wrap:wrap}
.aenderung{margin:12px 0;padding:14px;border-radius:16px;background:var(--card)}.aenderung label{display:flex;gap:12px;align-items:flex-start}
.aenderung input{width:22px;height:22px;flex:none;margin-top:2px}.aenderung .art{font-size:.85rem;color:var(--muted)}
.aenderung .vorher{color:var(--muted);text-decoration:line-through;overflow-wrap:anywhere}.aenderung .nachher{font-weight:600;overflow-wrap:anywhere}
.aenderung .hinweis{margin:8px 0 0 34px;color:var(--warn,#e0a030);font-size:.9rem}.aenderung .stand{margin:6px 0 0 34px;font-size:.85rem;color:var(--muted)}</style>`

// pruefenScript is the two buttons: the phone's share sheet (WhatsApp,
// SMS, Mail), else the text copied, each saying what it did.
const pruefenScript = `<script>(function(){var t=document.getElementById('pruefen-text'),s=document.getElementById('pruefen-teilen'),k=document.getElementById('pruefen-kopieren');if(!t)return;
function kopiert(){var w=k.textContent;k.textContent=k.getAttribute('data-fertig');setTimeout(function(){k.textContent=w},2500)}
function kopieren(){if(navigator.clipboard){navigator.clipboard.writeText(t.value).then(kopiert,function(){t.select();document.execCommand('copy');kopiert()})}else{t.select();document.execCommand('copy');kopiert()}}
k.addEventListener('click',kopieren);
if(!navigator.share){s.hidden=true;return}
s.addEventListener('click',function(){navigator.share({title:s.getAttribute('data-titel'),text:t.value}).catch(function(e){if(e&&e.name!=='AbortError')kopieren()})})})()</script>`

// Antworten is Kontakt-Antworten: the list (GET /kontakte/antworten) and
// one answer's review (…/<id>, POST aktion=auswahl|alle|verwerfen, nimm=<n>…).
func Antworten(cfg StartConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if !cfg.pruefenAn() {
			notSetContacts.write(w, r, cfg, texte.T(texte.AntwortenTitel))
			return
		}
		if r.Method != http.MethodPost && !showOnly(w, r, texte.T(texte.AntwortenTitel)) {
			return
		}
		wer := r.Header.Get("Remote-User")
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, AntwortenPath), "/")
		b := familyHead(r, cfg, texte.T(texte.AntwortenTitel), "Kontakte")
		b.WriteString(pruefenStyle)
		if id == "" {
			antwortenListe(b, cfg, wer)
			writePage(w, http.StatusOK, b)
			return
		}
		f, err := cfg.Antworten.Eine(id, wer)
		if err != nil {
			b.WriteString("<h1>" + wort(texte.AntwortenTitel) + "</h1><p>" + wort(texte.AntwortWeg) + "</p><p><a href=" + AntwortenPath + ">" + wort(texte.AntwortenZurueck) + "</a></p>")
			writePage(w, http.StatusNotFound, b)
			return
		}
		said, problem := "", ""
		if r.Method == http.MethodPost {
			if !fromHomes(r) {
				http.Error(w, "Nur von Homes' eigener Seite.", http.StatusForbidden)
				return
			}
			f, said, problem = antwortEntscheiden(r, cfg, f)
		}
		antwortSeite(b, f, said, problem)
		writePage(w, http.StatusOK, b)
	}
}

func antwortenListe(b *strings.Builder, cfg StartConfig, wer string) {
	b.WriteString("<h1>" + wort(texte.AntwortenTitel) + "</h1>")
	offen := cfg.Antworten.ZuPruefen(wer)
	if len(offen) == 0 {
		b.WriteString("<p class=k>" + wort(texte.AntwortenLeer) + "</p>")
	}
	b.WriteString("<ul class=kontakte>")
	for _, f := range offen {
		was := texte.T(texte.AntwortenBestaetigt)
		if len(f.Aenderungen) > 0 {
			was = texte.Anzahl(len(f.Aenderungen), "Änderung", "Änderungen")
		}
		b.WriteString(`<li><a href=` + AntwortenPath + `/` + f.ID + `>` + html.EscapeString(f.Name()) + `</a><span class=k> · ` + html.EscapeString(was) + `</span></li>`)
	}
	b.WriteString("</ul><p><a href=" + PruefenPath + ">" + wort(texte.PruefenTitel) + "</a></p>")
}

// antwortEntscheiden keeps the review and, for a Haushalts-Kontakt, writes
// what was taken: the answer as it stands then, and what to say.
func antwortEntscheiden(r *http.Request, cfg StartConfig, f kontaktantwort.Frage) (kontaktantwort.Frage, string, string) {
	if !f.ZuPruefen() {
		return f, "", ""
	}
	r.Body = http.MaxBytesReader(nil, r.Body, 8<<10)
	r.ParseForm()
	var nimm []int
	for _, v := range r.PostForm["nimm"] {
		if n, err := strconv.Atoi(v); err == nil {
			nimm = append(nimm, n)
		}
	}
	aktion := r.PostFormValue("aktion")
	stand := kontaktantwort.Stand(f, aktion == "alle", nimm)
	if aktion == "verwerfen" {
		stand = kontaktantwort.Stand(f, false, nil)
	}
	var err error
	fetch(r, func(ctx context.Context) { stand, err = kontaktantwort.Uebernehmen(ctx, cfg.Contacts, f, stand) })
	problem := ""
	switch {
	case errors.Is(err, kontaktantwort.ErrWeg):
		problem = texte.T(texte.AntwortKontaktWeg)
	case err != nil:
		log.Printf("homes: kontakt-antwort %s: not written: %v", f.ID, err)
		return f, "", texte.T(texte.AntwortNicht)
	}
	done, err := cfg.Antworten.Entscheiden(f.ID, f.Wer, stand)
	if err != nil {
		return f, "", texte.T(texte.AntwortNicht)
	}
	n := map[string]int{}
	for _, s := range stand {
		n[s]++
	}
	log.Printf("homes: kontakt-antwort %s decided: %v", f.ID, n)
	var said []string
	if aktion == "verwerfen" || n[kontaktantwort.Abgelehnt] == len(stand) && len(stand) > 0 {
		said = append(said, texte.T(texte.AntwortVerworfen))
	}
	if k := n[kontaktantwort.Uebernommen]; k > 0 {
		said = append(said, fmt.Sprintf(texte.T(texte.AntwortGeschrieben), texte.Anzahl(k, "Änderung", "Änderungen")))
	}
	if k := n[kontaktantwort.Angenommen]; k > 0 {
		said = append(said, fmt.Sprintf(texte.T(texte.AntwortZumHandy), texte.Anzahl(k, "Änderung", "Änderungen")))
	}
	if k := n[kontaktantwort.Uebersprungen]; k > 0 && problem == "" {
		problem = fmt.Sprintf(texte.T(texte.AntwortUebersprungen), texte.Anzahl(k, "Änderung", "Änderungen"))
	}
	if len(stand) == 0 {
		said = append(said, texte.T(texte.AntwortErledigtGesagt))
	}
	return done, strings.Join(said, " "), problem
}

// artText is a field's name as the review says it.
var artText = map[string]texte.ID{"vorname": texte.ArtVorname, "nachname": texte.ArtNachname, "telefon": texte.ArtTelefon, "mail": texte.ArtMail,
	"adresse": texte.ArtAdresse, "geburtstag": texte.ArtGeburtstag, "firma": texte.ArtFirma, "position": texte.ArtPosition}

// nummerArt is a number's kind as the family says it.
var nummerArt = map[string]texte.ID{"mobil": texte.NummerMobil, "privat": texte.NummerPrivat, "geschaeftlich": texte.NummerGeschaeftlich}

// nummerText is a number with its kind: „0171 1234567 (mobil)“.
func nummerText(n, art string) string {
	if id, ok := nummerArt[art]; ok && n != "" {
		return n + " (" + texte.T(id) + ")"
	}
	return n
}

// standText is what became of a change, once decided.
var standText = map[string]texte.ID{kontaktantwort.Uebernommen: texte.StandUebernommen, kontaktantwort.Abgelehnt: texte.StandAbgelehnt,
	kontaktantwort.Uebersprungen: texte.StandUebersprungen, kontaktantwort.Angenommen: texte.StandAngenommen}

func wertText(a kontaktantwort.Aenderung, v, art string) string {
	switch {
	case a.Art == "geburtstag" && v != "":
		return haushalt.BirthdayText(v)
	case a.Art == "telefon":
		return nummerText(v, art)
	}
	return v
}

// antwortSeite is one answer: each field before → after, its findings,
// and while undecided the three ways to end it.
func antwortSeite(b *strings.Builder, f kontaktantwort.Frage, said, problem string) {
	buch := texte.T(texte.BuchHandy)
	if f.Buch == kontaktantwort.Haushalt {
		buch = texte.T(texte.BuchHaus)
	}
	am := ""
	if f.Beantwortet != nil {
		am = f.Beantwortet.Local().Format("02.01.2006, 15:04")
	}
	b.WriteString("<h1>" + html.EscapeString(f.Name()) + "</h1><p class=k>" + html.EscapeString(fmt.Sprintf(texte.T(texte.AntwortVon), am, buch)) + "</p>" + gesagt(said, problem))
	for _, h := range f.Hinweise {
		b.WriteString(`<div class=achtung role=note><p>` + html.EscapeString(h) + `</p></div>`)
	}
	offen := f.ZuPruefen()
	if len(f.Aenderungen) == 0 {
		b.WriteString("<p>" + html.EscapeString(fmt.Sprintf(texte.T(texte.AntwortNurBestaetigt), f.Name())) + "</p>")
	}
	if offen {
		b.WriteString(`<form method=post action=` + AntwortenPath + `/` + f.ID + `>`)
	}
	for i, a := range f.Aenderungen {
		vorher, nachher := wertText(a, a.Vorher, a.VorherArt), wertText(a, a.Nachher, a.NachherArt)
		b.WriteString(`<div class=aenderung><label>`)
		if offen {
			checked := " checked"
			if len(a.Hinweise) > 0 {
				checked = ""
			}
			b.WriteString(`<input type=checkbox name=nimm value=` + strconv.Itoa(i) + checked + `>`)
		}
		b.WriteString(`<span><span class=art>` + wort(artText[a.Art]) + `</span><br>`)
		switch {
		case vorher == "":
			b.WriteString(`<span class=nachher>` + html.EscapeString(nachher) + `</span> <span class=k>(` + wort(texte.AntwortNeu) + `)</span>`)
		case nachher == "":
			b.WriteString(`<span class=vorher>` + html.EscapeString(vorher) + `</span> <span class=k>(` + wort(texte.AntwortEntfernt) + `)</span>`)
		default:
			b.WriteString(`<span class=vorher>` + html.EscapeString(vorher) + `</span> → <span class=nachher>` + html.EscapeString(nachher) + `</span>`)
		}
		b.WriteString(`</span></label>`)
		for _, h := range a.Hinweise {
			b.WriteString(`<p class=hinweis>` + html.EscapeString(h) + `</p>`)
		}
		if id, ok := standText[a.Stand]; ok && !offen {
			b.WriteString(`<p class=stand>` + wort(id) + `</p>`)
		}
		b.WriteString(`</div>`)
	}
	if offen {
		if len(f.Aenderungen) == 0 {
			b.WriteString(`<button name=aktion value=alle data-laeuft="` + wort(texte.AntwortLaeuft) + `">` + wort(texte.AntwortErledigt) + `</button></form>`)
		} else {
			b.WriteString(`<p class=knoepfe><button name=aktion value=auswahl data-laeuft="` + wort(texte.AntwortLaeuft) + `">` + wort(texte.AntwortAuswahl) + `</button>` +
				`<button name=aktion value=alle data-laeuft="` + wort(texte.AntwortLaeuft) + `">` + wort(texte.AntwortAlle) + `</button>` +
				`<button name=aktion value=verwerfen class=leise data-frage="` + wort(texte.AntwortVerwerfenFrage) + `" data-laeuft="` + wort(texte.AntwortVerwirft) + `">` + wort(texte.AntwortVerwerfen) + `</button></p></form>`)
		}
	}
	b.WriteString("<p><a href=" + AntwortenPath + ">" + wort(texte.AntwortenZurueck) + "</a></p>")
}
